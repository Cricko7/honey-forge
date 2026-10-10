//go:build integration

package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"honey-forge/modules/auth"
	"honey-forge/modules/profiles"
)

func TestRealConcurrentProfileRequestsCommitOnce(t *testing.T) {
	runtime := openIntegrationRuntime(t)
	admin := registerOperator(t, runtime, "admin@example.com", auth.OrganizationInput{Mode: "create", Name: "Demo"})
	input := catalogProfileRequest(t, runtime, admin)
	body := integrationJSON(t, input)
	requests := []*http.Request{
		operatorRequest(t, "POST", "/api/profiles", body, admin),
		operatorRequest(t, "POST", "/api/profiles", body, admin),
	}
	responses := concurrentOperatorRequests(runtime.Router, requests)

	var created, replayed *httptest.ResponseRecorder
	for _, w := range responses {
		switch w.Code {
		case 201:
			created = w
		case 200:
			replayed = w
		default:
			t.Fatalf("concurrent creation: status %d: %s", w.Code, w.Body.String())
		}
	}
	if created == nil || replayed == nil || replayed.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal("concurrent create must yield one creation and one replay")
	}
	profile := decodeIntegration[profiles.Profile](t, created)
	if other := decodeIntegration[profiles.Profile](t, replayed); other.ID != profile.ID || other.Revision != 1 {
		t.Fatal("concurrent request_id created two profiles")
	}

	path, etag := created.Header().Get("Location"), created.Header().Get("ETag")
	requests = nil
	for _, banner := range []string{"first update", "second update"} {
		config := validIntegrationConfig()
		config["listeners"].([]any)[0].(profiles.Object)["banner"] = banner
		req := operatorRequest(t, "PATCH", path, integrationJSON(t, profiles.Object{"config": config}), admin)
		req.Header.Set("If-Match", etag)
		requests = append(requests, req)
	}
	responses = concurrentOperatorRequests(runtime.Router, requests)
	var winner *httptest.ResponseRecorder
	conflicts := 0
	for _, w := range responses {
		switch w.Code {
		case 200:
			winner = w
		case 412:
			conflicts++
			requireOperatorError(t, w, "revision_mismatch")
		default:
			t.Fatalf("concurrent patch: status %d: %s", w.Code, w.Body.String())
		}
	}
	if winner == nil || conflicts != 1 {
		t.Fatal("concurrent PATCH must yield one update and one revision conflict")
	}
	current := decodeIntegration[profiles.Profile](t, sendOperator(t, runtime, "GET", path, "", admin, 200))
	winning := decodeIntegration[profiles.Profile](t, winner)
	if current.Revision != 2 || !profiles.EqualJSON(current.Config, winning.Config) {
		t.Fatal("losing PATCH overwrote winning configuration or incremented revision")
	}
	page := decodeIntegration[profiles.Page](t, sendOperator(t, runtime, "GET", "/api/profiles", "", admin, 200))
	if len(page.Items) != 1 || page.Items[0].ID != profile.ID {
		t.Fatal("concurrent creation left duplicate profiles")
	}

	var revisions, audits, changes int
	err := runtime.pool.QueryRow(t.Context(), `SELECT
		(SELECT count(*) FROM profile_revisions WHERE profile_id=$1),
		(SELECT count(*) FROM profile_audit WHERE resource_id=$1),
		(SELECT count(*) FROM profile_changes WHERE organization_id=$2)`, profile.ID, admin.view.Organization.ID).Scan(&revisions, &audits, &changes)
	if err != nil {
		t.Fatal(err)
	}
	if revisions != 2 || audits != 2 || changes != 4 {
		t.Fatalf("duplicate transactional effects: revisions=%d audits=%d changes=%d, want 2/2/4", revisions, audits, changes)
	}
}

func concurrentOperatorRequests(router http.Handler, requests []*http.Request) []*httptest.ResponseRecorder {
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, len(requests))
	for _, req := range requests {
		go func() {
			<-start
			results <- serveOperator(router, req)
		}()
	}
	close(start)

	responses := make([]*httptest.ResponseRecorder, 0, len(requests))
	for range requests {
		responses = append(responses, <-results)
	}
	return responses
}
