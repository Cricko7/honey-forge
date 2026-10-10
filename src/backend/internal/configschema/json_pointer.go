package configschema

import (
	"strconv"
	"strings"
)

func escape(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1") }
func parts(path string) []string {
	if path == "" {
		return nil
	}
	p := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, s := range p {
		p[i] = strings.ReplaceAll(strings.ReplaceAll(s, "~1", "/"), "~0", "~")
	}
	return p
}

func at(root any, path string) (any, bool) {
	value := root
	for _, key := range parts(path) {
		switch container := value.(type) {
		case map[string]any:
			var exists bool
			value, exists = container[key]
			if !exists {
				return nil, false
			}
		case []any:
			i, err := strconv.Atoi(key)
			if err != nil || i < 0 || i >= len(container) {
				return nil, false
			}
			value = container[i]
		default:
			return nil, false
		}
	}
	return value, true
}

func put(root map[string]any, path string, value any) bool {
	p := parts(path)
	if len(p) == 0 {
		return false
	}
	var current any = root
	for i, key := range p {
		last := i == len(p)-1
		switch container := current.(type) {
		case map[string]any:
			if last {
				container[key] = value
				return true
			}
			next, exists := container[key]
			if !exists {
				next = map[string]any{}
				container[key] = next
			}
			current = next
		case []any:
			index, err := strconv.Atoi(key)
			if err != nil || index < 0 || index >= len(container) {
				return false
			}
			if last {
				container[index] = value
				return true
			}
			current = container[index]
		default:
			return false
		}
	}
	return false
}

func remove(root map[string]any, path string) {
	p := parts(path)
	if len(p) == 0 {
		clear(root)
		return
	}
	parentPath := ""
	for _, key := range p[:len(p)-1] {
		parentPath += "/" + escape(key)
	}
	parent, ok := at(root, parentPath)
	if !ok {
		return
	}
	key := p[len(p)-1]
	switch container := parent.(type) {
	case map[string]any:
		delete(container, key)
	case []any:
		i, err := strconv.Atoi(key)
		if err == nil && i >= 0 && i < len(container) {
			container[i] = nil
		}
	}
}
