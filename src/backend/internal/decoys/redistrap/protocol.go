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
	"time"
	"unicode/utf8"

	"honey-forge/internal/contract"
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

func bulk(value string) string {
	return fmt.Sprintf("$%d\r\n%s\r\n", len(value), value)
}

type database struct {
	values   map[string]string
	bytes    int
	identity serverIdentity
}

func newDatabase() *database {
	d := &database{values: map[string]string{"app:mode": "staging", "backup:note": "nightly snapshot pending", "users:admin": "disabled"}, identity: newServerIdentity(6379)}
	for key, value := range d.values {
		d.bytes += len(key) + len(value)
	}

	return d
}

type serverIdentity struct {
	runID   string
	port    int
	started time.Time
}

func newServerIdentity(port int) serverIdentity {
	id := strings.ReplaceAll(string(contract.NewID()), "-", "")
	extra := strings.ReplaceAll(string(contract.NewID()), "-", "")

	return serverIdentity{runID: id + extra[:8], port: port, started: time.Now()}
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
			previous, exists := d.values[args[1]]
			growth := len(args[2]) - len(previous)
			if !exists {
				growth += len(args[1])
			}

			// Frame limits do not bound data retained across repeated SETs.
			if !exists && len(d.values) >= 256 || growth > 256*1024-d.bytes {
				return "-OOM command not allowed when used memory > 'maxmemory'.\r\n", false, false
			}

			d.bytes += growth
			d.values[args[1]] = args[2]

			return "+OK\r\n", true, false
		}
	case "DEL":
		if len(args) >= 2 {
			deleted := 0
			for _, key := range args[1:] {
				if value, ok := d.values[key]; ok {
					d.bytes -= len(key) + len(value)
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
		return bulk(d.info(args[1:])), true, false
	case "ROLE":
		if len(args) == 1 {
			return "*3\r\n$6\r\nmaster\r\n:0\r\n*0\r\n", true, false
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
		// Quote untrusted bytes so a command cannot inject another RESP reply.
		quoted := strconv.Quote(args[0])
		return "-ERR unknown command '" + quoted[1:len(quoted)-1] + "', with args beginning with: \r\n", false, false
	}

	return wrongArgs, false, false
}
