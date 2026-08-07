package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCreateIssueAssignedToCrewEnqueuesLeader verifies that creating an
// issue with assignee_type=crew immediately enqueues a task for the crew
// leader (mirrors the agent-assignee parking-lot rule: skip backlog only).
func TestCreateIssueAssignedToCrewEnqueuesLeader(t *testing.T) {
	ctx := context.Background()

	// Look up the seeded test agent — it has a runtime, so it can lead a crew.
	var leaderID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&leaderID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	// Create a crew with that agent as leader.
	var crewID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO crew (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "Trigger Test Crew", leaderID, testUserID).Scan(&crewID); err != nil {
		t.Fatalf("create crew: %v", err)
	}
	defer testPool.Exec(ctx, `DELETE FROM crew WHERE id = $1`, crewID)

	// Create an issue assigned to the crew.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Crew-assigned at creation",
		"assignee_type": "crew",
		"assignee_id":   crewID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	defer func() {
		cleanupReq := newRequest("DELETE", "/api/issues/"+created.ID, nil)
		cleanupReq = withURLParam(cleanupReq, "id", created.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), cleanupReq)
	}()

	// A task for the crew leader should now exist for this issue.
	var taskCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2
	`, created.ID, leaderID).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if taskCount == 0 {
		t.Fatalf("expected crew-leader task to be enqueued after crew-assigned create, got 0")
	}
}
