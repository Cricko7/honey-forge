package contract

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func ProfileETag(id ID, revision Revision) string {
	return fmt.Sprintf(`"profile:%s:%d"`, id, revision)
}

// Preconditions only parse/compare headers. Features must compare and write atomically in their repository.
func CheckProfileRevision(c *gin.Context, id ID, current Revision) bool {
	revision, ok := ExpectedProfileRevision(c, id)
	if !ok {
		return false
	}
	if revision != current || strings.TrimSpace(c.GetHeader("If-Match")) != ProfileETag(id, current) {
		Fail(c, NewError("revision_mismatch"))
		return false
	}
	return true
}

// ExpectedProfileRevision is parsed before the repository performs its atomic compare-and-write.
func ExpectedProfileRevision(c *gin.Context, id ID) (Revision, bool) {
	values := c.Request.Header.Values("If-Match")
	if len(values) == 0 {
		Fail(c, NewError("precondition_required"))
		return 0, false
	}
	if len(values) != 1 {
		Fail(c, NewError("invalid_precondition"))
		return 0, false
	}
	value := strings.TrimSpace(values[0])
	prefix := `"profile:`
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, `"`) {
		Fail(c, NewError("invalid_precondition"))
		return 0, false
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(value, prefix), `"`), ":")
	if len(parts) != 2 || !ValidID(parts[0]) {
		Fail(c, NewError("invalid_precondition"))
		return 0, false
	}
	revision, err := parseRevision(parts[1])
	if err != nil {
		Fail(c, NewError("invalid_precondition"))
		return 0, false
	}
	if parts[0] != string(id) || parts[1] != strconv.FormatInt(int64(revision), 10) {
		Fail(c, NewError("revision_mismatch"))
		return 0, false
	}
	return revision, true
}
func ExpectedTrapRevision(c *gin.Context) (Revision, bool) {
	values := c.Request.Header.Values("X-Expected-Revision")
	if len(values) == 0 {
		Fail(c, NewError("precondition_required"))
		return 0, false
	}
	if len(values) != 1 {
		Fail(c, NewError("invalid_precondition"))
		return 0, false
	}
	r, err := parseRevision(values[0])
	if err != nil {
		Fail(c, NewError("invalid_precondition"))
		return 0, false
	}
	return r, true
}
func parseRevision(s string) (Revision, error) {
	if s == "" {
		return 0, fmt.Errorf("empty revision")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid revision")
		}
	}
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("invalid revision")
	}
	return Revision(n), nil
}
func RequireID(c *gin.Context, name string) (ID, bool) {
	s := c.Param(name)
	if !ValidID(s) {
		Fail(c, NewError("invalid_id"))
		return "", false
	}
	return ID(strings.ToLower(s)), true
}
