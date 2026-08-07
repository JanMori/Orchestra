package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// crewScopeReq builds a request as the given user (empty = workspace owner)
// with the chi URL params the crew handlers read (workspaceId + optional id).
// The crew handlers resolve the workspace from workspaceIDFromURL, which reads
// the chi route context, not the query string — so tests must inject the params
// here rather than on the path.
func crewScopeReq(userID, method, path string, body any, params map[string]string) *http.Request {
	var req *http.Request
	if userID == "" {
		req = newRequest(method, path, body)
	} else {
		req = newRequestAs(userID, method, path, body)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspaceId", testWorkspaceID)
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// createCrewAs creates a crew through the handler as the given user and
// returns the decoded response. Registers cleanup for the crew + its members.
func createCrewAs(t *testing.T, userID, name, leaderID string) CrewResponse {
	t.Helper()
	w := httptest.NewRecorder()
	r := crewScopeReq(userID, "POST", "/api/crews", map[string]any{
		"name":      name,
		"leader_id": leaderID,
	}, nil)
	testHandler.CreateCrew(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateCrew(%s): expected 201, got %d: %s", name, w.Code, w.Body.String())
	}
	var resp CrewResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode crew: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM crew_member WHERE crew_id = $1`, resp.ID)
		testPool.Exec(context.Background(), `DELETE FROM crew WHERE id = $1`, resp.ID)
	})
	return resp
}

// TestCreateCrew_PlainMemberBecomesCreator verifies the gate change: a plain
// workspace member (not owner/admin) can create a crew and is recorded as its
// creator.
func TestCreateCrew_PlainMemberBecomesCreator(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	memberID := createPlainMember(t, "crew-creator@multica.test")
	leaderID := createHandlerTestAgent(t, "crew-creator-leader", nil)

	crew := createCrewAs(t, memberID, "Member Owned Crew", leaderID)
	if crew.CreatorID != memberID {
		t.Fatalf("expected creator_id=%s, got %s", memberID, crew.CreatorID)
	}
}

// TestManageCrew_CreatorCanManageOwn verifies a creator can update, add a
// member to, and archive their own crew.
func TestManageCrew_CreatorCanManageOwn(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	memberID := createPlainMember(t, "crew-owner-manage@multica.test")
	leaderID := createHandlerTestAgent(t, "crew-owner-manage-leader", nil)
	worker := createHandlerTestAgent(t, "crew-owner-manage-worker", nil)

	crew := createCrewAs(t, memberID, "Manage Own Crew", leaderID)

	// Update name.
	w := httptest.NewRecorder()
	testHandler.UpdateCrew(w, crewScopeReq(memberID, "PATCH", "/api/crews", map[string]any{
		"name": "Renamed By Creator",
	}, map[string]string{"id": crew.ID}))
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateCrew as creator: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Add a public agent worker.
	w = httptest.NewRecorder()
	testHandler.AddCrewMember(w, crewScopeReq(memberID, "POST", "/api/crews/members", map[string]any{
		"member_type": "agent",
		"member_id":   worker,
	}, map[string]string{"id": crew.ID}))
	if w.Code != http.StatusCreated {
		t.Fatalf("AddCrewMember as creator: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Archive.
	w = httptest.NewRecorder()
	testHandler.DeleteCrew(w, crewScopeReq(memberID, "DELETE", "/api/crews", nil,
		map[string]string{"id": crew.ID}))
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteCrew as creator: expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

// TestManageCrew_StrangerMemberForbidden verifies a plain member who did not
// create the crew cannot manage it, while a workspace admin/owner still can.
func TestManageCrew_StrangerMemberForbidden(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	creatorID := createPlainMember(t, "crew-stranger-creator@multica.test")
	strangerID := createPlainMember(t, "crew-stranger-other@multica.test")
	leaderID := createHandlerTestAgent(t, "crew-stranger-leader", nil)

	crew := createCrewAs(t, creatorID, "Stranger Test Crew", leaderID)

	// Stranger member: update denied.
	w := httptest.NewRecorder()
	testHandler.UpdateCrew(w, crewScopeReq(strangerID, "PATCH", "/api/crews", map[string]any{
		"name": "Hijacked",
	}, map[string]string{"id": crew.ID}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("UpdateCrew as stranger: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// Stranger member: archive denied.
	w = httptest.NewRecorder()
	testHandler.DeleteCrew(w, crewScopeReq(strangerID, "DELETE", "/api/crews", nil,
		map[string]string{"id": crew.ID}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("DeleteCrew as stranger: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// Workspace owner (testUserID): update allowed — admin management unchanged.
	w = httptest.NewRecorder()
	testHandler.UpdateCrew(w, crewScopeReq("", "PATCH", "/api/crews", map[string]any{
		"name": "Renamed By Admin",
	}, map[string]string{"id": crew.ID}))
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateCrew as workspace owner: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAddCrewMember_CreatorAgentAccessGate verifies the comment-#2 rule: a
// non-admin creator may add a public agent (invocable) but not a private agent
// they cannot @-trigger. The workspace owner may add the same private agent —
// admin wiring is unrestricted.
func TestAddCrewMember_CreatorAgentAccessGate(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	privateAgentID, _, memberID := privateAgentTestFixture(t)
	publicLeaderID := createHandlerTestAgent(t, "crew-gate-leader", nil)
	publicWorkerID := createHandlerTestAgent(t, "crew-gate-worker", nil)

	crew := createCrewAs(t, memberID, "Agent Gate Crew", publicLeaderID)

	// Creator adds a public (invocable) worker — allowed.
	w := httptest.NewRecorder()
	testHandler.AddCrewMember(w, crewScopeReq(memberID, "POST", "/api/crews/members", map[string]any{
		"member_type": "agent",
		"member_id":   publicWorkerID,
	}, map[string]string{"id": crew.ID}))
	if w.Code != http.StatusCreated {
		t.Fatalf("AddCrewMember public agent: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Creator adds a private agent they cannot invoke — denied.
	w = httptest.NewRecorder()
	testHandler.AddCrewMember(w, crewScopeReq(memberID, "POST", "/api/crews/members", map[string]any{
		"member_type": "agent",
		"member_id":   privateAgentID,
	}, map[string]string{"id": crew.ID}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("AddCrewMember private agent as creator: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// Workspace owner adds the same private agent — allowed (admin unchanged).
	w = httptest.NewRecorder()
	testHandler.AddCrewMember(w, crewScopeReq("", "POST", "/api/crews/members", map[string]any{
		"member_type": "agent",
		"member_id":   privateAgentID,
	}, map[string]string{"id": crew.ID}))
	if w.Code != http.StatusCreated {
		t.Fatalf("AddCrewMember private agent as owner: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateCrew_CreatorPrivateLeaderForbidden verifies a non-admin cannot
// create a crew led by a private agent they cannot @-trigger.
func TestCreateCrew_CreatorPrivateLeaderForbidden(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	privateAgentID, _, memberID := privateAgentTestFixture(t)

	w := httptest.NewRecorder()
	r := crewScopeReq(memberID, "POST", "/api/crews", map[string]any{
		"name":      "Private Leader Crew",
		"leader_id": privateAgentID,
	}, nil)
	testHandler.CreateCrew(w, r)
	if w.Code != http.StatusForbidden {
		// Nothing should have been created; if it slipped through, clean up.
		if w.Code == http.StatusCreated {
			var resp CrewResponse
			if json.NewDecoder(w.Body).Decode(&resp) == nil {
				testPool.Exec(context.Background(), `DELETE FROM crew_member WHERE crew_id = $1`, resp.ID)
				testPool.Exec(context.Background(), `DELETE FROM crew WHERE id = $1`, resp.ID)
			}
		}
		t.Fatalf("CreateCrew with private leader: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
