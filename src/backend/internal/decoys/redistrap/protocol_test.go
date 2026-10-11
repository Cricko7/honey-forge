package redistrap

import (
	"strings"
	"testing"
)

func TestProbeResponses(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"info server", []string{"INFO", "server"}, "redis_version:7.2.0\r\n"},
		{"info unknown section", []string{"INFO", "missing"}, "$0\r\n\r\n"},
		{"info keyspace", []string{"INFO", "keyspace"}, "db0:keys=3,expires=0,avg_ttl=0\r\n"},
		{"role", []string{"ROLE"}, "*3\r\n$6\r\nmaster\r\n:0\r\n*0\r\n"},
		{"unknown casing", []string{"MiSsInG"}, "unknown command 'MiSsInG'"},
		{"unknown injection", []string{"bad\r\n+OK"}, "unknown command 'bad\\r\\n+OK'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply, _, _ := newDatabase().execute(tc.args)
			if !strings.Contains(reply, tc.want) {
				t.Fatalf("reply %q, want %q", reply, tc.want)
			}
			if tc.name == "info server" && strings.Contains(reply, "# Keyspace") {
				t.Fatal("INFO server included another section")
			}
		})
	}
}
