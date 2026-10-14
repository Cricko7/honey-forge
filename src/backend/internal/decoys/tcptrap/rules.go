package tcptrap

import "strings"

func (r Rule) matches(line string) bool {
	switch r.Match {
	case "exact":
		return line == r.Input
	case "prefix":
		return strings.HasPrefix(line, r.Input)
	case "contains":
		return strings.Contains(line, r.Input)
	case "any":
		return true
	default:
		return false
	}
}
