package events

import (
	"context"
	"errors"
	"testing"

	"honey-forge/internal/contract"
)

type recordingFake struct {
	calls  int
	events []Event
}

func (f *recordingFake) Recording(context.Context, string, string) ([]Event, error) {
	f.calls++
	return f.events, nil
}

func TestRecordingAuthorizationAndCompleteness(t *testing.T) {
	for _, tc := range []struct {
		name      string
		role      contract.Role
		sequences []int64
		closed    bool
		code      string
		complete  bool
	}{
		{"viewer success", contract.Viewer, []int64{1, 2}, true, "", true},
		{"admin active", contract.Admin, []int64{1}, false, "", false},
		{"missing chunk", contract.Viewer, []int64{1, 3}, true, "", false},
		{"absent", contract.Viewer, nil, false, "resource_not_found", false},
		{"unauthenticated", "", nil, false, "unauthenticated", false},
		{"agent forbidden", contract.Agent, nil, false, "forbidden", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &recordingFake{}
			for i, seq := range tc.sequences {
				kind := "service.connection_opened"
				if tc.closed && i == len(tc.sequences)-1 {
					kind = "service.connection_closed"
				}

				store.events = append(store.events, Event{AgentEvent: AgentEvent{SessionSequence: seq, EventType: kind}})
			}

			ctx := context.Background()
			if tc.role != "" {
				ctx = contract.WithPrincipal(ctx, contract.Principal{Role: tc.role, OrganizationID: "11111111-1111-4111-8111-111111111111"})
			}

			recording, err := NewRecordingService(store).Read(ctx, "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222")
			if tc.code != "" {
				var api *contract.Error
				if !errors.As(err, &api) || api.Code != tc.code {
					t.Fatalf("error %v", err)
				}
			} else if err != nil || recording.Complete != tc.complete {
				t.Fatalf("recording %+v: %v", recording, err)
			}

			if (tc.role == "" || tc.role == contract.Agent) && store.calls != 0 {
				t.Fatal("unauthorized database read")
			}
		})
	}
}
