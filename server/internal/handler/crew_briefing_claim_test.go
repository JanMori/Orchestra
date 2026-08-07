package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// claimAgentInstructionsForTest claims the next queued task for runtimeID and
// returns the claimed task id plus the agent Instructions carried on the claim
// response (the field the crew-leader briefing is injected into). Empty task
// id means no task was claimed.
func claimAgentInstructionsForTest(t *testing.T, runtimeID string) (taskID string, instructions string, raw string) {
	t.Helper()

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "crew-briefing-claim")
	req = withURLParam(req, "runtimeId", runtimeID)

	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Task *struct {
			ID    string `json:"id"`
			Agent *struct {
				Instructions string `json:"instructions"`
			} `json:"agent"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if resp.Task == nil {
		return "", "", w.Body.String()
	}
	var instr string
	if resp.Task.Agent != nil {
		instr = resp.Task.Agent.Instructions
	}
	return resp.Task.ID, instr, w.Body.String()
}

// crewBriefingClaimFixture wires a runtime + leader agent + crew and returns
// the IDs needed to enqueue leader tasks against that runtime.
type crewBriefingClaimFixture struct {
	RuntimeID string
	AgentID   string // crew leader, has the runtime and empty instructions
	CrewID   string
	IssueID   string // assignee_type='agent' (NOT crew) — reproduces MUL-3724
}

func newCrewBriefingClaimFixture(t *testing.T, ctx context.Context, name string) crewBriefingClaimFixture {
	t.Helper()

	runtimeID := createClaimReclaimRuntime(t, ctx, name+" runtime")
	// Leader agent + an issue assigned to that agent (assignee_type='agent').
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, name+" leader")
	// Force empty instructions so the test asserts the briefing alone — this
	// mirrors MUL-3724 where the leader's own instructions were blank.
	if _, err := testPool.Exec(ctx, `UPDATE agent SET instructions = '' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("clear leader instructions: %v", err)
	}
	// Make the issue assignee an agent (NOT the crew). The pre-fix code only
	// injected the briefing when issue.assignee_type='crew', so this is the
	// exact gap the fix closes: a comment @crew-mention leader task running on
	// an agent-assigned issue.
	if _, err := testPool.Exec(ctx, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, agentID); err != nil {
		t.Fatalf("set issue agent assignee: %v", err)
	}

	var crewID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO crew (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, name+" crew", agentID, testUserID).Scan(&crewID); err != nil {
		t.Fatalf("create crew: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM crew WHERE id = $1`, crewID) })

	return crewBriefingClaimFixture{
		RuntimeID: runtimeID,
		AgentID:   agentID,
		CrewID:   crewID,
		IssueID:   issueID,
	}
}

func enqueueClaimTask(t *testing.T, ctx context.Context, fx crewBriefingClaimFixture, isLeader bool, withCrewID bool) string {
	t.Helper()
	var taskID string
	var crewArg any
	if withCrewID {
		crewArg = fx.CrewID
	} else {
		crewArg = nil
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, is_leader_task, crew_id)
		VALUES ($1, $2, $3, 'queued', 0, $4, $5)
		RETURNING id
	`, fx.AgentID, fx.RuntimeID, fx.IssueID, isLeader, crewArg).Scan(&taskID); err != nil {
		t.Fatalf("enqueue claim task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID
}

// TestClaim_LeaderTaskFromCommentMention_InjectsBriefing is the MUL-3724
// reproduction: a leader task (is_leader_task=true) carrying a crew_id, on an
// issue assigned to a plain AGENT (not the crew). The pre-fix gate
// (issue.assignee_type='crew') would NOT inject the briefing here, so the
// leader booted with no crew context and degraded into doing the work itself.
// After the fix the briefing is keyed off the task flag + crew_id, so it is
// injected regardless of issue assignee.
func TestClaim_LeaderTaskFromCommentMention_InjectsBriefing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newCrewBriefingClaimFixture(t, ctx, "Briefing inject")
	want := enqueueClaimTask(t, ctx, fx, true /*isLeader*/, true /*withCrewID*/)

	got, instr, raw := claimAgentInstructionsForTest(t, fx.RuntimeID)
	if got != want {
		t.Fatalf("claimed task id = %q, want %q: %s", got, want, raw)
	}
	if !strings.Contains(instr, "## Crew Operating Protocol") || !strings.Contains(instr, "## Crew Roster") {
		t.Fatalf("expected crew-leader briefing in agent instructions, got:\n%s", instr)
	}
}

// TestClaim_NonLeaderTask_NoBriefing guards the negative: a task that is NOT a
// leader task (is_leader_task=false), even with a crew_id present, must not
// receive the briefing. This keeps worker/mention runs free of leader framing.
func TestClaim_NonLeaderTask_NoBriefing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newCrewBriefingClaimFixture(t, ctx, "Briefing nonleader")
	enqueueClaimTask(t, ctx, fx, false /*isLeader*/, true /*withCrewID*/)

	_, instr, _ := claimAgentInstructionsForTest(t, fx.RuntimeID)
	if strings.Contains(instr, "## Crew Operating Protocol") || strings.Contains(instr, "## Crew Roster") {
		t.Fatalf("non-leader task must NOT get crew briefing, got:\n%s", instr)
	}
}

// TestClaim_LeaderTaskWithDanglingCrewID_NoBriefing is the load-bearing
// contract for dropping the FK on agent_task_queue.crew_id (migration 127):
// when a crew is hard-deleted AFTER a leader task was enqueued, the task row
// keeps a now-dangling crew_id. The claim must still succeed (HTTP 200, task
// delivered) and simply skip briefing injection — GetCrewInWorkspace returns
// no row, so the err != nil guard makes this identical to "condition not
// matched". Never a 500, never a stale/empty briefing. Without the FK nothing
// in the DB prevents the dangling row, so this guard lives entirely in the
// claim handler and must stay tested.
func TestClaim_LeaderTaskWithDanglingCrewID_NoBriefing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newCrewBriefingClaimFixture(t, ctx, "Briefing dangling")
	want := enqueueClaimTask(t, ctx, fx, true /*isLeader*/, true /*withCrewID*/)

	// Hard-delete the crew AFTER enqueue, leaving task.crew_id dangling.
	// There is no FK (migration 127), so the task row is untouched.
	if _, err := testPool.Exec(ctx, `DELETE FROM crew WHERE id = $1`, fx.CrewID); err != nil {
		t.Fatalf("delete crew: %v", err)
	}
	// Confirm the task still carries the (now orphaned) crew_id — i.e. the
	// delete did not cascade/null it, which is the whole point of no-FK.
	var stillSet bool
	if err := testPool.QueryRow(ctx,
		`SELECT crew_id = $2 FROM agent_task_queue WHERE id = $1`, want, fx.CrewID,
	).Scan(&stillSet); err != nil {
		t.Fatalf("reload task crew_id: %v", err)
	}
	if !stillSet {
		t.Fatalf("expected task.crew_id to remain the dangling UUID after crew delete (no FK)")
	}

	got, instr, raw := claimAgentInstructionsForTest(t, fx.RuntimeID)
	if got != want {
		t.Fatalf("claimed task id = %q, want %q (claim must still succeed 200): %s", got, want, raw)
	}
	if strings.Contains(instr, "## Crew Operating Protocol") || strings.Contains(instr, "## Crew Roster") {
		t.Fatalf("dangling crew_id must NOT get crew briefing, got:\n%s", instr)
	}
}

// leader tasks enqueued before migration 127 (or by an old binary) have a NULL
// crew_id. The claim handler must skip injection rather than panic or guess —
// equivalent to the pre-fix "condition not matched" behavior, never a stale
// briefing.
func TestClaim_LeaderTaskWithoutCrewID_NoBriefing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newCrewBriefingClaimFixture(t, ctx, "Briefing nullcrew")
	enqueueClaimTask(t, ctx, fx, true /*isLeader*/, false /*withCrewID*/)

	_, instr, _ := claimAgentInstructionsForTest(t, fx.RuntimeID)
	if strings.Contains(instr, "## Crew Operating Protocol") || strings.Contains(instr, "## Crew Roster") {
		t.Fatalf("leader task with NULL crew_id must NOT get crew briefing, got:\n%s", instr)
	}
}
