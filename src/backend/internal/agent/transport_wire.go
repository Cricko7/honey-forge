package agent

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"honey-forge/internal/contract"
	"time"
)

type wire struct {
	conn *websocket.Conn
}

func (w *wire) send(kind string, payload any) (contract.ID, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode agent payload: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", err
	}
	id := contract.NewID()
	if err := w.conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return "", err
	}
	return id, w.conn.WriteJSON(contract.Envelope{MessageID: id, Type: kind, Payload: fields})
}

func payload(message contract.Envelope, dst any) error {
	raw, err := json.Marshal(message.Payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}
