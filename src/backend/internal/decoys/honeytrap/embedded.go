package honeytrap

import (
	"context"
	"fmt"
	"net/http"

	"honey-forge/modules/profiles"
)

// NewEmbeddedHandler uses the ordinary token matching and recording without a listener.
func NewEmbeddedHandler(ctx context.Context, snapshot profiles.Snapshot, sink Sink) (http.Handler, error) {
	config, err := ParseConfig(ctx, snapshot)
	if err != nil {
		return nil, err
	}

	if sink == nil {
		return nil, fmt.Errorf("event sink is required")
	}

	r := &Runtime{ctx: ctx, snapshot: snapshot, config: config, sink: sink}
	return http.HandlerFunc(r.serveHTTP), nil
}
