// Package frontendws delivers the durable organization journal to browsers.
package frontendws

import (
	"context"
	"encoding/json"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
)

type journal interface {
	Boundary(context.Context) (int64, int64, error)
	Changes(context.Context, int64, int64) ([]Change, error)
}
type sessions interface {
	ResolveSession(context.Context, string) (auth.ResolvedSession, error)
}

func subscribe(request contract.Envelope) (*string, error) {
	if request.Type != "stream.subscribe" || request.ReplyTo != nil || len(request.Payload) != 1 {
		return nil, contract.NewError("invalid_message")
	}
	raw, ok := request.Payload["after"]
	if !ok {
		return nil, contract.NewError("invalid_message")
	}
	var after *string
	if err := json.Unmarshal(raw, &after); err != nil || after != nil && (*after == "" || len(*after) > contract.MaxCursorLength) {
		return nil, contract.NewError("invalid_message")
	}
	return after, nil
}

func envelope(reply *contract.ID, kind string, payload any) (contract.Envelope, int, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return contract.Envelope{}, 0, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return contract.Envelope{}, 0, err
	}
	e := contract.Envelope{MessageID: contract.NewID(), Type: kind, ReplyTo: reply, Payload: fields}
	b, err := json.Marshal(e)
	if err != nil {
		return e, 0, err
	}
	if len(b) > contract.MaxBodyBytes {
		return e, 0, contract.NewError("body_too_large")
	}
	return e, len(b), nil
}

type ready struct {
	Cursor     string    `json:"cursor"`
	ServerTime time.Time `json:"server_time"`
	Replayed   bool      `json:"replayed"`
}
type notification struct {
	Cursor     string          `json:"cursor"`
	OccurredAt time.Time       `json:"occurred_at"`
	Data       json.RawMessage `json:"data"`
}
