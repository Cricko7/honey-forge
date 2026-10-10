package honeytrap

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path"
	"strconv"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/events"
)

func (r *Runtime) serveHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	status, body := http.StatusNotFound, "Not found\n"
	var triggered *Token
	if req.Method != http.MethodGet {
		status, body = http.StatusMethodNotAllowed, "Method not allowed\n"
		w.Header().Set("Allow", "GET")
	} else if req.URL.EscapedPath() == req.URL.Path {
		for _, token := range r.config.Tokens {
			if req.URL.Path == "/artifacts/"+token.ID {
				// Distribution is separate from use: fetching bait does not claim a hit.
				raw, err := json.Marshal(token)
				if err != nil {
					r.fail(err)
					http.Error(w, "Service unavailable", 503)
					return
				}
				status, body = http.StatusOK, string(raw)
				w.Header().Set("Content-Type", "application/json")
				break
			}

			if req.URL.Path != token.Path {
				continue
			}

			if token.Kind == "key" {
				provided := sha256.Sum256([]byte(req.Header.Get("Authorization")))
				expected := sha256.Sum256([]byte("Bearer " + token.Value))
				if len(req.Header.Values("Authorization")) != 1 || subtle.ConstantTimeCompare(provided[:], expected[:]) != 1 {
					status, body = http.StatusUnauthorized, "Unauthorized\n"
					break
				}
				status, body = http.StatusOK, "{\"status\":\"ok\",\"items\":[]}\n"
				w.Header().Set("Content-Type", "application/json")
			} else {
				status, body = http.StatusOK, token.Value
				if token.Kind == "file" {
					w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, path.Base(token.Path)))
				}
			}
			triggered = &token
			break
		}
	}

	if err := r.record(req, triggered); err != nil {
		r.fail(err)
		w.Header().Del("Content-Disposition")
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(status)
	// A disconnected client does not stop the trap.
	if _, err := w.Write([]byte(body)); err != nil {
		return
	}
}

func (r *Runtime) record(req *http.Request, token *Token) error {
	host, portText, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return fmt.Errorf("parse HTTP peer: %w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 || net.ParseIP(host) == nil {
		return fmt.Errorf("invalid HTTP peer")
	}

	service := r.config.Services[0]
	session := string(contract.NewID())
	sequence := int64(0)
	started := time.Now()

	// Use the runtime context so a client disconnect cannot discard an observed hit.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.ctx), 5*time.Second)
	defer cancel()

	emit := func(kind string, data map[string]any) error {
		raw, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("encode HTTP event: %w", err)
		}
		sequence++
		return r.sink(ctx, events.AgentEvent{
			EventID: string(contract.NewID()), EventType: kind, TypeID: r.snapshot.TypeID, TypeVersion: 1,
			ProfileRevision: int64(r.snapshot.ProfileRevision), OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
			SessionID: session, SessionSequence: sequence, Source: events.Source{IP: host, Port: port},
			Destination: events.Destination{Protocol: "tcp", Port: service.Port}, Data: raw,
		})
	}

	if err := emit("service.connection_opened", map[string]any{"service": service.Name}); err != nil {
		return err
	}
	if token != nil {
		if err := emit("honeytoken.triggered", map[string]any{"service": service.Name, "token_id": token.ID, "kind": token.Kind, "method": "GET"}); err != nil {
			return err
		}
	}

	return emit("service.connection_closed", map[string]any{"service": service.Name, "duration_ms": time.Since(started).Milliseconds(), "bytes_received": 0, "reason": "peer_closed"})
}
