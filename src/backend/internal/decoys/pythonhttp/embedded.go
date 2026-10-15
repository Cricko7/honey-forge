package pythonhttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"honey-forge/internal/decoys/honeytrap"
	"honey-forge/internal/decoys/redistrap"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

type embeddedCall struct {
	ID      string          `json:"id"`
	NodeID  string          `json:"node_id"`
	Payload json.RawMessage `json:"payload"`
}

type embeddedTraps struct {
	mu       sync.Mutex
	pending  sync.WaitGroup
	slots    chan struct{}
	ctx      context.Context
	snapshot profiles.Snapshot
	configs  map[string]embeddedConfig
	redis    map[string]*redistrap.Embedded
}

type embeddedConfig struct {
	Kind   string
	Tokens string `json:"tokens"`
}

func newEmbeddedTraps(ctx context.Context, snapshot profiles.Snapshot) (*embeddedTraps, error) {
	e := &embeddedTraps{ctx: ctx, snapshot: snapshot, configs: map[string]embeddedConfig{}, redis: map[string]*redistrap.Embedded{}, slots: make(chan struct{}, 16)}

	raw, err := json.Marshal(snapshot.Config)
	if err != nil {
		return nil, fmt.Errorf("encode embedded traps: %w", err)
	}

	var c struct {
		Blocks []struct {
			ID, Kind string
			Config   embeddedConfig
		}
	}

	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("decode embedded traps: %w", err)
	}

	for _, b := range c.Blocks {
		if b.Kind == "redis" || b.Kind == "honeytoken" {
			config := b.Config
			config.Kind = b.Kind
			e.configs[b.ID] = config
		}
	}

	return e, nil
}

func (e *embeddedTraps) stop() {
	e.pending.Wait()

	for _, r := range e.redis {
		r.Stop()
	}
}

func (e *embeddedTraps) call(ctx context.Context, request embeddedCall) (map[string]any, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	config, ok := e.configs[request.NodeID]
	if !ok || len(request.Payload) > 8192 {
		return nil, fmt.Errorf("unconfigured embedded call")
	}

	result := map[string]any{"status": 200, "headers": map[string]string{}, "events": []any{}}
	if config.Kind == "redis" {
		var payload struct {
			Commands [][]string `json:"commands"`
		}

		if err := json.Unmarshal(request.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode Redis request: %w", err)
		}

		runtime := e.redis[request.NodeID]
		if runtime == nil {
			var err error
			runtime, err = redistrap.StartEmbedded(e.ctx)
			if err != nil {
				return nil, err
			}
			e.redis[request.NodeID] = runtime
		}
		values, err := runtime.Execute(ctx, payload.Commands)
		if err != nil {
			return nil, err
		}
		result["body"] = values
	} else {
		var payload struct {
			Method, Path string
			SourcePort   int `json:"source_port"`
			Headers      map[string]string
			IP           string `json:"source_ip"`
		}

		if err := json.Unmarshal(request.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode token request: %w", err)
		}
		parsed, err := url.ParseRequestURI(payload.Path)
		if err != nil || parsed.IsAbs() || parsed.Host != "" || strings.HasPrefix(payload.Path, "//") || net.ParseIP(payload.IP) == nil || payload.SourcePort < 1 || payload.SourcePort > 65535 {
			return nil, fmt.Errorf("invalid token request")
		}

		var tokens any
		if err := json.Unmarshal([]byte(config.Tokens), &tokens); err != nil {
			return nil, fmt.Errorf("decode embedded tokens: %w", err)
		}

		child := profiles.Snapshot{TypeID: "honeytoken-http", TypeVersion: 1, ProfileRevision: e.snapshot.ProfileRevision, Config: profiles.Object{
			"services": []any{map[string]any{"name": "embedded", "port": 1}}, "tokens": tokens,
			"management": map[string]any{"heartbeat_interval_seconds": 10, "telemetry_flush_interval_ms": 500},
		}}
		hits := []any{}
		handler, err := honeytrap.NewEmbeddedHandler(ctx, child, func(_ context.Context, event events.AgentEvent) error {
			if event.EventType == "honeytoken.triggered" {
				hits = append(hits, map[string]any{"name": event.EventType, "payload": event.Data})
			}

			return nil
		})
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, payload.Method, payload.Path, nil)
		if err != nil {
			return nil, fmt.Errorf("create token request: %w", err)
		}
		req.RemoteAddr = net.JoinHostPort(payload.IP, strconv.Itoa(payload.SourcePort))
		for key, value := range payload.Headers {
			req.Header.Set(key, value)
		}

		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		headers := map[string]string{}
		for key := range response.Header() {
			headers[key] = response.Header().Get(key)
		}
		result["status"], result["body"], result["headers"], result["events"] = response.Code, response.Body.String(), headers, hits
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode embedded reply: %w", err)
	}

	if len(raw) > 32768 {
		return nil, fmt.Errorf("embedded reply limit exceeded")
	}

	return result, nil
}

// Keep telemetry acknowledgements moving while a trap executes a request.
func (r *Runtime) dispatchEmbedded(ctx context.Context, request embeddedCall) error {
	select {
	case r.embedded.slots <- struct{}{}:
		r.embedded.pending.Add(1)
		go func(request embeddedCall) {
			defer r.embedded.pending.Done()
			defer func() { <-r.embedded.slots }()

			callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			result, err := r.embedded.call(callCtx, request)
			if err := r.write(map[string]any{"embedded_id": request.ID, "ok": err == nil, "result": result}); err != nil {
				_ = r.cmd.Process.Kill()
			}
		}(request)
	default:
		if err := r.write(map[string]any{"embedded_id": request.ID, "ok": false}); err != nil {
			return err
		}
	}
	return nil
}
