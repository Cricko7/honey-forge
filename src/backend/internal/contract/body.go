package contract

import (
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/gin-gonic/gin"
)

func readBody(c *gin.Context) ([]byte, bool) {
	b, err := io.ReadAll(io.LimitReader(c.Request.Body, MaxBodyBytes+1))
	if err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			Fail(c, NewError("body_too_large"))
		} else {
			Fail(c, NewError("invalid_json"))
		}
		return nil, false
	}
	if len(b) > MaxBodyBytes {
		Fail(c, NewError("body_too_large"))
		return nil, false
	}
	if len(b) > 0 {
		values := c.Request.Header.Values("Content-Type")
		if len(values) != 1 {
			Fail(c, NewError("unsupported_media_type"))
			return nil, false
		}
		media, _, err := mime.ParseMediaType(values[0])
		if err != nil || media != "application/json" {
			Fail(c, NewError("unsupported_media_type"))
			return nil, false
		}
	}
	return b, true
}
