package sshtrap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"honey-forge/internal/contract"
	"honey-forge/modules/catalog"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

func TestSFTPUploadSurvivesDeletionAndMatchesCatalog(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	var mu sync.Mutex
	var captured []events.AgentEvent
	r, err := start(t.Context(), testSnapshot(port), "127.0.0.1", func(_ context.Context, e events.AgentEvent) error {
		mu.Lock()
		defer mu.Unlock()
		captured = append(captured, e)
		return nil
	}, &fakeBackend{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Stop)
	client, err := ssh.Dial("tcp", r.listener.Addr().String(), &ssh.ClientConfig{User: "root", Auth: []ssh.AuthMethod{ssh.Password("bait")}, HostKeyCallback: ssh.InsecureIgnoreHostKey()})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	remote, err := sftp.NewClient(client)
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	f, err := remote.Create("/upload.bin")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte{0, 255, 1, 2, 3}
	if _, err := f.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := remote.Remove("/upload.bin"); err != nil {
		t.Fatal(err)
	}
	remote.Close()
	client.Close()
	r.Stop()
	codec, err := contract.NewCursorCodec(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.NewService(catalog.BuiltinDefinitions(), codec)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	var block string
	var complete bool
	sum := sha256.Sum256(payload)
	for _, e := range captured {
		if err := cat.CheckEvent(t.Context(), e.TypeID, 1, e.EventType, e.Data); err != nil {
			t.Fatalf("catalog rejected %s: %s %v", e.EventType, e.Data, err)
		}
		if e.EventType == "service.file_chunk" {
			var d struct {
				Payload string `json:"payload_base64"`
			}
			json.Unmarshal(e.Data, &d)
			block += d.Payload
		}
		if e.EventType == "service.file_closed" {
			var d struct {
				Hash     string `json:"sha256"`
				Complete bool   `json:"complete"`
			}
			json.Unmarshal(e.Data, &d)
			complete = d.Complete && d.Hash == hex.EncodeToString(sum[:])
		}
	}
	if block != base64.StdEncoding.EncodeToString(payload) || !complete {
		t.Fatalf("missing artifact after deletion: %+v", captured)
	}
}

func TestSFTPRecordingAcrossMultiplePacketsAndDisconnect(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		name := "completed"
		if interrupted {
			name = "interrupted"
		}
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			var captured []events.AgentEvent
			r, client := startTestGateway(t, &fakeBackend{}, func(s *profiles.Snapshot) {
				s.Config["session"].(map[string]any)["max_upload_bytes"] = 1048576
			}, func(_ context.Context, e events.AgentEvent) error {
				mu.Lock()
				defer mu.Unlock()
				captured = append(captured, e)
				return nil
			})
			remote, err := sftp.NewClient(client)
			if err != nil {
				t.Fatal(err)
			}
			defer remote.Close()
			f, err := remote.Create("/large.bin")
			if err != nil {
				t.Fatal(err)
			}
			payload := bytes.Repeat([]byte{0, 255, 1, 2, 3, 128}, 12000)
			if n, err := f.Write(payload); err != nil || n != len(payload) {
				t.Fatalf("upload: %d %v", n, err)
			}
			if !interrupted {
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
				got, err := readSFTPFile(remote, "/large.bin")
				if err != nil || !bytes.Equal(got, payload) {
					t.Fatalf("download differs: %v", err)
				}
				if err := remote.Rename("/large.bin", "/renamed.bin"); err != nil {
					t.Fatal(err)
				}
				if err := remote.Remove("/renamed.bin"); err != nil {
					t.Fatal(err)
				}
			}
			client.Close() // the interrupted case deliberately leaves the file handle open
			r.Stop()
			if err := r.Err(); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			rebuilt := make([]byte, len(payload))
			chunks := 0
			closed := false
			for _, e := range captured {
				switch e.EventType {
				case "service.file_chunk":
					var d struct {
						Offset  int    `json:"offset"`
						Payload string `json:"payload_base64"`
					}
					if err := json.Unmarshal(e.Data, &d); err != nil {
						t.Fatal(err)
					}
					block, err := base64.StdEncoding.DecodeString(d.Payload)
					if err != nil || d.Offset < 0 || d.Offset > len(rebuilt)-len(block) {
						t.Fatalf("invalid block: %+v %v", d, err)
					}
					copy(rebuilt[d.Offset:], block)
					chunks++
				case "service.file_closed":
					var d struct {
						Size     int    `json:"size"`
						Hash     string `json:"sha256"`
						Complete bool   `json:"complete"`
					}
					if err := json.Unmarshal(e.Data, &d); err != nil {
						t.Fatal(err)
					}
					sum := sha256.Sum256(payload)
					if d.Size != len(payload) || d.Hash != hex.EncodeToString(sum[:]) || d.Complete == interrupted {
						t.Fatalf("wrong artifact status: %+v", d)
					}
					closed = true
				}
			}
			if chunks < 2 || !closed || !bytes.Equal(rebuilt, payload) {
				t.Fatal("recording cannot reconstruct uploaded bytes")
			}
		})
	}
}

func TestSeparateSSHConnectionsDoNotShareFiles(t *testing.T) {
	r, first := startTestGateway(t, &fakeBackend{}, func(s *profiles.Snapshot) {
		s.Config["session"].(map[string]any)["max_connections"] = 2
	}, nil)
	second, err := ssh.Dial("tcp", r.listener.Addr().String(), &ssh.ClientConfig{User: "root", Auth: []ssh.AuthMethod{ssh.Password("bait")}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	one, err := sftp.NewClient(first)
	if err != nil {
		t.Fatal(err)
	}
	defer one.Close()
	two, err := sftp.NewClient(second)
	if err != nil {
		t.Fatal(err)
	}
	defer two.Close()
	f, err := one.Create("/private.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("first connection")); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := two.Stat("/private.bin"); err == nil {
		t.Fatal("file visible to another SSH connection")
	}
	if got, err := readSFTPFile(one, "/private.bin"); err != nil || string(got) != "first connection" {
		t.Fatalf("owner cannot read file: %q %v", got, err)
	}
}

func readSFTPFile(client *sftp.Client, path string) ([]byte, error) {
	f, err := client.Open(path)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	return data, errors.Join(err, f.Close())
}
