package enginepipe

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"honey-forge/modules/events"
)

type testPipe struct{ bytes.Buffer }

func (*testPipe) Close() error { return nil }

func TestPythonEnvironmentExcludesManagementSecrets(t *testing.T) {
	env := strings.Join(Environment([]string{"PATH=/usr/bin", "NM_TOKEN=secret", "NM_WS_URL=wss://api.internal", "NM_LOCAL_TOKEN=pipe-secret", "PYTHONPATH=/evil", "HTTP_PROXY=http://proxy.internal", "SYSTEMROOT=C:/Windows"}, "conpot"), " ")
	if !strings.Contains(env, "PATH=/usr/bin") {
		t.Fatal(env)
	}
	for _, secret := range []string{"secret", "api.internal", "PYTHONPATH", "HTTP_PROXY"} {
		if strings.Contains(env, secret) {
			t.Fatalf("child environment leaks %s", secret)
		}
	}
}

func TestPythonEnvironmentPreservesWindowsRuntimePaths(t *testing.T) {
	for _, entry := range []string{"SYSTEMDRIVE=C:", "USERPROFILE=C:/Users/test", "LOCALAPPDATA=C:/Users/test/Local", "APPDATA=C:/Users/test/Roaming", "TEMP=C:/Temp", "TMP=C:/Temp"} {
		env := Environment([]string{entry}, "conpot")
		found := false
		for _, value := range env {
			if value == entry {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing OS runtime path %s", entry)
		}
	}
}

func TestPipeRejectsUntrustedFramesBeforeAcknowledgement(t *testing.T) {
	for _, tc := range []struct{ name, input string }{
		{"event before ready", `{"kind":"opened"}`},
		{"duplicate ready", "{\"kind\":\"ready\"}\n{\"kind\":\"ready\"}"},
		{"unknown attribution", `{"kind":"ready","token":"secret"}`},
		{"multiple JSON values", `{"kind":"ready"} {}`},
		{"oversized", strings.Repeat("x", 16385)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testSnapshot(t)
			c, err := ParseConfig(t.Context(), s, "conpot")
			if err != nil {
				t.Fatal(err)
			}
			b := bridge{snapshot: s, config: c, sessions: map[string]*session{}}
			pipe := &testPipe{}
			r := Runtime{input: pipe}
			err = r.read(t.Context(), strings.NewReader(tc.input), &b, func(context.Context, events.AgentEvent) error { t.Fatal("invalid frame reached sink"); return nil }, make(chan struct{}, 1))
			if err == nil || pipe.Len() != 0 {
				t.Fatalf("error=%v ACK=%q", err, pipe.String())
			}
		})
	}
}

func TestPipeAcknowledgesOnlySuccessfulJournalDelivery(t *testing.T) {
	s := testSnapshot(t)
	c, err := ParseConfig(t.Context(), s, "conpot")
	if err != nil {
		t.Fatal(err)
	}
	input := "{\"kind\":\"ready\"}\n{\"kind\":\"opened\",\"session_id\":\"" + sessionID + "\",\"source_ip\":\"192.0.2.1\",\"source_port\":1234}\n"
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "journal failure"}[fail], func(t *testing.T) {
			b := bridge{snapshot: s, config: c, sessions: map[string]*session{}}
			pipe := &testPipe{}
			r := Runtime{input: pipe}
			failure := errors.New("journal down")
			err := r.read(t.Context(), strings.NewReader(input), &b, func(context.Context, events.AgentEvent) error {
				if fail {
					return failure
				}
				return nil
			}, make(chan struct{}, 1))
			if fail {
				if !errors.Is(err, failure) || pipe.Len() != 0 {
					t.Fatalf("error=%v ACK=%q", err, pipe.String())
				}
			} else if err != nil || pipe.String() != "ok\n" {
				t.Fatalf("error=%v ACK=%q", err, pipe.String())
			}
		})
	}
}

var _ io.WriteCloser = (*testPipe)(nil)
