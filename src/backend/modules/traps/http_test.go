package traps

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
)

const testID = "33333333-3333-4333-8333-333333333333"
const testOrg = "11111111-1111-4111-8111-111111111111"

type fakeStore struct {
	store
	record Record
}

func (f *fakeStore) Read(ctx context.Context, org, id string) (Record, error) {
	if org != f.record.OrganizationID || id != f.record.ID {
		return Record{}, contract.NewError("resource_not_found")
	}
	return f.record, nil
}
func (f *fakeStore) Update(ctx context.Context, org, id string, fn func(*Record) (string, error)) (Record, error) {
	r, err := f.Read(ctx, org, id)
	if err != nil {
		return Record{}, err
	}
	_, err = fn(&r)
	if err != nil {
		return Record{}, err
	}
	f.record = r
	return r, nil
}
func TestHTTPBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name, method, path, body, revision, role string
		status                                   int
	}{
		{"read", "GET", "/api/traps/" + testID, "", "", "viewer", 200},
		{"viewer patch", "PATCH", "/api/traps/" + testID, `{"name":"x"}`, "1", "viewer", 403},
		{"viewer credentials", "GET", "/api/traps/" + testID + "/agent-credentials", "", "", "viewer", 403},
		{"foreign", "GET", "/api/traps/44444444-4444-4444-8444-444444444444", "", "", "admin", 404},
		{"invalid id", "GET", "/api/traps/bad", "", "", "admin", 400},
		{"empty patch", "PATCH", "/api/traps/" + testID, `{}`, "1", "admin", 422},
		{"unknown field", "PATCH", "/api/traps/" + testID, `{"profile_id":"` + testID + `"}`, "1", "admin", 400},
		{"null field", "PATCH", "/api/traps/" + testID, `{"name":null}`, "1", "admin", 400},
		{"blank name", "PATCH", "/api/traps/" + testID, `{"name":"  "}`, "1", "admin", 422},
		{"stale revision", "PATCH", "/api/traps/" + testID, `{"name":"x"}`, "2", "admin", 412},
		{"missing revision", "PATCH", "/api/traps/" + testID, `{"name":"x"}`, "", "admin", 428},
		{"patch", "PATCH", "/api/traps/" + testID, `{"name":" x "}`, "1", "admin", 200},
		{"missing generation", "POST", "/api/traps/" + testID + "/agent-credentials", `{}`, "", "admin", 422},
		{"negative generation", "POST", "/api/traps/" + testID + "/agent-credentials", `{"expected_generation":-1}`, "", "admin", 422},
		{"fractional generation", "POST", "/api/traps/" + testID + "/agent-credentials", `{"expected_generation":0.5}`, "", "admin", 400},
		{"over generation", "POST", "/api/traps/" + testID + "/agent-credentials", `{"expected_generation":2147483647}`, "", "admin", 422},
		{"initial credentials", "POST", "/api/traps/" + testID + "/agent-credentials", `{"expected_generation":0}`, "", "admin", 200},
		{"extra query", "GET", "/api/traps/" + testID + "?secret=x", "", "", "admin", 400},
		{"read body", "GET", "/api/traps/" + testID, `{}`, "", "admin", 400},
		{"oversize", "PATCH", "/api/traps/" + testID, strings.Repeat("x", contract.MaxBodyBytes+1), "1", "admin", 413},
	} {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now().UTC()
			f := &fakeStore{record: Record{OrganizationID: testOrg, Trap: Trap{ID: testID, Name: "original", Revision: 1, StateVersion: 5, CreatedAt: now, UpdatedAt: now}}}
			s := NewService(f, nil, "wss://center.example/ws/agent", nil)
			router := gin.New()
			router.Use(contract.Middleware())
			session := func(c *gin.Context) {
				contract.SetPrincipal(c, contract.Principal{UserID: contract.NewID(), OrganizationID: contract.ID(testOrg), Role: contract.Role(tt.role)})
				c.Next()
			}
			NewHandler(s, nil, nil).RegisterRoutes(router, session)
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			if tt.revision != "" {
				req.Header.Set("X-Expected-Revision", tt.revision)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			if strings.HasSuffix(tt.path, testID) && w.Header().Get("ETag") != "" {
				t.Fatal("Trap ETag must be absent")
			}
		})
	}
}
func TestCredentialsLifecycle(t *testing.T) {
	f := &fakeStore{record: Record{OrganizationID: testOrg, Trap: Trap{ID: testID, Revision: 1, StateVersion: 1, Connectivity: "offline"}}}
	ctx := contract.WithPrincipal(t.Context(), contract.Principal{UserID: contract.NewID(), OrganizationID: contract.ID(testOrg), Role: contract.Admin})
	s := NewService(f, nil, "wss://center.example/ws/agent", nil)
	status, err := s.Credentials(ctx, testID)
	requireCode(t, err, "")
	if status.Generation != 0 || status.Active || status.IssuedAt != nil {
		t.Fatal(status)
	}
	first, err := s.IssueCredentials(ctx, testID, 0)
	requireCode(t, err, "")
	if first.Generation != 1 || len(first.Token) < 32 || f.record.StateVersion != 1 {
		t.Fatal("bad issuance")
	}
	_, err = s.IssueCredentials(ctx, testID, 0)
	requireCode(t, err, "agent_credentials_changed")
	status, err = s.Credentials(ctx, testID)
	requireCode(t, err, "")
	raw, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), first.Token) || strings.Contains(string(raw), "token") {
		t.Fatal("token leaked")
	}
	second, err := s.IssueCredentials(ctx, testID, 1)
	requireCode(t, err, "")
	if second.Token == first.Token {
		t.Fatal("token reused")
	}
	requireCode(t, s.RevokeCredentials(ctx, testID, CredentialsETag(testID, 1)), "agent_credentials_changed")
	requireCode(t, s.RevokeCredentials(ctx, testID, CredentialsETag(testID, 2)), "")
	if f.record.Generation != 3 || len(f.record.TokenHash) != 0 {
		t.Fatal("not revoked")
	}
	requireCode(t, s.RevokeCredentials(ctx, testID, CredentialsETag(testID, 3)), "")
	if f.record.Generation != 3 {
		t.Fatal("no-op bumped generation")
	}
}
