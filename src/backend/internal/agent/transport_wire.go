package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/gorilla/websocket"

	"honey-forge/internal/contract"
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
	encoded, err := json.Marshal(contract.Envelope{MessageID: id, Type: kind, Payload: fields})
	if err != nil {
		return "", fmt.Errorf("encode agent envelope: %w", err)
	}

	encoded, err = padMessage(encoded)
	if err != nil {
		return "", err
	}

	if err := w.conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return "", err
	}

	return id, w.conn.WriteMessage(websocket.TextMessage, encoded)
}

func padMessage(raw []byte) ([]byte, error) {
	remaining := contract.MaxBodyBytes - len(raw)
	if remaining < 0 {
		return nil, contract.NewError("body_too_large")
	}

	// JSON whitespace varies the frame size without changing the envelope or batch.
	padding := min(64+rand.IntN(961), remaining)

	return append(raw, bytes.Repeat([]byte(" "), padding)...), nil
}

func payload(message contract.Envelope, dst any) error {
	raw, err := json.Marshal(message.Payload)
	if err != nil {
		return err
	}

	return json.Unmarshal(raw, dst)
}
