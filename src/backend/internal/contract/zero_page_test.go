package contract

import (
	"encoding/json"
	"testing"
)

func TestZeroPage(t *testing.T) {
	b, err := json.Marshal(Page[int]{})
	if err != nil || string(b) != `{"items":[],"next_cursor":null}` {
		t.Fatalf("%s %v", b, err)
	}
}
