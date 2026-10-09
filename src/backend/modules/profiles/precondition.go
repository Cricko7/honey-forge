package profiles

import (
	"fmt"
	"strconv"
	"strings"
)

func ETag(p Profile) string { return fmt.Sprintf(`"profile:%s:%d"`, p.ID, p.Revision) }

func Precondition(p Profile, value string) error {
	if value == "" {
		return ErrPreconditionRequired
	}

	if !strings.HasPrefix(value, `"profile:`) || !strings.HasSuffix(value, `"`) {
		return ErrInvalidPrecondition
	}

	parts := strings.Split(value[1:len(value)-1], ":")
	if len(parts) != 3 || !ValidID(parts[1]) {
		return ErrInvalidPrecondition
	}

	rev, err := strconv.ParseInt(parts[2], 10, 32)
	if err != nil || rev < 1 || strconv.FormatInt(rev, 10) != parts[2] {
		return ErrInvalidPrecondition
	}

	if value != ETag(p) {
		return ErrRevisionMismatch
	}

	return nil
}
