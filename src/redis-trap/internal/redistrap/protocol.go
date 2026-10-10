package redistrap

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

var errFrame = errors.New("invalid Redis command frame")

// readFrame accepts RESP2 arrays and inline commands, with a strict per-frame cap.
func readFrame(reader *bufio.Reader) ([]string, int, error) {
	line, err := reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		return nil, len(line), errFrame
	}
	if err != nil {
		return nil, len(line), err
	}
	count := len(line)
	if count > 4096 || !strings.HasSuffix(string(line), "\r\n") {
		return nil, count, errFrame
	}
	if line[0] != '*' {
		if !utf8.Valid(line) {
			return nil, count, errFrame
		}
		args := strings.Fields(string(line[:len(line)-2]))
		if len(args) == 0 || len(args) > 16 {
			return nil, count, errFrame
		}
		return args, count, nil
	}
	items, err := strconv.Atoi(string(line[1 : len(line)-2]))
	if err != nil || items < 1 || items > 16 {
		return nil, count, errFrame
	}
	args := make([]string, 0, items)
	for range items {
		header, err := reader.ReadSlice('\n')
		count += len(header)
		if err != nil || len(header) < 4 || header[0] != '$' || !strings.HasSuffix(string(header), "\r\n") {
			return nil, count, errFrame
		}
		size, err := strconv.Atoi(string(header[1 : len(header)-2]))
		if err != nil || size < 0 || size > 4096 || count+size+2 > 8192 {
			return nil, count, errFrame
		}
		bulk := make([]byte, size+2)
		n, err := io.ReadFull(reader, bulk)
		count += n
		if err != nil || string(bulk[size:]) != "\r\n" || !utf8.Valid(bulk[:size]) {
			return nil, count, errFrame
		}
		args = append(args, string(bulk[:size]))
	}
	return args, count, nil
}

func bulk(value string) string { return fmt.Sprintf("$%d\r\n%s\r\n", len(value), value) }

type database struct {
	values map[string]string
}

func newDatabase() *database {
	return &database{values: map[string]string{"app:mode": "staging", "backup:note": "nightly snapshot pending", "users:admin": "disabled"}}
}

func (d *database) execute(args []string) (string, bool, bool) {
	command := strings.ToUpper(args[0])
	wrongArgs := "-ERR wrong number of arguments for '" + strings.ToLower(command) + "' command\r\n"
	switch command {
	case "PING":
		if len(args) == 1 {
			return "+PONG\r\n", true, false
		}
		if len(args) == 2 {
			return bulk(args[1]), true, false
		}
	case "ECHO":
		if len(args) == 2 {
			return bulk(args[1]), true, false
		}
	case "GET":
		if len(args) == 2 {
			if value, ok := d.values[args[1]]; ok {
				return bulk(value), true, false
			}
			return "$-1\r\n", true, false
		}
	case "SET":
		if len(args) == 3 {
			d.values[args[1]] = args[2]
			return "+OK\r\n", true, false
		}
	case "DEL":
		if len(args) >= 2 {
			deleted := 0
			for _, key := range args[1:] {
				if _, ok := d.values[key]; ok {
					delete(d.values, key)
					deleted++
				}
			}
			return fmt.Sprintf(":%d\r\n", deleted), true, false
		}
	case "EXISTS":
		if len(args) >= 2 {
			found := 0
			for _, key := range args[1:] {
				if _, ok := d.values[key]; ok {
					found++
				}
			}
			return fmt.Sprintf(":%d\r\n", found), true, false
		}
	case "DBSIZE":
		if len(args) == 1 {
			return fmt.Sprintf(":%d\r\n", len(d.values)), true, false
		}
	case "KEYS":
		if len(args) == 2 {
			var keys []string
			for key := range d.values {
				if matched, err := path.Match(args[1], key); err == nil && matched {
					keys = append(keys, key)
				}
			}
			sort.Strings(keys)
			var result strings.Builder
			fmt.Fprintf(&result, "*%d\r\n", len(keys))
			for _, key := range keys {
				result.WriteString(bulk(key))
			}
			return result.String(), true, false
		}
	case "INFO":
		if len(args) == 1 || len(args) == 2 {
			return bulk("# Server\r\nredis_version:7.2.0\r\nredis_mode:standalone\r\n# Keyspace\r\ndb0:keys=" + strconv.Itoa(len(d.values)) + "\r\n"), true, false
		}
	case "SELECT":
		if len(args) == 2 {
			if args[1] == "0" {
				return "+OK\r\n", true, false
			}
			return "-ERR DB index is out of range\r\n", false, false
		}
	case "QUIT":
		if len(args) == 1 {
			return "+OK\r\n", true, true
		}
	default:
		return "-ERR unknown command '" + strings.ToLower(command) + "'\r\n", false, false
	}
	return wrongArgs, false, false
}
