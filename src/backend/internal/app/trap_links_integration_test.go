//go:build integration

package app

import (
	"sync"
	"testing"

	"honey-forge/internal/traplinks"
	"honey-forge/modules/auth"
)

func TestTrapLinksPersistenceAndIsolation(t *testing.T) {
	r := openIntegrationRuntime(t)
	admin := registerOperator(t, r, "links@example.test", auth.OrganizationInput{Mode: "create", Name: "Links"})
	_, source, _ := createIntegrationTrap(t, r, admin)
	_, target, _ := createIntegrationTrap(t, r, admin)
	foreign := registerOperator(t, r, "foreign-links@example.test", auth.OrganizationInput{Mode: "create", Name: "Foreign"})
	_, foreignTrap, _ := createIntegrationTrap(t, r, foreign)
	link := traplinks.Link{SourceID: source.ID, TargetID: target.ID}
	body := integrationJSON(t, link)
	join := decodeIntegration[auth.JoinCode](t, sendOperator(t, r, "GET", "/api/organization/join-code", "", admin, 200))
	viewer := registerOperator(t, r, "links-viewer@example.test", auth.OrganizationInput{Mode: "join", JoinCode: join.Code})
	sendOperator(t, r, "GET", "/api/trap-links", "", viewer, 200)
	sendOperator(t, r, "POST", "/api/trap-links", body, viewer, 403)
	missingCSRF := operatorRequest(t, "POST", "/api/trap-links", body, admin)
	missingCSRF.Header.Del("X-CSRF-Token")
	if response := serveOperator(r.Router, missingCSRF); response.Code != 403 {
		t.Fatalf("missing CSRF: %d", response.Code)
	}

	sendOperator(t, r, "POST", "/api/trap-links", body, admin, 201)
	sendOperator(t, r, "DELETE", "/api/trap-links/"+source.ID+"/"+target.ID, "", viewer, 403)
	sendOperator(t, r, "POST", "/api/trap-links", body, admin, 409)
	sendOperator(t, r, "POST", "/api/trap-links", integrationJSON(t, traplinks.Link{SourceID: source.ID, TargetID: foreignTrap.ID}), admin, 404)
	sendOperator(t, r, "POST", "/api/trap-links", body, foreign, 404)
	sendOperator(t, r, "POST", "/api/trap-links", integrationJSON(t, traplinks.Link{SourceID: source.ID, TargetID: source.ID}), admin, 422)
	var listed struct {
		Items []traplinks.Link `json:"items"`
	}
	listed = decodeIntegration[struct {
		Items []traplinks.Link `json:"items"`
	}](t, sendOperator(t, r, "GET", "/api/trap-links", "", admin, 200))
	if len(listed.Items) != 1 || listed.Items[0] != link {
		t.Fatal(listed)
	}

	empty := decodeIntegration[struct {
		Items []traplinks.Link `json:"items"`
	}](t, sendOperator(t, r, "GET", "/api/trap-links", "", foreign, 200))
	if len(empty.Items) != 0 {
		t.Fatal("foreign organization sees links")
	}

	var count int
	if err := r.pool.QueryRow(t.Context(), `SELECT count(*) FROM trap_links WHERE organization_id=$1`, admin.view.Organization.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("persistent count=%d err=%v", count, err)
	}

	// Concurrent attempts produce one row and one successful create.
	sendOperator(t, r, "DELETE", "/api/trap-links/"+source.ID+"/"+target.ID, "", admin, 204)
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			codes <- serveOperator(r.Router, operatorRequest(t, "POST", "/api/trap-links", body, admin)).Code
		})
	}

	wg.Wait()
	close(codes)
	statuses := map[int]int{}
	for code := range codes {
		statuses[code]++
	}

	if statuses[201] != 1 || statuses[409] != 1 {
		t.Fatal(statuses)
	}

	// Tombstones cease routing and disappear from the graph even though FK history remains.
	trapMutation(t, r, admin, "DELETE", "/api/traps/"+target.ID, "", target.Revision, 204)
	listed = decodeIntegration[struct {
		Items []traplinks.Link `json:"items"`
	}](t, sendOperator(t, r, "GET", "/api/trap-links", "", admin, 200))
	if len(listed.Items) != 0 {
		t.Fatal("link to deleted trap remains visible")
	}
}
