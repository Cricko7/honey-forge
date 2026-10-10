package contract

import (
	"bytes"
	"io"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// RESTBody is installed after authentication/Origin/CSRF/role middleware on
// protected groups, before route binding. It also covers routes without binding.
func RESTBody() gin.HandlerFunc {
	return func(c *gin.Context) {
		b, ok := readBody(c)
		if !ok {
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(b))
		c.Next()
	}
}

func Created(c *gin.Context, location string, body any) {
	u, err := url.Parse(location)
	if err != nil || u.IsAbs() || u.Host != "" || u.Fragment != "" || u.RawQuery != "" || len(u.Path) < 5 || u.Path[:5] != "/api/" {
		Fail(c, NewError("internal_error"))
		return
	}
	c.Header("Location", location)
	c.JSON(201, body)
}
func NoContent(c *gin.Context) { c.Status(204) }
func ProfileResponse(c *gin.Context, id ID, revision Revision, status int, body any) {
	c.Header("ETag", ProfileETag(id, revision))
	c.JSON(status, body)
}
func CatalogResponse(c *gin.Context, etag string, body any) {
	c.Header("Cache-Control", "private, max-age=0, must-revalidate")
	c.Header("ETag", etag)
	if matchesETag(strings.Join(c.Request.Header.Values("If-None-Match"), ","), etag) {
		c.Status(304)
		return
	}
	c.JSON(200, body)
}
func matchesETag(value, etag string) bool {
	for _, candidate := range strings.Split(value, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}
func CreatedOrReplayed(c *gin.Context, location string, body any, replayed bool) {
	if replayed {
		c.JSON(200, body)
		return
	}
	Created(c, location, body)
}
func RequestQuery(c *gin.Context, allowed ...string) (url.Values, bool) {
	v, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		Fail(c, NewError("invalid_query"))
		return nil, false
	}
	if e := CheckQuery(v, allowed...); e != nil {
		Fail(c, e)
		return nil, false
	}
	return v, true
}
func CheckTrapRevision(c *gin.Context, current Revision) bool {
	expected, ok := ExpectedTrapRevision(c)
	if !ok {
		return false
	}
	if expected != current {
		Fail(c, NewError("revision_mismatch"))
		return false
	}
	return true
}
