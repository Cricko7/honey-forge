package contract

import (
	"net/url"
	"strconv"
	"time"
	"unicode/utf8"
)

type ListQuery struct {
	Limit  int
	Cursor string
}

// CheckQuery rejects duplicates and unknown keys before a feature parses its filters.
func CheckQuery(values url.Values, allowed ...string) *Error {
	keys := map[string]bool{}
	for _, key := range allowed {
		keys[key] = true
	}
	for key, v := range values {
		if !keys[key] || len(v) != 1 {
			e := NewError("invalid_query")
			e.Fields = []FieldError{{Path: "/query/" + escapePointer(key), Code: "invalid_parameter", Message: "Unknown or repeated query parameter"}}
			return e
		}
	}
	return nil
}

func ParseListQuery(values url.Values, filters ...string) (ListQuery, *Error) {
	allowed := append([]string{"limit", "cursor"}, filters...)
	if e := CheckQuery(values, allowed...); e != nil {
		return ListQuery{}, e
	}
	q := ListQuery{Limit: 50, Cursor: values.Get("cursor")}
	if v, ok := values["limit"]; ok {
		for _, r := range v[0] {
			if r < '0' || r > '9' {
				return ListQuery{}, NewError("invalid_query")
			}
		}
		n, err := strconv.Atoi(v[0])
		if err != nil || n < 1 || n > 100 {
			return ListQuery{}, NewError("invalid_query")
		}
		q.Limit = n
	}
	if v, ok := values["cursor"]; ok && (v[0] == "" || !utf8.ValidString(v[0]) || utf8.RuneCountInString(v[0]) > MaxCursorLength) {
		return ListQuery{}, NewError("invalid_cursor")
	}
	return q, nil
}

type TimeRange struct {
	From *time.Time
	To   *time.Time
}

func ParseTimeRange(values url.Values) (TimeRange, *Error) {
	var result TimeRange
	for key, target := range map[string]**time.Time{"from": &result.From, "to": &result.To} {
		v, ok := values[key]
		if !ok {
			continue
		}
		if len(v) != 1 {
			return TimeRange{}, NewError("invalid_query")
		}
		t, err := time.Parse(time.RFC3339Nano, v[0])
		if err != nil {
			return TimeRange{}, NewError("invalid_query")
		}
		*target = &t
	}
	if result.From != nil && result.To != nil && !result.From.Before(*result.To) {
		return TimeRange{}, NewError("invalid_time_range")
	}
	return result, nil
}
