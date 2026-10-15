package traps

import (
	"testing"

	"honey-forge/internal/contract"
)

func TestNetworkDeliveryIdentityIsStableAndSeparatesRecipients(t *testing.T) {
	first := networkDeliveryID("request-a", "target-a")
	if !contract.ValidID(first) || first != networkDeliveryID("request-a", "target-a") {
		t.Fatal("retry changed delivery identity")
	}

	for _, tc := range []struct{ name, request, target string }{
		{"other recipient", "request-a", "target-b"},
		{"other request", "request-b", "target-a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if first == networkDeliveryID(tc.request, tc.target) {
				t.Fatal("distinct delivery reused identity")
			}
		})
	}
}
