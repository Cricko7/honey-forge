package support

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
	"honey-forge/internal/platform/httpx"
	"honey-forge/modules/auth"
)

type fakeSender struct {
	messages []Message
	err      error
}

func (s *fakeSender) Send(_ context.Context, m Message) error {
	s.messages = append(s.messages, m)

	return s.err
}

func TestSendMessage(t *testing.T) {
	contract.Configure()

	const validBody = `{"email":"user@example.com","subject":"Ловушка","message":"Не запускается"}`

	for _, tt := range []struct {
		name            string
		body            string
		unauthenticated bool
		unconfigured    bool
		failed          bool
		status          int
	}{
		{
			name:   "viewer may contact support",
			body:   validBody,
			status: 204,
		},
		{
			name:            "unauthenticated",
			body:            validBody,
			unauthenticated: true,
			status:          401,
		},
		{
			name:   "malformed JSON",
			body:   `{`,
			status: 400,
		},
		{
			name:   "unknown recipient field",
			body:   `{"recipient":"other@example.com"}`,
			status: 400,
		},
		{
			name:   "invalid email",
			body:   `{"email":"bad","subject":"Help","message":"Details"}`,
			status: 422,
		},
		{
			name:   "header injection",
			body:   `{"email":"user@example.com","subject":"Hi\r\nBcc: other@example.com","message":"Details"}`,
			status: 422,
		},
		{
			name:   "blank message",
			body:   `{"email":"user@example.com","subject":"Help","message":" \n "}`,
			status: 422,
		},
		{
			name:         "SMTP unconfigured",
			body:         validBody,
			unconfigured: true,
			status:       503,
		},
		{
			name:   "SMTP failure",
			body:   validBody,
			failed: true,
			status: 503,
		},
		{
			name:   "message exceeds limit",
			body:   `{"email":"user@example.com","subject":"Help","message":"` + strings.Repeat("x", 5001) + `"}`,
			status: 422,
		},
		{
			name:   "request exceeds body limit",
			body:   strings.Repeat("x", httpx.MaxBodySize+1),
			status: 413,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sender := &fakeSender{}
			if tt.failed {
				sender.err = errors.New("private SMTP error")
			}

			var delivery Sender
			if !tt.unconfigured {
				delivery = sender
			}

			router := gin.New()
			router.Use(contract.Middleware())
			session := func(c *gin.Context) {
				if tt.unauthenticated {
					httpx.WriteError(c, 401, "unauthenticated", "Sign in")
					return
				}

				c.Set("auth_context", auth.AuthContext{UserID: "user", Role: "viewer"})
				c.Next()
			}

			NewHandler(NewService(delivery), slog.New(slog.NewTextHandler(io.Discard, nil))).Register(router, session)

			req := httptest.NewRequest("POST", "/api/support/messages", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			result := httptest.NewRecorder()
			router.ServeHTTP(result, req)

			if result.Code != tt.status {
				t.Fatalf("status %d: %s", result.Code, result.Body.String())
			}

			if tt.status != 204 && !tt.failed && len(sender.messages) != 0 {
				t.Fatal("sent invalid message")
			}

			if tt.status == 204 && len(sender.messages) != 1 {
				t.Fatal("message not delivered")
			}

			if strings.Contains(result.Body.String(), "private SMTP") {
				t.Fatal("leaked SMTP details")
			}
		})
	}
}

func TestServiceRequiresIdentity(t *testing.T) {
	sender := &fakeSender{}

	if err := NewService(sender).Send(t.Context(), auth.AuthContext{}, Message{Email: "user@example.com", Subject: "Help", Message: "Details"}); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("got %v", err)
	}

	if len(sender.messages) != 0 {
		t.Fatal("unauthorized delivery")
	}
}
