package sshtrap

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

type fakeBackend struct {
	opened, closed atomic.Int32
	onRequest      func(*ssh.Request)
}

func (b *fakeBackend) Open(ctx context.Context, id string, seconds int) (*ssh.Client, func() error, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}
	defer listener.Close()
	client, err := (&net.Dialer{}).DialContext(ctx, "tcp", listener.Addr().String())
	if err != nil {
		return nil, nil, err
	}
	server, err := listener.Accept()
	if err != nil {
		client.Close()
		return nil, nil, err
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return nil, nil, err
	}
	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)
	files := sftp.InMemHandler()
	go func() {
		conn, channels, requests, err := ssh.NewServerConn(server, config)
		if err != nil {
			server.Close()
			return
		}
		defer conn.Close()
		go ssh.DiscardRequests(requests)
		for incoming := range channels {
			ch, reqs, err := incoming.Accept()
			if err != nil {
				return
			}
			go func() {
				defer ch.Close()
				for req := range reqs {
					if b.onRequest != nil {
						b.onRequest(req)
					}
					switch req.Type {
					case "pty-req", "window-change":
						req.Reply(true, nil)
					case "subsystem":
						var p struct{ Name string }
						if ssh.Unmarshal(req.Payload, &p) != nil || p.Name != "sftp" {
							req.Reply(false, nil)
							continue
						}
						req.Reply(true, nil)
						server := sftp.NewRequestServer(ch, files)
						server.Serve()
						server.Close()
						return
					case "exec":
						req.Reply(true, nil)
						ch.Write([]byte("real-backend-output\n"))
						ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
						return
					case "shell":
						req.Reply(true, nil)
						io.Copy(ch, ch)
						return
					default:
						req.Reply(false, nil)
					}
				}
			}()
		}
	}()
	conn, channels, requests, err := ssh.NewClientConn(client, "sandbox", &ssh.ClientConfig{User: "sandbox", HostKeyCallback: ssh.InsecureIgnoreHostKey()})
	if err != nil {
		client.Close()
		return nil, nil, err
	}
	b.opened.Add(1)
	return ssh.NewClient(conn, channels, requests), func() error { b.closed.Add(1); return nil }, nil
}

func testSnapshot(port int) profiles.Snapshot {
	return profiles.Snapshot{TypeID: "ssh-high-interaction", TypeVersion: 1, ProfileRevision: 1, Config: map[string]any{
		"services":   []any{map[string]any{"name": "ssh", "port": port, "username": "root", "password": "bait"}},
		"session":    map[string]any{"max_seconds": 10, "max_connections": 1, "max_recording_bytes": 8192, "max_upload_bytes": 8192},
		"management": map[string]any{"heartbeat_interval_seconds": 5, "telemetry_flush_interval_ms": 100},
	}}
}

func TestRuntimeAuthenticationRecordingAndCleanup(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	b := &fakeBackend{}
	var mu sync.Mutex
	var got []events.AgentEvent
	r, err := start(t.Context(), testSnapshot(port), "127.0.0.1", func(_ context.Context, e events.AgentEvent) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, e)
		return nil
	}, b)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Stop)
	dial := func(password string) (*ssh.Client, error) {
		return ssh.Dial("tcp", r.listener.Addr().String(), &ssh.ClientConfig{User: "root", Auth: []ssh.AuthMethod{ssh.Password(password)}, HostKeyCallback: ssh.InsecureIgnoreHostKey()})
	}
	if conn, err := dial("wrong"); err == nil {
		conn.Close()
		t.Fatal("wrong password accepted")
	}
	if b.opened.Load() != 0 {
		t.Fatal("unauthenticated caller allocated sandbox")
	}
	// Stop joins the rejected connection before the single-slot success case.
	// The client can observe an auth failure before server cleanup has finished.
	r.Stop()
	r, err = start(t.Context(), testSnapshot(port), "127.0.0.1", func(_ context.Context, e events.AgentEvent) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, e)
		return nil
	}, b)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Stop)
	client, err := dial("bait")
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	output, err := session.Output("id")
	if err != nil || string(output) != "real-backend-output\n" {
		t.Fatalf("output %q: %v", output, err)
	}
	if other, err := dial("bait"); err == nil {
		other.Close()
		t.Fatal("connection limit bypassed")
	}
	client.Close()
	r.Stop()
	if b.opened.Load() != 1 || b.closed.Load() != 1 {
		t.Fatalf("sandbox lifecycle %d/%d", b.opened.Load(), b.closed.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	streams := map[string]bool{}
	for i, e := range got {
		if e.SessionSequence != int64(i+1) {
			t.Fatalf("sequence %d at %d", e.SessionSequence, i)
		}
		var d struct {
			Stream string `json:"stream"`
		}
		json.Unmarshal(e.Data, &d)
		streams[d.Stream] = true
	}
	if len(got) < 4 || got[0].EventType != "service.connection_opened" || got[len(got)-1].EventType != "service.connection_closed" || !streams["exec"] || !streams["stdout"] {
		t.Fatalf("missing recording: %+v", got)
	}
}
