package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestCreateComment_CrewMentionStampsCrewIDOnLeaderTask locks the enqueue
// side of the MUL-3730 fix: when a comment @mentions a crew, the leader task
// it enqueues must carry crew_id on the task row, so the daemon claim handler
// can locate the crew and inject the briefing (keyed off is_leader_task +
// crew_id, not issue assignee). The issue here is NOT assigned to the crew —
// exactly the comment-mention path that the old issue-assignee gate missed.
func TestCreateComment_CrewMentionStampsCrewIDOnLeaderTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var leaderID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&leaderID); err != nil {
		t.Fatalf("load leader agent: %v", err)
	}

	var crewID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO crew (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Crew ID Stamp Crew', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, leaderID, testUserID).Scan(&crewID); err != nil {
		t.Fatalf("create crew: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM crew WHERE id = $1`, crewID) })

	// Issue assigned to nobody (definitely not the crew) — the leader task is
	// produced purely by the @crew comment mention.
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, 'crew_id stamp test')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "[@Crew](mention://crew/" + crewID + ") please handle this",
	})
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// The leader task must be queued AND carry crew_id = crewID, with
	// is_leader_task = true.
	var gotCrewID string
	var isLeader bool
	if err := testPool.QueryRow(ctx, `
		SELECT crew_id::text, is_leader_task
		FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
	`, issueID, leaderID).Scan(&gotCrewID, &isLeader); err != nil {
		t.Fatalf("load leader task: %v", err)
	}
	if gotCrewID != crewID {
		t.Fatalf("leader task crew_id = %q, want %q", gotCrewID, crewID)
	}
	if !isLeader {
		t.Fatalf("leader task is_leader_task = false, want true")
	}
}

// TestCreateRetryTask_InheritsCrewID locks the retry-clone contract for the
// MUL-3730 fix: a retried leader task must inherit crew_id from its parent so
// the crew-leader briefing keeps being injected across retries. Parallels
// TestCreateRetryTask_InheritsIsLeaderTask.
func TestCreateRetryTask_InheritsCrewID(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newCrewCommentTriggerFixture(t)
	issueID := uuidToString(fx.Issue.ID)

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	})

	var runtimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.LeaderID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}

	var parentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, attempt, max_attempts, is_leader_task, crew_id)
		VALUES ($1, $2, $3, 'failed', 1, 3, TRUE, $4)
		RETURNING id
	`, fx.LeaderID, runtimeID, issueID, fx.CrewID).Scan(&parentID); err != nil {
		t.Fatalf("seed parent task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1 OR parent_task_id = $1`, parentID)
	})

	child, err := testHandler.Queries.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: util.MustParseUUID(parentID)})
	if err != nil {
		t.Fatalf("CreateRetryTask: %v", err)
	}
	if !child.CrewID.Valid || util.UUIDToString(child.CrewID) != fx.CrewID {
		t.Fatalf("child.CrewID = %v (valid=%v), want %s", util.UUIDToString(child.CrewID), child.CrewID.Valid, fx.CrewID)
	}
	if !child.IsLeaderTask {
		t.Fatalf("child.IsLeaderTask = false, want true (provenance must survive retry)")
	}
}
