package http

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/goccy/go-yaml"

	"honey-forge/internal/configschema"
	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
	"honey-forge/modules/commands"
	"honey-forge/modules/commands/service"
	"honey-forge/modules/profiles"
)

const (
	httpOrg  = "22222222-2222-4222-8222-222222222222"
	httpUser = "11111111-1111-4111-8111-111111111111"
	httpTrap = "33333333-3333-4333-8333-333333333333"
	httpKey  = "44444444-4444-4444-8444-444444444444"
	httpID   = "55555555-5555-4555-8555-555555555555"
)

type testStore struct {
	command commands.Command
}

func (s *testStore) Create(ctx context.Context, _, trapID, requestID string, _ json.RawMessage, prepare func(context.Context, commands.Trap, commands.SnapshotReader) (commands.Prepared, error)) (commands.CreateResult, error) {
	if s.command.ID != "" {
		return commands.CreateResult{Command: s.command, Replayed: true}, nil
	}

	prepared, err := prepare(ctx, commands.Trap{ID: trapID, OrganizationID: httpOrg, TypeID: "tcp-banner", TypeVersion: 1}, noSnapshot{})
	if err != nil {
		return commands.CreateResult{}, err
	}

	s.command = commands.Command{ID: httpID, TrapID: trapID, RequestID: requestID, Action: prepared.Action, Params: prepared.Params, Status: commands.Queued, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(24 * time.Hour)}
	return commands.CreateResult{Command: s.command}, nil
}

func (s *testStore) Read(_ context.Context, _, trapID, commandID string) (commands.Command, error) {
	if s.command.ID != commandID || s.command.TrapID != trapID {
		return commands.Command{}, commands.ErrNotFound
	}

	return s.command, nil
}

func (s *testStore) List(_ context.Context, _, _ string, _ commands.ListQuery) ([]commands.Command, bool, error) {
	if s.command.ID == "" {
		return []commands.Command{}, false, nil
	}

	return []commands.Command{s.command}, false, nil
}

type noSnapshot struct{}

func (noSnapshot) Current(context.Context, string, string, int32) (profiles.Snapshot, error) {
	return profiles.Snapshot{}, commands.ErrNotFound
}

func testRouter(t *testing.T, store *testStore, role string) *gin.Engine {
	t.Helper()

	cursors, err := contract.NewCursorCodec([]byte("integration-cursor-key-32-bytes!"))
	if err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("auth_context", auth.AuthContext{UserID: httpUser, OrganizationID: httpOrg, Role: role})
		c.Next()
	})
	s := service.New(store, func(_ context.Context, _ string, _ int32, action string, params json.RawMessage) error {
		if action != "stop" || string(params) != `{}` {
			return commands.ErrInvalidParams
		}

		return nil
	})
	NewHandler(s, cursors, slog.New(slog.NewTextHandler(io.Discard, nil))).RegisterRoutes(r, func(c *gin.Context) { c.Next() })
	return r
}

func TestHTTPCreateReadAndReplay(t *testing.T) {
	store := &testStore{}
	r := testRouter(t, store, auth.RoleAdmin)
	body := `{"request_id":"` + httpKey + `","action":"stop","params":{}}`

	for _, tt := range []struct {
		name, method, path, body string
		status                   int
	}{
		{"create", "POST", "/api/traps/" + httpTrap + "/commands", body, 201},
		{"replay", "POST", "/api/traps/" + httpTrap + "/commands", body, 200},
		{"read", "GET", "/api/traps/" + httpTrap + "/commands/" + httpID, "", 200},
		{"list", "GET", "/api/traps/" + httpTrap + "/commands?status=queued", "", 200},
		{"wrong command", "GET", "/api/traps/" + httpTrap + "/commands/66666666-6666-4666-8666-666666666666", "", 404},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}

			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Fatalf("status %d, body %s", w.Code, w.Body.String())
			}

			if tt.name == "create" && w.Header().Get("Location") != "/api/traps/"+httpTrap+"/commands/"+httpID {
				t.Fatal("missing command location")
			}

			if tt.name == "replay" && w.Header().Get("Idempotency-Replayed") != "true" {
				t.Fatal("missing replay header")
			}
		})
	}
}

func TestHTTPRejectsInvalidAndViewer(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		role       string
		status     int
	}{
		{"viewer", `{"request_id":"` + httpKey + `","action":"stop","params":{}}`, auth.RoleViewer, 403},
		{"missing action", `{"request_id":"` + httpKey + `","params":{}}`, auth.RoleAdmin, 422},
		{"unknown field", `{"request_id":"` + httpKey + `","action":"stop","params":{},"secret":"x"}`, auth.RoleAdmin, 400},
		{"invalid params", `{"request_id":"` + httpKey + `","action":"stop","params":{"extra":true}}`, auth.RoleAdmin, 422},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := testRouter(t, &testStore{}, tt.role)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/traps/"+httpTrap+"/commands", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Fatalf("status %d, body %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestOpenAPICommandResponses(t *testing.T) {
	raw, err := os.ReadFile("../../../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}

	store := &testStore{}
	r := testRouter(t, store, auth.RoleAdmin)
	request := httptest.NewRequest(http.MethodPost, "/api/traps/"+httpTrap+"/commands", strings.NewReader(`{"request_id":"`+httpKey+`","action":"stop","params":{}}`))
	request.Header.Set("Content-Type", "application/json")
	created := httptest.NewRecorder()
	r.ServeHTTP(created, request)
	if created.Code != http.StatusCreated {
		t.Fatal(created.Body.String())
	}

	for _, tt := range []struct {
		name, ref, body string
	}{
		{"command", "Command", created.Body.String()},
		{"page", "CommandPage", func() string {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/traps/"+httpTrap+"/commands", nil))
			return w.Body.String()
		}()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := map[string]any{"$schema": "https://json-schema.org/draft/2020-12/schema", "components": document["components"], "$ref": "#/components/schemas/" + tt.ref}
			schemaRaw, err := json.Marshal(root)
			if err != nil {
				t.Fatal(err)
			}
			schema, err := configschema.Compile(schemaRaw)
			if err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate([]byte(tt.body), ""); err != nil {
				t.Fatalf("response violates OpenAPI: %v", err)
			}
		})
	}
}
