package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/gin-gonic/gin/binding"
	"github.com/gorilla/websocket"
	"honey-forge/internal/contract"
	"honey-forge/internal/platform/httpx"
	"honey-forge/internal/platform/pipeline"
)

type pipelineOutgoing struct {
	request pipeline.Request
	result  chan bool
	expires time.Time
}

func (l *Local) pipelineSend(ctx context.Context, raw []byte) (string, bool) {
	var message struct {
		Type    string           `json:"type"`
		Request pipeline.Request `json:"request"`
	}
	if contract.CheckJSON(raw) != nil || !httpx.StrictObject(raw, reflect.TypeOf(message)) || json.Unmarshal(raw, &message) != nil || binding.Validator.ValidateStruct(message.Request) != nil || pipeline.Check(message.Request) != nil {
		return "", false
	}
	l.mu.Lock()
	snapshot := l.snapshot
	l.mu.Unlock()
	if !pipeline.AllowsSend(snapshot, message.Request) {
		return message.Request.RequestID, false
	}
	item := pipelineOutgoing{message.Request, make(chan bool, 1), time.Now().Add(4 * time.Second)}
	select {
	case l.PipelineOutgoing <- item:
	default:
		return message.Request.RequestID, false
	}
	timer := time.NewTimer(4 * time.Second)
	defer timer.Stop()
	select {
	case ok := <-item.result:
		return message.Request.RequestID, ok
	case <-ctx.Done():
		return message.Request.RequestID, false
	case <-timer.C:
		return message.Request.RequestID, false
	}
}

func (l *Local) pipelineWriter(ctx context.Context, write func(any) error) {
	for {
		select {
		case <-ctx.Done():
			return
		case d := <-l.PipelineIncoming:
			l.mu.Lock()
			snapshot := l.snapshot
			l.mu.Unlock()
			if !time.Now().Before(d.ExpiresAt) || !pipeline.AllowsReceive(snapshot, d.Request) {
				continue
			}
			if err := write(map[string]any{"type": "trap.pipeline", "delivery": d}); err != nil {
				return
			}
		}
	}
}

func localWriter(conn *websocket.Conn) func(any) error {
	var mu sync.Mutex
	return func(payload any) error { mu.Lock(); defer mu.Unlock(); return writeLocal(conn, payload) }
}

func (c *LocalClient) SendPipeline(ctx context.Context, r pipeline.Request) error {
	if err := pipeline.Check(r); err != nil {
		return err
	}
	return c.exchange(ctx, map[string]any{"type": "trap.pipeline_send", "request": r}, "pipeline_request_id", r.RequestID)
}

func (c *LocalClient) receivePipeline(raw json.RawMessage) error {
	var d pipeline.Delivery
	if json.Unmarshal(raw, &d) != nil || pipeline.Check(d.Request) != nil || !contract.ValidID(d.TraceID) || !contract.ValidID(d.Token) || d.Hop < 1 || d.Hop > pipeline.MaxHops || !time.Now().Before(d.ExpiresAt) {
		return fmt.Errorf("invalid pipeline delivery")
	}
	select {
	case c.PipelineIncoming <- d:
		return nil
	default:
		return fmt.Errorf("pipeline runtime queue full")
	}
}
