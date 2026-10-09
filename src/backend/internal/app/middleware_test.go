package app

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPanicRecoveryDoesNotExposePanicSecrets(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	router := gin.New()
	router.Use(requestLogger(logger), recovery(logger))

	router.GET("/panic", func(c *gin.Context) {
		panic("password-secret")
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("panic response status = %d", response.Code)
	}

	if strings.Contains(output.String(), "password-secret") || strings.Contains(response.Body.String(), "password-secret") {
		t.Fatal("panic values must not be exposed in responses or logs")
	}
}
