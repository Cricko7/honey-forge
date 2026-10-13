package redistrap

import (
	"bufio"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestObserveCommands(t *testing.T) {
	for _, tc := range []struct {
		name, wire         string
		args               []string
		invalid, truncated bool
	}{
		{"inline quotes", "AUTH \"bait pass\"\r\n", []string{"AUTH", "bait pass"}, false, false},
		{"inline escapes", "ECHO \"a\\x00b\"\r\n", []string{"ECHO", "a\x00b"}, false, false},
		{"binary", "*2\r\n$4\r\nECHO\r\n$3\r\na\x00b\r\n", []string{"ECHO", "a\x00b"}, false, false},
		{"malformed", "*bad\r\n", nil, true, false},
		{"partial", "*2\r\n$4\r\nAUTH\r\n$5\r\nab", []string{"AUTH"}, true, true},
		{"unclosed quote", "AUTH \"oops\r\n", []string{"AUTH"}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, count, truncated, err := readCommand(bufio.NewReader(strings.NewReader(tc.wire)))
			if (err != nil) != tc.invalid || count != int64(len(tc.wire)) || truncated != tc.truncated || !reflect.DeepEqual(args, tc.args) {
				t.Fatalf("args=%q count=%d truncated=%v err=%v", args, count, truncated, err)
			}
		})
	}
}

func TestLargeBulkDoesNotDesynchronizeObserver(t *testing.T) {
	value := strings.Repeat("x", 128<<10)
	wire := fmt.Sprintf("*3\r\n$3\r\nSET\r\n$4\r\ndemo\r\n$%d\r\n%s\r\n", len(value), value)
	r := bufio.NewReader(strings.NewReader(wire + "PING\r\n"))
	args, count, truncated, err := readCommand(r)
	if err != nil || !truncated || count != int64(len(wire)) || len(args[2]) > 8192 {
		t.Fatalf("large frame: count=%d truncated=%v err=%v", count, truncated, err)
	}

	args, count, truncated, err = readCommand(r)
	if err != nil || truncated || count != 6 || !reflect.DeepEqual(args, []string{"PING"}) {
		t.Fatalf("following frame: args=%q count=%d truncated=%v err=%v", args, count, truncated, err)
	}
}
