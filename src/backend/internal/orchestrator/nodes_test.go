package orchestrator

import "testing"

func TestDecodeNodesRequiresAssignedWorker(t *testing.T) {
	nodes, err := decodeNodes([]byte(`[
		{"ID":"assigned","Spec":{"Role":"worker","Labels":{"honeyforge.organization":"11111111-1111-4111-8111-111111111111"}},"Description":{"Hostname":"company"}},
		{"ID":"unassigned","Spec":{"Role":"worker"}},
		{"ID":"invalid","Spec":{"Role":"worker","Labels":{"honeyforge.organization":"not-a-uuid"}}},
		{"ID":"manager","Spec":{"Role":"manager","Labels":{"honeyforge.organization":"11111111-1111-4111-8111-111111111111"}}}
	]`))
	if err != nil {
		t.Fatal(err)
	}

	if len(nodes) != 1 || nodes[0].ID != "assigned" || nodes[0].Description.Hostname != "company" {
		t.Fatalf("nodes=%+v", nodes)
	}

	if _, err := decodeNodes([]byte(`not-json`)); err == nil {
		t.Fatal("accepted invalid node metadata")
	}
}
