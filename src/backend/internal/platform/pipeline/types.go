// Package pipeline defines bounded JSON handoffs over the authenticated agent channel.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"honey-forge/internal/contract"
)

const QueueCapacity = 16
const MaxPayloadBytes = 8192
const MaxHops = 4096
const Lifetime = 5 * time.Minute

// NetworkTargetID resolves recipients from the current directed network links.
// A reserved UUID keeps published custom-http configuration schemas unchanged.
const NetworkTargetID = "00000000-0000-0000-0000-000000000000"

type Request struct {
	RequestID  string          `json:"request_id" validate:"required,uuid"`
	NodeID     string          `json:"node_id" validate:"required,min=1,max=64"`
	TargetID   string          `json:"target_id" validate:"required,uuid"`
	Service    string          `json:"service" validate:"required,min=1,max=32"`
	Method     string          `json:"method" validate:"oneof=POST PUT PATCH"`
	Path       string          `json:"path" validate:"required,min=1,max=2048"`
	Payload    json.RawMessage `json:"payload" validate:"required"`
	Token      string          `json:"token,omitempty" validate:"omitempty,uuid"`
	SourceIP   string          `json:"source_ip" validate:"required"`
	SourcePort int             `json:"source_port" validate:"min=1,max=65535"`
}

type Delivery struct {
	Request
	SourceID  string    `json:"source_id"`
	TraceID   string    `json:"trace_id"`
	Hop       int       `json:"hop"`
	ExpiresAt time.Time `json:"expires_at"`
}

func Check(r Request) error {
	u, err := url.ParseRequestURI(r.Path)
	ip, ipErr := netip.ParseAddr(r.SourceIP)
	if err != nil || u.IsAbs() || u.Host != "" || u.RawQuery != "" || u.Fragment != "" || !strings.HasPrefix(r.Path, "/") || strings.HasPrefix(r.Path, "//") || strings.ContainsAny(r.Path, "?#\r\n") || ipErr != nil || ip.Zone() != "" || len(r.Payload) > MaxPayloadBytes || contract.CheckJSON(r.Payload) != nil || !json.Valid(r.Payload) || !contract.ValidTypeID(r.Service) {
		return fmt.Errorf("invalid pipeline request")
	}
	return nil
}

type senderKey struct{}
type Sender func(context.Context, Request) error

func WithSender(ctx context.Context, send Sender) context.Context {
	return context.WithValue(ctx, senderKey{}, send)
}

func Send(ctx context.Context, request Request) error {
	send, ok := ctx.Value(senderKey{}).(Sender)
	if !ok {
		return fmt.Errorf("pipeline channel unavailable")
	}
	return send(ctx, request)
}
