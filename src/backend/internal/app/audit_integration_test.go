//go:build integration

package app

import (
	"honey-forge/internal/contract"
	"honey-forge/modules/audit"
	"honey-forge/modules/auth"
	"honey-forge/modules/traps"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuditUnifiedHistoryReplayAccessAndStream(t *testing.T) {
	r := openIntegrationRuntime(t)
	admin := registerOperator(t, r, "audit@example.test", auth.OrganizationInput{Mode: "create", Name: "Audit"})
	foreign := registerOperator(t, r, "foreign-audit@example.test", auth.OrganizationInput{Mode: "create", Name: "Foreign"})
	_, trap, _ := createIntegrationTrap(t, r, admin)
	path := "/api/traps/" + trap.ID
	sendOperator(t, r, "POST", path+"/agent-credentials", `{"expected_generation":0}`, admin, 200)
	page := decodeIntegration[contract.Page[audit.Entry]](t, sendOperator(t, r, "GET", "/api/audit-entries", "", admin, 200))
	if len(page.Items) != 5 {
		t.Fatalf("audit count %d: %+v", len(page.Items), page.Items)
	}
	actions := map[string]bool{}
	for _, entry := range page.Items {
		actions[entry.Action] = true
		if entry.Actor.UserID != admin.view.User.ID || entry.Actor.Email != admin.view.User.Email || entry.Actor.Role != contract.Admin {
			t.Fatalf("actor snapshot %+v", entry.Actor)
		}
		sendOperator(t, r, "GET", "/api/audit-entries/"+entry.ID, "", admin, 200)
		requireOperatorError(t, sendOperator(t, r, "GET", "/api/audit-entries/"+entry.ID, "", foreign, 404), "resource_not_found")
	}
	for _, action := range []string{"organization.created", "session.created", "profile.created", "trap.created", "trap.agent_credentials_issued"} {
		if !actions[action] {
			t.Fatalf("missing %s", action)
		}
	}
	listed := sendOperator(t, r, "GET", "/api/audit-entries?limit=2", "", admin, 200)
	first := decodeIntegration[contract.Page[audit.Entry]](t, listed)
	if first.NextCursor == nil {
		t.Fatal("missing pagination")
	}
	next := decodeIntegration[contract.Page[audit.Entry]](t, sendOperator(t, r, "GET", "/api/audit-entries?limit=2&cursor="+*first.NextCursor, "", admin, 200))
	if len(next.Items) != 2 || next.Items[0].ID == first.Items[0].ID {
		t.Fatal("pagination repeated records")
	}
	requireOperatorError(t, sendOperator(t, r, "GET", "/api/audit-entries?cursor="+*first.NextCursor, "", foreign, 400), "invalid_cursor")
	for _, q := range []string{"action=unknown", "actor_id=" + string(contract.NewID()), "resource_id=" + string(contract.NewID())} {
		p := decodeIntegration[contract.Page[audit.Entry]](t, sendOperator(t, r, "GET", "/api/audit-entries?"+q, "", admin, 200))
		if len(p.Items) != 0 {
			t.Fatal("unknown filter returned records")
		}
	}
	for _, q := range []string{"actor_id=invalid", "action=", "limit=0", "from=invalid", "resource_id=invalid", "action=x&action=y"} {
		sendOperator(t, r, "GET", "/api/audit-entries?"+q, "", admin, 400)
	}
	sendOperator(t, r, "GET", "/api/audit-entries", "", operatorSession{}, 401)
	for _, method := range []string{"POST", "PATCH", "DELETE"} {
		sendOperator(t, r, method, "/api/audit-entries/"+page.Items[0].ID, "", admin, 405)
	}
	// Rotation emits a safe DTO, and a newly joined viewer can read the history.
	eventPage := sendOperator(t, r, "GET", "/api/events", "", admin, 200)
	var eventSnapshot struct {
		Cursor string `json:"stream_cursor"`
	}
	eventSnapshot = decodeIntegration[struct {
		Cursor string `json:"stream_cursor"`
	}](t, eventPage)
	currentCode := decodeIntegration[auth.JoinCode](t, sendOperator(t, r, "GET", "/api/organization/join-code", "", admin, 200))
	sendOperator(t, r, "POST", "/api/organization/join-code/rotations", integrationJSON(t, auth.RotateJoinCodeRequest{ExpectedRevision: int64(currentCode.Revision)}), admin, 200)
	server := httptest.NewTLSServer(r.Router)
	defer server.Close()
	conn := dialFrontend(t, server, admin)
	subscribeFrontend(t, conn, eventSnapshot.Cursor)
	notice := readFrontend(t, conn, "audit.created")
	data := string(notice.Payload["data"])
	if !strings.Contains(data, "audit_id") || strings.Contains(data, "email") || strings.Contains(data, "join_code") {
		t.Fatalf("unsafe notification %s", data)
	}
	readFrontend(t, conn, "stream.ready")
	code := decodeIntegration[auth.JoinCode](t, sendOperator(t, r, "GET", "/api/organization/join-code", "", admin, 200))
	viewer := registerOperator(t, r, "viewer-audit@example.test", auth.OrganizationInput{Mode: "join", JoinCode: code.Code})
	sendOperator(t, r, "GET", "/api/audit-entries", "", viewer, 200)
	// A create replay records a single operator action.
	req := traps.CreateRequest{RequestID: string(contract.NewID()), Name: "Replay", ProfileID: trap.ProfileID}
	body := integrationJSON(t, req)
	sendOperator(t, r, "POST", "/api/traps", body, admin, 201)
	sendOperator(t, r, "POST", "/api/traps", body, admin, 200)
	filtered := decodeIntegration[contract.Page[audit.Entry]](t, sendOperator(t, r, "GET", "/api/audit-entries?action=trap.created", "", viewer, 200))
	if len(filtered.Items) != 2 {
		t.Fatalf("replay produced extra audit: %d", len(filtered.Items))
	}
}
