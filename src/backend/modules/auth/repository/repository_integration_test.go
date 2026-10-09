//go:build integration

package repository

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jackc/pgx/v5"
)

func TestPostgresMigrationsUpDownUp(t *testing.T) {
	pool := integrationPool(t)
	applyMigrations(t, pool, "Down")
	applyMigrations(t, pool, "Up")

	if count := sqlCount(t, pool, "operator_sessions"); count != 0 {
		t.Fatal("fresh migration must have empty sessions")
	}
}

func TestPostgresRegistrationConcurrencyAndRotation(t *testing.T) {
	pool := integrationPool(t)
	service := NewService(NewRepository(pool))
	ctx := t.Context()
	var winners atomic.Int32
	var wg sync.WaitGroup

	for range 6 {
		wg.Go(func() {
			_, _, err := service.Register(ctx, createRequest("admin@example.com"), "")
			if err == nil {
				winners.Add(1)
			} else if !errors.Is(err, ErrEmailTaken) {
				t.Errorf("registration: %v", err)
			}
		})
	}
	wg.Wait()

	if winners.Load() != 1 || sqlCount(t, pool, "organizations") != 1 || sqlCount(t, pool, "users") != 1 ||
		sqlCount(t, pool, "operator_sessions") != 1 || sqlCount(t, pool, "auth_audit") != 2 || sqlCount(t, pool, "auth_changes") != 2 {
		t.Fatal("duplicate registration left partial state or extra audits")
	}

	view, raw, err := service.Login(ctx, LoginRequest{"admin@example.com", "demo-password-2026"}, "")
	if err != nil {
		t.Fatal(err)
	}
	admin, err := service.ResolveSession(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	code, err := service.JoinCode(ctx, admin.AuthContext())
	if err != nil {
		t.Fatal(err)
	}

	winners.Store(0)
	for range 10 {
		wg.Go(func() {
			_, err := service.RotateJoinCode(ctx, admin.AuthContext(), 1)
			if err == nil {
				winners.Add(1)
			} else if !errors.Is(err, ErrJoinCodeChanged) {
				t.Errorf("rotation: %v", err)
			}
		})
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatal("concurrent rotations must have exactly one winner")
	}

	join := createRequest("old-code@example.com")
	join.Organization = &OrganizationInput{Mode: "join", JoinCode: code.Code}
	if _, _, err := service.Register(ctx, join, ""); !errors.Is(err, ErrInvalidJoinCode) {
		t.Fatalf("old code: %v", err)
	}

	current, _ := service.JoinCode(ctx, admin.AuthContext())
	join.Organization.JoinCode = current.Code
	viewer, viewerRaw, err := service.Register(ctx, join, "")
	if err != nil || viewer.User.Role != RoleViewer || viewer.User.OrganizationID != view.User.OrganizationID {
		t.Fatalf("viewer registration: %v %+v", err, viewer)
	}
	if _, err := service.JoinCode(ctx, AuthContext{viewer.User.ID, viewer.User.OrganizationID, viewer.User.Role}); !errors.Is(err, ErrForbidden) {
		t.Fatal("viewer accessed code")
	}

	if err := service.Logout(ctx, raw, view.CSRFToken); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveSession(ctx, raw); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("logout did not revoke SQL session")
	}
	if _, err := service.ResolveSession(ctx, viewerRaw); err != nil {
		t.Fatal("logout revoked another browser session")
	}

	var unsafe int
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM auth_audit WHERE details::text LIKE '%code%'
		OR details::text LIKE '%password%' OR details::text LIKE '%csrf%'
	`).Scan(&unsafe)
	if err != nil || unsafe != 0 {
		t.Fatalf("audit exposed secrets: %v", err)
	}
}

func TestPostgresJoinWaitsForRotationAndAuditFailureRollsBack(t *testing.T) {
	pool := integrationPool(t)
	repo := NewRepository(pool)
	service := NewService(repo)
	ctx := t.Context()
	adminView, raw, err := service.Register(ctx, createRequest("admin@example.com"), "")
	if err != nil {
		t.Fatal(err)
	}
	admin, _ := service.ResolveSession(ctx, raw)
	code, _ := service.JoinCode(ctx, admin.AuthContext())

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Error(err)
		}
	}()

	newCode := NewOpaque(24)
	_, err = tx.Exec(ctx, "UPDATE organizations SET join_code = $1, join_code_revision = 2 WHERE id = $2", newCode, adminView.Organization.ID)
	if err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() {
		req := createRequest("viewer@example.com")
		req.Organization = &OrganizationInput{Mode: "join", JoinCode: code.Code}
		_, _, err := service.Register(ctx, req, "")
		result <- err
	}()

	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, ErrInvalidJoinCode) {
		t.Fatalf("join with replaced code: %v", err)
	}
	if sqlCount(t, pool, "users") != 1 || sqlCount(t, pool, "operator_sessions") != 1 {
		t.Fatal("rejected join left user/session")
	}

	_, err = pool.Exec(ctx, "ALTER TABLE auth_audit ADD CONSTRAINT test_reject_audit CHECK (action <> 'organization.created') NOT VALID")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Register(ctx, createRequest("rollback@example.com"), ""); err == nil {
		t.Fatal("audit failure must fail registration")
	}
	if sqlCount(t, pool, "organizations") != 1 || sqlCount(t, pool, "users") != 1 {
		t.Fatal("audit failure did not roll back business writes")
	}
}

func TestPostgresHTTPFlow(t *testing.T) {
	pool := integrationPool(t)
	service := NewService(NewRepository(pool))
	handler := NewHandler(service, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	router := gin.New()
	if err := handler.RegisterRoutes(router); err != nil {
		t.Fatal(err)
	}

	register := authRequest(router, "POST", "/api/registrations",
		`{"email":"admin@example.com","password":"demo-password-2026","organization":{"mode":"create","name":"Demo"}}`, "", "")
	if register.Code != 201 {
		t.Fatalf("HTTP/SQL registration: %d %s", register.Code, register.Body.String())
	}

	cookie := register.Result().Cookies()[0]
	get := authRequest(router, "GET", "/api/session", "", cookie.Value, "")
	if get.Code != 200 {
		t.Fatalf("HTTP/SQL session: %s", get.Body.String())
	}

	// A second organization never uses the first user's server-side identity.
	second, _, err := service.Register(t.Context(), createRequest("other@example.com"), "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.ResolveSession(t.Context(), cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	org, err := service.Organization(t.Context(), first.AuthContext())
	if err != nil || org.ID == second.Organization.ID {
		t.Fatal("organization isolation failed")
	}

	request := httptest.NewRequest("GET", "/api/organization?organization_id="+second.Organization.ID, nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 400 || strings.Contains(response.Body.String(), second.Organization.ID) {
		t.Fatal("client cannot select another organization")
	}
}
