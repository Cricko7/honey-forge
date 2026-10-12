package redistrap

import (
	"bufio"
	"context"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"honey-forge/modules/events"
)

func TestServerIdentityAcrossConcurrentConnections(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	r, err := Start(t.Context(), testSnapshot(port), "127.0.0.1", func(context.Context, events.AgentEvent) error {
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(r.Stop)

	identities := make(chan string, 4)
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			if err != nil {
				t.Error(err)
				return
			}

			defer conn.Close()

			if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Error(err)
				return
			}

			reader := bufio.NewReader(conn)
			if _, err := io.WriteString(conn, "AUTH bait-pass\r\nINFO server\r\n"); err != nil {
				t.Error(err)
				return
			}

			line, err := reader.ReadString('\n')
			if err != nil || line != "+OK\r\n" {
				t.Errorf("AUTH: %q %v", line, err)
				return
			}

			line, err = reader.ReadString('\n')
			if err != nil || !strings.HasPrefix(line, "$") {
				t.Errorf("INFO: %q %v", line, err)
				return
			}

			size, err := strconv.Atoi(strings.TrimSpace(line[1:]))
			if err != nil || size < 0 || size > 4096 {
				t.Errorf("invalid bulk size: %q", line)
				return
			}

			body := make([]byte, size+2)
			if _, err := io.ReadFull(reader, body); err != nil {
				t.Error(err)
				return
			}

			fields := map[string]string{}
			for _, line := range strings.Split(string(body), "\r\n") {
				if key, value, ok := strings.Cut(line, ":"); ok {
					fields[key] = value
				}
			}

			if fields["tcp_port"] != strconv.Itoa(port) || len(fields["run_id"]) != 40 || fields["uptime_in_seconds"] == "" {
				t.Errorf("inconsistent identity: %s", body)
				return
			}

			identities <- fields["run_id"]
		})
	}

	workers.Wait()
	close(identities)
	first, count := "", 0
	for id := range identities {
		if first != "" && id != first {
			t.Error("run_id changed between connections")
		}

		first = id
		count++
	}

	if count != 4 {
		t.Fatalf("completed probes = %d", count)
	}
}
