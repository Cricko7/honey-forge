package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	authhttp "github.com/Cricko7/honey-forge/src/backend/modules/auth/http"
)

func TestOriginMustExactlyMatchConfiguredOrigin(t *testing.T) {
	cases := []struct {
		name    string
		origins []string
		status  int
	}{
		{"missing", nil, 403},
		{"null", []string{"null"}, 403},
		{"attacker", []string{"https://evil.example"}, 403},
		{"prefix", []string{"https://example.com.evil.example"}, 403},
		{"scheme", []string{"http://example.com"}, 403},
		{"path", []string{"https://example.com/"}, 403},
		{"duplicate", []string{"https://example.com", "https://example.com"}, 403},
		{"allowed", []string{"https://example.com"}, 204},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := testRouter(t, io.Discard)
			request := httptest.NewRequest("DELETE", "https://spoofed-host.example/api/session", nil)
			for _, origin := range tc.origins {
				request.Header.Add("Origin", origin)
			}

			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if tc.status == 403 {
				var body struct {
					RequestID string `json:"request_id"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if len(body.RequestID) != 36 || body.RequestID != response.Header().Get("X-Request-ID") {
					t.Fatal("error/header must contain the same UUID request ID")
				}
			}
		})
	}
}

func TestBearerAndLegacyCookiesDoNotAuthenticate(t *testing.T) {
	router := testRouter(t, io.Discard)
	request := httptest.NewRequest("GET", "/api/session", nil)
	request.Header.Set("Authorization", "Bearer token")
	request.AddCookie(&http.Cookie{Name: "access_token", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != 401 {
		t.Fatal("only __Host-session may authenticate operator requests")
	}

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx, ok := authhttp.Context(context)
	if ok || ctx.UserID != "" {
		t.Fatal("identity without verified middleware must be absent")
	}
}
