package httpx

import (
	"github.com/gin-gonic/gin"

	"honey-forge/internal/platform/validation"
)

type FieldError = validation.FieldError

type Error struct {
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Fields  []FieldError `json:"fields,omitempty"`
}

func WriteError(c *gin.Context, status int, code, message string, fields ...FieldError) {
	c.Set("error_code", code)
	c.AbortWithStatusJSON(status, gin.H{
		"error":      Error{Code: code, Message: message, Fields: fields},
		"request_id": c.GetString("request_id"),
	})
}
