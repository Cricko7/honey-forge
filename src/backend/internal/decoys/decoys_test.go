package decoys

import (
	"reflect"
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
		{name: "SSH", typeID: "ssh-high-interaction", version: 1},
		{name: "SSH host telemetry", typeID: "ssh-high-interaction", version: 2},
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

func TestSupportedTypesAreStableAndDetached(t *testing.T) {
	types := SupportedTypes()
	var ids []string
	for _, entry := range types {
		ids = append(ids, entry.TypeID)
		if (entry.TypeVersion != 1 && !(entry.TypeID == "ssh-high-interaction" && entry.TypeVersion == 2)) || !reflect.DeepEqual(entry.Actions, []string{"start", "stop", "apply_config"}) {
			t.Fatalf("changed agent capabilities: %+v", entry)
		}

		if _, err := Select(profiles.Snapshot{TypeID: entry.TypeID, TypeVersion: int32(entry.TypeVersion)}); err != nil {
			t.Fatal(err)
		}
	}

	if !reflect.DeepEqual(ids, []string{"tcp-banner", "redis-emulator", "honeytoken-http", "ssh-high-interaction", "ssh-high-interaction"}) {
		t.Fatal("capability ordering changed", ids)
	}

	types[0].Actions[0] = "changed"
	if SupportedTypes()[0].Actions[0] != "start" || types[1].Actions[0] != "start" {
		t.Fatal("capability snapshots share mutable actions")
	}
}

func TestForType(t *testing.T) {
	for _, tc := range []struct {
		name  string
		want  int
		error bool
	}{
		{name: "", want: 5},
		{name: "tcp-banner", want: 1},
		{name: "redis-emulator", want: 1},
		{name: "honeytoken-http", want: 1},
		{name: "ssh-high-interaction", want: 2},
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
