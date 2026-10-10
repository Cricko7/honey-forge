package decoys

import (
	"testing"

	"honey-forge/modules/profiles"
)

func TestSelect(t *testing.T) {
	for _, tc := range []struct {
		name    string
		typeID  string
		version int32
		wantErr bool
	}{
		{name: "TCP", typeID: "tcp-banner", version: 1},
		{name: "Redis", typeID: "redis-emulator", version: 1},
		{name: "Honeytokens", typeID: "honeytoken-http", version: 1},
		{name: "unknown type", typeID: "unknown", version: 1, wantErr: true},
		{name: "unknown version", typeID: "tcp-banner", version: 2, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start, err := Select(profiles.Snapshot{TypeID: tc.typeID, TypeVersion: tc.version})
			if (err != nil) != tc.wantErr || (start == nil) != tc.wantErr {
				t.Fatalf("Select() start=%v error=%v", start != nil, err)
			}
		})
	}
}

func TestForType(t *testing.T) {
	for _, tc := range []struct {
		name  string
		want  int
		error bool
	}{
		{name: "", want: 3},
		{name: "tcp-banner", want: 1},
		{name: "redis-emulator", want: 1},
		{name: "honeytoken-http", want: 1},
		{name: "unknown", error: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			types, err := ForType(tc.name)
			if (err != nil) != tc.error || len(types) != tc.want {
				t.Fatalf("ForType() types=%v error=%v", types, err)
			}
		})
	}
}
