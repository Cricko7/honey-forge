package pipeline

import (
	"testing"

	"honey-forge/modules/profiles"
)

func TestNetworkSendRemainsBoundToInstalledBlockAndEndpoint(t *testing.T) {
	snapshot := profiles.Snapshot{TypeID: "custom-http", Config: profiles.Object{
		"blocks": []any{map[string]any{"id": "send", "kind": "send", "config": map[string]any{
			"target_id": NetworkTargetID, "service": "web", "method": "POST", "path": "/pipeline",
		}}},
	}}
	for _, tc := range []struct {
		name    string
		change  func(*Request)
		allowed bool
	}{
		{"network route", func(*Request) {}, true},
		{"unconfigured target", func(r *Request) { r.TargetID = "11111111-1111-4111-8111-111111111111" }, false},
		{"unconfigured block", func(r *Request) { r.NodeID = "other" }, false},
		{"unconfigured service", func(r *Request) { r.Service = "other" }, false},
		{"unconfigured path", func(r *Request) { r.Path = "/other" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := Request{NodeID: "send", TargetID: NetworkTargetID, Service: "web", Method: "POST", Path: "/pipeline"}
			tc.change(&request)
			if got := AllowsSend(snapshot, request); got != tc.allowed {
				t.Fatalf("allowed=%v, want=%v", got, tc.allowed)
			}
		})
	}
}
