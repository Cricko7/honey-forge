package profiles

import (
	"strconv"
	"strings"
)

func pointerParts(path string) ([]string, bool) {
	if !strings.HasPrefix(path, "/") {
		return nil, false
	}

	parts := strings.Split(path[1:], "/")
	for i, p := range parts {
		for j := 0; j < len(p); j++ {
			if p[j] == '~' {
				if j+1 >= len(p) || (p[j+1] != '0' && p[j+1] != '1') {
					return nil, false
				}

				j++
			}
		}

		parts[i] = strings.ReplaceAll(strings.ReplaceAll(p, "~1", "/"), "~0", "~")
	}

	return parts, true
}

func arrayIndex(part string, length int) (int, bool) {
	i, err := strconv.Atoi(part)
	return i, err == nil && i >= 0 && i < length && strconv.Itoa(i) == part
}

func PointerGet(value any, path string) (any, bool) {
	parts, ok := pointerParts(path)
	if !ok {
		return nil, false
	}

	for _, part := range parts {
		switch v := value.(type) {
		case map[string]any:
			value, ok = v[part]
		case []any:
			i, valid := arrayIndex(part, len(v))
			ok = valid
			if valid {
				value = v[i]
			}
		default:
			return nil, false
		}

		if !ok {
			return nil, false
		}
	}

	return value, true
}

func pointerPut(object, template Object, path string, value any) bool {
	parts, ok := pointerParts(path)
	if !ok {
		return false
	}

	_, ok = putPart(object, template, parts, value)
	return ok
}

func putPart(node, template any, parts []string, value any) (any, bool) {
	if len(parts) == 0 {
		return value, true
	}

	if node == nil {
		node = secretContainer(template)
	}

	switch n := node.(type) {
	case map[string]any:
		current, exists := n[parts[0]]
		if exists && current == nil && len(parts) > 1 {
			return nil, false
		}

		var previous any
		if source, ok := template.(map[string]any); ok {
			previous = source[parts[0]]
		}

		child, ok := putPart(current, previous, parts[1:], value)
		if !ok {
			return nil, false
		}

		n[parts[0]] = child
		return n, true
	case []any:
		i, ok := arrayIndex(parts[0], len(n))
		if !ok {
			return nil, false
		}

		if n[i] == nil && len(parts) > 1 {
			return nil, false
		}

		var previous any
		if source, ok := template.([]any); ok && i < len(source) {
			previous = source[i]
		}

		child, ok := putPart(n[i], previous, parts[1:], value)
		if !ok {
			return nil, false
		}

		n[i] = child
		return n, true
	default:
		return nil, false
	}
}

func secretContainer(template any) any {
	if previous, ok := template.([]any); ok {
		array := make([]any, len(previous))
		for i, v := range previous {
			switch v.(type) {
			case map[string]any, []any:
				array[i] = secretContainer(v)
			}
		}

		return array
	}

	return Object{}
}

func pointerRemove(object Object, path string) {
	parts, ok := pointerParts(path)
	if !ok {
		return
	}

	var parent any = object
	if len(parts) > 1 {
		prefix := "/" + strings.Join(strings.Split(path[1:], "/")[:len(parts)-1], "/")
		parent, ok = PointerGet(object, prefix)
		if !ok {
			return
		}
	}

	last := parts[len(parts)-1]
	switch p := parent.(type) {
	case map[string]any:
		delete(p, last)
	case []any:
		if i, ok := arrayIndex(last, len(p)); ok {
			p[i] = nil
		}
	}
}
