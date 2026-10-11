package agentws

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMTLSRejectsMissingOrMismatchedTrapCertificate(t *testing.T) {
	for _, name := range []string{"missing", "another-trap"} {
		t.Run(name, func(t *testing.T) {
			handler := NewHandler(&fakeGateway{token: "valid"}, &fakeCommands{})
			handler.ClientCAFile = "required"
			router := gin.New()
			handler.Register(router)
			request := httptest.NewRequest("GET", "https://resources.example/assets/stream", nil)
			request.Header.Set("Connection", "Upgrade")
			request.Header.Set("Upgrade", "websocket")
			request.Header.Set("Authorization", "Bearer valid")
			request.Header.Set("X-Agent-Certificate", "spoofed")
			if name != "missing" {
				request.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{Subject: pkix.Name{CommonName: name}}}}}
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != 200 || recorder.Header().Get("Content-Type") != "application/javascript; charset=utf-8" {
				t.Fatalf("missing/mismatched mTLS exposed stream: %d", recorder.Code)
			}
		})
	}
}
