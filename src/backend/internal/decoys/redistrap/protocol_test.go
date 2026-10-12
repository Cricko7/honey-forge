package redistrap

import (
	"fmt"
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

func TestDatabaseStorageLimits(t *testing.T) {
	t.Run("distinct keys", func(t *testing.T) {
		db := newDatabase()
		for i := len(db.values); i < 256; i++ {
			reply, accepted, _ := db.execute([]string{"SET", fmt.Sprintf("key:%d", i), "value"})
			if !accepted || reply != "+OK\r\n" {
				t.Fatalf("SET below limit rejected: %q", reply)
			}
		}

		reply, accepted, _ := db.execute([]string{"SET", "overflow", "value"})
		if accepted || !strings.HasPrefix(reply, "-OOM ") || len(db.values) != 256 {
			t.Fatalf("unbounded key storage: accepted=%v keys=%d reply=%q", accepted, len(db.values), reply)
		}

		if _, accepted, _ := db.execute([]string{"SET", "app:mode", "updated"}); !accepted {
			t.Fatal("overwrite at key limit rejected")
		}

		db.execute([]string{"DEL", "app:mode"})
		if _, accepted, _ := db.execute([]string{"SET", "replacement", "value"}); !accepted {
			t.Fatal("deletion did not release key capacity")
		}
	})

	t.Run("stored bytes", func(t *testing.T) {
		db := newDatabase()
		value := strings.Repeat("x", 4096)
		blocked := false
		for i := 0; i < 100; i++ {
			key := fmt.Sprintf("key:%d", i)
			reply, accepted, _ := db.execute([]string{"SET", key, value})
			if !accepted {
				if !strings.HasPrefix(reply, "-OOM ") {
					t.Fatalf("unexpected limit reply: %q", reply)
				}

				blocked = true
				break
			}
		}

		if !blocked {
			t.Fatal("SET did not bound stored bytes")
		}

		before := db.values["key:0"]
		if _, accepted, _ := db.execute([]string{"SET", "key:0", strings.Repeat("x", 8192)}); accepted || db.values["key:0"] != before {
			t.Fatal("over-budget overwrite changed existing value")
		}

		db.execute([]string{"DEL", "key:0"})
		if _, accepted, _ := db.execute([]string{"SET", "key:0", value}); !accepted {
			t.Fatal("deletion did not release byte capacity")
		}
	})
}
