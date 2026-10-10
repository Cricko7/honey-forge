package stream

import (
	"strconv"
	"time"

	"honey-forge/internal/contract"
)

const ReplayRetention = 24 * time.Hour

func cursorScope(org contract.ID) contract.CursorScope {
	return contract.CursorScope{OrganizationID: org, Collection: "frontend-stream", Filters: "dashboard-stream.v1"}
}

// EncodeCursor is shared by REST snapshots and WSS. The issue time also makes
// an empty organization's cursor expire without requiring a journal row.
func EncodeCursor(codec *contract.CursorCodec, org contract.ID, sequence int64, issued time.Time) (string, error) {
	if sequence < 0 {
		return "", contract.NewError("invalid_cursor")
	}
	return codec.Encode(cursorScope(org), contract.CursorPosition{Boundary: strconv.FormatInt(sequence, 10), After: issued.UTC().Format(time.RFC3339Nano)})
}

func DecodeCursor(codec *contract.CursorCodec, org contract.ID, token string, now time.Time) (int64, error) {
	p, err := codec.Decode(cursorScope(org), token)
	if err != nil {
		return 0, err
	}
	seq, err := strconv.ParseInt(p.Boundary, 10, 64)
	if err != nil || seq < 0 {
		return 0, contract.NewError("invalid_cursor")
	}
	issued, err := time.Parse(time.RFC3339Nano, p.After)
	if err != nil || issued.After(now.Add(time.Minute)) {
		return 0, contract.NewError("invalid_cursor")
	}
	if now.Sub(issued) > ReplayRetention {
		return 0, contract.NewError("cursor_expired")
	}
	return seq, nil
}
