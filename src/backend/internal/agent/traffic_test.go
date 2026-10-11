package agent

import (
	"bytes"
	"path/filepath"
	"testing"

	"honey-forge/internal/contract"
	"honey-forge/internal/platform/traffic"
)

func TestLocalTrafficQueueDoesNotUseTelemetryJournal(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), "trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	local, err := NewLocal(j, "token")
	if err != nil {
		t.Fatal(err)
	}
	frame := traffic.Frame{ConnectionID: string(contract.NewID()), Sequence: 1, Kind: "data", RemoteAddress: "remote", LocalAddress: "local", Data: []byte{0, 255}}
	if err := local.acceptTraffic(frame); err != nil {
		t.Fatal(err)
	}
	frame.Data[0] = 77
	got := <-local.Traffic
	if !bytes.Equal(got.Data, []byte{0, 255}) || len(j.Pending()) != 0 {
		t.Fatal("traffic changed or entered telemetry")
	}
	for i := 0; i < traffic.QueueCapacity+1; i++ {
		if err := local.acceptTraffic(frame); err != nil {
			t.Fatal(err)
		}
	}
	if local.TrafficDropped.Load() != 1 {
		t.Fatal("overflow was not counted")
	}
	frame.Kind = "open"
	if err := local.acceptTraffic(frame); err == nil {
		t.Fatal("invalid frame accepted")
	}
}
