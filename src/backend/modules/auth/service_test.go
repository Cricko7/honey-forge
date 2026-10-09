package auth

import (
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func createRequest(email string) RegisterRequest {
	return RegisterRequest{
		Email: email, Password: "demo-password-2026",
		Organization: &OrganizationInput{Mode: "create", Name: " Demo SOC "},
	}
}

func TestCreateJoinAndSessionLifecycle(t *testing.T) {
	repo := newMemoryStore()
	service := NewService(repo)
	ctx := t.Context()

	admin, raw, err := service.Register(ctx, createRequest(" Admin@Example.com "), "")
	if err != nil {
		t.Fatal(err)
	}
	if admin.User.Role != RoleAdmin || admin.User.Email != "admin@example.com" || admin.Organization.Name != "Demo SOC" {
		t.Fatalf("invalid admin: %+v", admin)
	}
	if delta := time.Until(admin.ExpiresAt); delta < sessionTTL-time.Minute || delta > sessionTTL {
		t.Fatal("session must expire in seven days")
	}

	resolved, err := service.ResolveSession(ctx, raw)
	if err != nil || CheckCSRF(resolved, admin.CSRFToken) != nil {
		t.Fatalf("session/CSRF resolution: %v", err)
	}
	if !errors.Is(CheckCSRF(resolved, ""), ErrCSRF) {
		t.Fatal("missing CSRF accepted")
	}

	if _, _, err := service.Register(ctx, createRequest("other@example.com"), raw); !errors.Is(err, ErrAlreadyAuthenticated) {
		t.Fatalf("authenticated registration: %v", err)
	}
	if _, _, err := service.Register(ctx, createRequest("ADMIN@example.com"), ""); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("duplicate email: %v", err)
	}

	code, err := service.JoinCode(ctx, resolved.AuthContext())
	if err != nil {
		t.Fatal(err)
	}

	join := createRequest("viewer@example.com")
	join.Organization = &OrganizationInput{Mode: "join", JoinCode: code.Code}
	viewer, viewerRaw, err := service.Register(ctx, join, "")
	if err != nil {
		t.Fatal(err)
	}
	if viewer.User.Role != RoleViewer || viewer.User.OrganizationID != admin.User.OrganizationID {
		t.Fatal("join must create a viewer of the selected organization")
	}

	viewerSession, _ := service.ResolveSession(ctx, viewerRaw)
	if _, err := service.JoinCode(ctx, viewerSession.AuthContext()); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer read code: %v", err)
	}
	if _, err := service.RotateJoinCode(ctx, viewerSession.AuthContext(), 1); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer rotate code: %v", err)
	}

	login := LoginRequest{Email: admin.User.Email, Password: "wrong-password"}
	if _, _, err := service.Login(ctx, login, raw); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("incorrect password: %v", err)
	}
	if _, err := service.ResolveSession(ctx, raw); err != nil {
		t.Fatal("failed login changed the existing session")
	}

	login.Password = "demo-password-2026"
	newView, newRaw, err := service.Login(ctx, login, raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveSession(ctx, raw); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("successful login must replace the old browser session")
	}

	_, otherRaw, err := service.Login(ctx, login, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Logout(ctx, newRaw, ""); !errors.Is(err, ErrCSRF) {
		t.Fatalf("logout without CSRF: %v", err)
	}
	if err := service.Logout(ctx, newRaw, newView.CSRFToken); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveSession(ctx, newRaw); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("logout did not revoke session")
	}
	if _, err := service.ResolveSession(ctx, otherRaw); err != nil {
		t.Fatal("logout revoked another browser")
	}
	if err := service.Logout(ctx, newRaw, ""); err != nil {
		t.Fatal("logout must be idempotent")
	}
}

func TestCodeRotationConcurrentAndOldCodeRejected(t *testing.T) {
	repo := newMemoryStore()
	service := NewService(repo)
	admin, raw, err := service.Register(t.Context(), createRequest("admin@example.com"), "")
	if err != nil {
		t.Fatal(err)
	}
	sess, _ := service.ResolveSession(t.Context(), raw)
	old, _ := service.JoinCode(t.Context(), sess.AuthContext())

	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			_, err := service.RotateJoinCode(t.Context(), sess.AuthContext(), 1)
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrJoinCodeChanged) {
				t.Errorf("rotation: %v", err)
			}
		})
	}
	wg.Wait()

	if successes.Load() != 1 {
		t.Fatalf("rotation winners=%d", successes.Load())
	}

	join := createRequest("viewer@example.com")
	join.Organization = &OrganizationInput{Mode: "join", JoinCode: old.Code}
	if _, _, err := service.Register(t.Context(), join, ""); !errors.Is(err, ErrInvalidJoinCode) {
		t.Fatalf("old code accepted: %v", err)
	}

	current, _ := service.ResolveSession(t.Context(), raw)
	if current.View.User != admin.User {
		t.Fatal("rotation changed existing user/session")
	}
}

func TestSessionExpiryAndDatabaseFailures(t *testing.T) {
	repo := newMemoryStore()
	service := NewService(repo)
	_, raw, err := service.Register(t.Context(), createRequest("a@example.com"), "")
	if err != nil {
		t.Fatal(err)
	}

	key := hex.EncodeToString(hashToken(raw))
	sess := repo.sessions[key]
	sess.ExpiresAt = time.Now().Add(-time.Second)
	repo.sessions[key] = sess

	if _, err := service.ResolveSession(t.Context(), raw); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expired session: %v", err)
	}

	repo.err = ErrUnavailable
	if _, err := service.ResolveSession(t.Context(), raw); !errors.Is(err, ErrUnavailable) {
		t.Fatal("database outage must not turn into unauthorized")
	}
}

func TestPasswordSupportsLongUnicodeAndLegacyHashes(t *testing.T) {
	password := strings.Repeat("я", 128)
	hash, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := checkPassword(hash, password)
	if err != nil || !ok || strings.Contains(hash, password) {
		t.Fatal("long Unicode password hash failed")
	}
	ok, err = checkPassword(hash, password+"x")
	if err != nil || ok {
		t.Fatal("password suffix must affect verification")
	}

	legacy, err := bcrypt.GenerateFromPassword([]byte("demo-password-2026"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	ok, err = checkPassword(string(legacy), "demo-password-2026")
	if err != nil || !ok {
		t.Fatal("legacy bcrypt hash must remain usable")
	}
}
