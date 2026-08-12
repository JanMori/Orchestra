package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JanMori/Orchestra/server/internal/util"
	db "github.com/JanMori/Orchestra/server/pkg/db/generated"
)

// TestCommentMentionsAnyone covers the pure helper that drives the
// "skip leader on @<anyone>" behavior. Routing-style mentions
// (agent/member/crew/all) count; issue cross-references do not. Kept as a
// unit test so it runs without a database connection.
func TestCommentMentionsAnyone(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "empty", content: "", want: false},
		{name: "plain text", content: "please take a look", want: false},
		{name: "literal at sign only", content: "ping @alice", want: false},
		{name: "agent mention", content: "[@A](mention://agent/11111111-1111-1111-1111-111111111111) handle this", want: true},
		{name: "member mention", content: "[@Bob](mention://member/22222222-2222-2222-2222-222222222222)", want: true},
		{name: "crew mention", content: "[@Crew](mention://crew/44444444-4444-4444-4444-444444444444)", want: true},
		{name: "mention all", content: "[@all](mention://all/all)", want: true},
		{name: "issue mention only", content: "see [ISS-1](mention://issue/33333333-3333-3333-3333-333333333333)", want: false},
		{name: "issue + plain text", content: "see [ISS-1](mention://issue/33333333-3333-3333-3333-333333333333) for context", want: false},
		{name: "agent plus member", content: "[@A](mention://agent/11111111-1111-1111-1111-111111111111) cc [@B](mention://member/22222222-2222-2222-2222-222222222222)", want: true},
		{name: "issue plus member", content: "blocks [ISS-1](mention://issue/33333333-3333-3333-3333-333333333333) — [@Bob](mention://member/22222222-2222-2222-2222-222222222222)", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := commentMentionsAnyone(tc.content); got != tc.want {
				t.Fatalf("commentMentionsAnyone(%q) = %v, want %v", tc.content, got, tc.want)
			}
		})
	}
}

// shouldEnqueueCrewLeaderOnCommentForTest reports whether the shared cascade
// would wake the issue's assigned crew leader.
func shouldEnqueueCrewLeaderOnCommentForTest(ctx context.Context, issue db.Issue, content, authorType, authorID string) bool {
	triggers, _ := testHandler.computeCommentAgentTriggers(ctx, issue, content, nil, authorType, authorID, commentTriggerComputeOptions{})
	return triggersContainIssueAssigneeCrewLeader(triggers)
}

func shouldEnqueueCrewLeaderOnReplyForTest(ctx context.Context, issue db.Issue, content string, parent *db.Comment, authorType, authorID string) bool {
	triggers, _ := testHandler.computeCommentAgentTriggers(ctx, issue, content, parent, authorType, authorID, commentTriggerComputeOptions{})
	return triggersContainIssueAssigneeCrewLeader(triggers)
}

func triggersContainIssueAssigneeCrewLeader(triggers []commentAgentTrigger) bool {
	for _, trigger := range triggers {
		if trigger.Source == commentTriggerSourceIssueAssignee && trigger.Crew != nil {
			return true
		}
	}
	return false
}

// crewCommentTriggerFixture wires a crew assigned to a fresh issue and
// returns the loaded db.Issue plus the leader agent UUID for use in
// cascade integration tests.
type crewCommentTriggerFixture struct {
	Issue    db.Issue
	CrewID  string
	LeaderID string
	OtherID  string // second agent in workspace (with runtime), used as a non-leader @mention target
}

func newCrewCommentTriggerFixture(t *testing.T) crewCommentTriggerFixture {
	t.Helper()
	ctx := context.Background()

	// Reuse the seeded "Handler Test Agent" as the leader — it has a runtime.
	var leaderID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&leaderID); err != nil {
		t.Fatalf("load leader agent: %v", err)
	}

	// Spin up a second agent in the same workspace as a non-leader mention
	// target. createHandlerTestAgent installs a t.Cleanup row deletion.
	otherID := createHandlerTestAgent(t, "Crew Comment Other", nil)

	var crewID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO crew (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "Crew Comment Trigger", leaderID, testUserID).Scan(&crewID); err != nil {
		t.Fatalf("create crew: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM crew WHERE id = $1`, crewID)
	})

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title, assignee_type, assignee_id)
		VALUES ($1, 'member', $2, $3, 'crew', $4)
		RETURNING id
	`, testWorkspaceID, testUserID, "crew comment trigger", crewID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}

	return crewCommentTriggerFixture{
		Issue:    issue,
		CrewID:  crewID,
		LeaderID: leaderID,
		OtherID:  otherID,
	}
}

// TestShouldEnqueueCrewLeaderOnComment_SkipsWhenCommentRoutesElsewhere
// pins the cascade: explicit participant mentions do not also wake the assigned
// crew leader. Issue cross-references are not routing and do not suppress the
// leader.
func TestShouldEnqueueCrewLeaderOnComment_SkipsWhenMemberMentionsAnyone(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCrewCommentTriggerFixture(t)
	ctx := context.Background()

	cases := []struct {
		name        string
		content     string
		authorType  string
		authorID    string
		want        bool
		description string
	}{
		{
			name:        "member plain comment triggers leader",
			content:     "what is the latest on this?",
			authorType:  "member",
			authorID:    testUserID,
			want:        true,
			description: "no @ in body → leader must coordinate as today",
		},
		{
			name:        "member issue cross-reference only triggers leader",
			content:     "blocked by [ISS-1](mention://issue/" + testUserID + ")",
			authorType:  "member",
			authorID:    testUserID,
			want:        true,
			description: "issue mentions are not routing — leader still owns dispatch",
		},
		{
			name:        "member mentions another member skips leader",
			content:     "[@self](mention://member/" + testUserID + ") please weigh in",
			authorType:  "member",
			authorID:    testUserID,
			want:        false,
			description: "user routed at a human — leader stays out (extended rule)",
		},
		{
			name:        "member mentions non-leader agent skips leader",
			content:     "[@Other](mention://agent/" + fx.OtherID + ") please take this",
			authorType:  "member",
			authorID:    testUserID,
			want:        false,
			description: "user routed at an agent — leader stays out",
		},
		{
			name:        "member mentions leader skips leader on comment path",
			content:     "[@Leader](mention://agent/" + fx.LeaderID + ") your call",
			authorType:  "member",
			authorID:    testUserID,
			want:        false,
			description: "even @leader is dispatched via the mention path; comment path must not double-enqueue",
		},
		{
			name:        "member mention all skips leader",
			content:     "[@all](mention://all/all) heads up",
			authorType:  "member",
			authorID:    testUserID,
			want:        false,
			description: "@all is a broadcast — leader does not need to wake to evaluate routing",
		},
		{
			name:        "member mentions a crew skips leader",
			content:     "handing to [@Other Crew](mention://crew/" + fx.CrewID + ")",
			authorType:  "member",
			authorID:    testUserID,
			want:        false,
			description: "@crew routes the issue to that crew's leader — current leader stays out",
		},
		{
			name:        "agent comment with @agent does not also trigger leader",
			content:     "delegating to [@Other](mention://agent/" + fx.OtherID + ")",
			authorType:  "agent",
			authorID:    fx.OtherID,
			want:        false,
			description: "explicit @agent routes only to the mentioned target; the assigned crew-leader fallback must not also fire (no double-enqueue)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldEnqueueCrewLeaderOnCommentForTest(ctx, fx.Issue, tc.content, tc.authorType, tc.authorID)
			if got != tc.want {
				t.Fatalf("%s\n  content=%q author=%s/%s\n  got=%v want=%v",
					tc.description, tc.content, tc.authorType, tc.authorID, got, tc.want)
			}
		})
	}
}

// TestShouldEnqueueCrewLeaderOnComment_AgentAuthoredWorkerCommentsWakeLeader
// pins the ISS-3879 restored behavior in the new ISS-3794 cascade: an
// agent-authored worker-result comment on a crew-assigned issue wakes the
// assigned crew leader so the leader→worker→leader coordination loop stays
// closed, while the leader's own self-trigger loop stays suppressed.
func TestShouldEnqueueCrewLeaderOnComment_AgentAuthoredWorkerCommentsWakeLeader(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCrewCommentTriggerFixture(t)
	ctx := context.Background()
	issueID := uuidToString(fx.Issue.ID)

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	})

	clearTasks := func() {
		if _, err := testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID); err != nil {
			t.Fatalf("clear tasks: %v", err)
		}
	}
	// insertLeaderTask seeds a same-crew task for the leader agent so the
	// self-trigger guard can read the agent's most recent role on the issue.
	// Separate Exec calls get distinct created_at values, so the last inserted
	// row is the "latest" task.
	insertLeaderTask := func(isLeader bool, status string) {
		t.Helper()
		var runtimeID string
		if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.LeaderID).Scan(&runtimeID); err != nil {
			t.Fatalf("load runtime: %v", err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, is_leader_task, crew_id)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, fx.LeaderID, runtimeID, issueID, status, isLeader, fx.CrewID); err != nil {
			t.Fatalf("insert task: %v", err)
		}
	}

	// Case 1: a worker agent (not the leader) posts a result comment on the
	// crew-assigned issue — the assigned leader must wake to coordinate.
	t.Run("worker agent comment wakes crew leader", func(t *testing.T) {
		clearTasks()
		if got := shouldEnqueueCrewLeaderOnCommentForTest(ctx, fx.Issue, "pushed the fix, PR is up", "agent", fx.OtherID); !got {
			t.Fatalf("worker agent comment: expected leader to wake, got skip")
		}
	})

	// Case 2: a dual-role agent (leader of the crew, also runs worker tasks)
	// posts while its latest task on the issue was a worker task — the leader
	// role must still wake because the comment is a worker result, not a
	// leader self-trigger.
	t.Run("dual-role worker comment wakes leader when latest task is worker", func(t *testing.T) {
		clearTasks()
		insertLeaderTask(true, "completed")  // older leader task
		insertLeaderTask(false, "completed") // newer worker task → latest role is worker
		if got := shouldEnqueueCrewLeaderOnCommentForTest(ctx, fx.Issue, "done with my worker slice", "agent", fx.LeaderID); !got {
			t.Fatalf("dual-role worker comment: expected leader to wake, got skip")
		}
	})

	// Case 3: the leader posts while its latest task was a leader task — this
	// is a self-trigger loop and must stay suppressed.
	t.Run("leader comment from latest leader task does not self-trigger", func(t *testing.T) {
		clearTasks()
		insertLeaderTask(false, "completed") // older worker task
		insertLeaderTask(true, "completed")  // newer leader task → latest role is leader
		if got := shouldEnqueueCrewLeaderOnCommentForTest(ctx, fx.Issue, "coordinating next steps", "agent", fx.LeaderID); got {
			t.Fatalf("leader self-trigger: expected skip, got wake")
		}
	})

	// Case 4: an agent-authored comment carrying an explicit @agent mention
	// routes only to the mentioned target — the assigned crew leader must NOT
	// also be enqueued via the fallback path (no double-enqueue).
	t.Run("explicit mention does not double-enqueue assigned leader", func(t *testing.T) {
		clearTasks()
		content := "handing to [@Other](mention://agent/" + fx.OtherID + ")"
		if got := shouldEnqueueCrewLeaderOnCommentForTest(ctx, fx.Issue, content, "agent", fx.LeaderID); got {
			t.Fatalf("explicit mention: expected no assigned-leader fallback, got wake")
		}
	})
}

// TestCreateComment_CrewPlainReplyToMemberParentKeepsRootMentionOwner drives the
// full CreateComment handler to lock the cascade's reply behavior:
//
//   - A member top-level comment that @mentions another agent does NOT
//     enqueue the crew leader (the mentioned agent owns the next step).
//   - A subsequent member reply to that member-authored root with no explicit
//     agent mention continues to the root owner instead of the assignee.
func TestCreateComment_CrewPlainReplyToMemberParentKeepsRootMentionOwner(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newCrewCommentTriggerFixture(t)
	issueID := uuidToString(fx.Issue.ID)

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
	})

	countQueued := func(agentID string) int {
		var n int
		if err := testPool.QueryRow(ctx,
			`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
			issueID, agentID,
		).Scan(&n); err != nil {
			t.Fatalf("count tasks for %s: %v", agentID, err)
		}
		return n
	}

	postMemberComment := func(body map[string]any) CommentResponse {
		t.Helper()
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/issues/"+issueID+"/comments", body)
		r = withURLParam(r, "id", issueID)
		testHandler.CreateComment(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp CommentResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode comment: %v", err)
		}
		return resp
	}

	// 1. Member top-level comment mentions OtherAgent.
	//    Leader must be skipped; OtherAgent must be enqueued via the mention path.
	parent := postMemberComment(map[string]any{
		"content": "[@Other](mention://agent/" + fx.OtherID + ") please take this",
	})
	if got := countQueued(fx.LeaderID); got != 0 {
		t.Fatalf("after parent (@OtherAgent): expected 0 leader tasks (skipped), got %d", got)
	}
	if got := countQueued(fx.OtherID); got != 1 {
		t.Fatalf("after parent (@OtherAgent): expected 1 OtherAgent task (mention path), got %d", got)
	}

	// 2. Mark OtherAgent's parent task done so queued-task counts below only
	//    reflect what the plain reply does.
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue SET status = 'completed'
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
	`, issueID, fx.OtherID); err != nil {
		t.Fatalf("complete OtherAgent parent task: %v", err)
	}

	// 3. Member posts a reply in the same thread with NO mentions.
	//    The root's @OtherAgent mention owns the thread, so the reply returns
	//    to OtherAgent instead of falling back to the assigned crew leader.
	postMemberComment(map[string]any{
		"content":   "any update?",
		"parent_id": parent.ID,
	})
	if got := countQueued(fx.LeaderID); got != 0 {
		t.Fatalf("after plain reply: expected 0 leader tasks, got %d", got)
	}
	if got := countQueued(fx.OtherID); got != 1 {
		t.Fatalf("after plain reply: expected 1 OtherAgent task, got %d", got)
	}
}

// TestCreateComment_DualRoleAgentWorkerCommentWakesLeader pins the ISS-3879
// restored coordination loop at the full-handler level. Scenario:
//
//   - Agent L is the leader of crew S and also runs worker tasks on issues
//     belonging to S.
//   - L is woken in its worker role (is_leader_task=false) and posts a result
//     comment.
//   - A leader-role task IS enqueued so the crew leader can coordinate the
//     next step — the worker result must not silently strand the issue.
func TestCreateComment_DualRoleAgentWorkerCommentWakesLeader(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newCrewCommentTriggerFixture(t)
	issueID := uuidToString(fx.Issue.ID)

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
	})

	// Seed a same-crew worker task for the leader agent on this issue so the
	// guard infers "agent's last activity was a worker task" — i.e. L is
	// running in its worker role when it posts the comment. We make it running
	// (not completed) so we can hand its ID back through X-Task-ID for the
	// resolveActor agent-identity check.
	var runtimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.LeaderID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	var workerTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, is_leader_task, crew_id)
		VALUES ($1, $2, $3, 'running', FALSE, $4)
		RETURNING id
	`, fx.LeaderID, runtimeID, issueID, fx.CrewID).Scan(&workerTaskID); err != nil {
		t.Fatalf("seed worker task: %v", err)
	}

	// L posts a comment in its agent identity (X-Agent-ID + X-Task-ID, the
	// pair required by resolveActor to trust the agent header).
	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "done — pushed the change",
	})
	r.Header.Set("X-Agent-ID", fx.LeaderID)
	r.Header.Set("X-Task-ID", workerTaskID)
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// A new leader-role task is enqueued so the leader coordinates next steps.
	var leaderTasks int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued' AND is_leader_task = TRUE
	`, issueID, fx.LeaderID).Scan(&leaderTasks); err != nil {
		t.Fatalf("count leader tasks: %v", err)
	}
	if leaderTasks != 1 {
		t.Fatalf("after worker comment from dual-role agent: expected 1 queued leader task, got %d", leaderTasks)
	}
}

// TestCreateComment_CrewLeaderMentionTaskDoesNotSelfTriggerAssignedFallback
// pins ISS-4024's direct-mention gap:
//
//   - A member explicitly @mentions the issue's assigned crew leader by agent
//     id, which queues a generic mention task for L (is_leader_task=false,
//     crew_id=NULL).
//   - L posts a plain reply while running that mention task.
//   - The assigned-crew fallback must not treat that generic mention task as a
//     same-crew worker result and queue L again as the leader.
func TestCreateComment_CrewLeaderMentionTaskDoesNotSelfTriggerAssignedFallback(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newCrewCommentTriggerFixture(t)
	issueID := uuidToString(fx.Issue.ID)

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
	})

	postMemberComment := func(body map[string]any) CommentResponse {
		t.Helper()
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/issues/"+issueID+"/comments", body)
		r = withURLParam(r, "id", issueID)
		testHandler.CreateComment(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateComment(member): expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp CommentResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode member comment: %v", err)
		}
		return resp
	}
	postAgentComment := func(taskID string, body map[string]any) {
		t.Helper()
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/issues/"+issueID+"/comments", body)
		r.Header.Set("X-Agent-ID", fx.LeaderID)
		r.Header.Set("X-Task-ID", taskID)
		r = withURLParam(r, "id", issueID)
		testHandler.CreateComment(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateComment(agent): expected 201, got %d: %s", w.Code, w.Body.String())
		}
	}
	countQueuedLeaderTasks := func() int {
		t.Helper()
		var n int
		if err := testPool.QueryRow(ctx, `
			SELECT count(*) FROM agent_task_queue
			WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued' AND is_leader_task = TRUE
		`, issueID, fx.LeaderID).Scan(&n); err != nil {
			t.Fatalf("count queued leader tasks: %v", err)
		}
		return n
	}

	trigger := postMemberComment(map[string]any{
		"content": "[@Leader](mention://agent/" + fx.LeaderID + ") can you check this?",
	})

	var mentionTaskID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
		  AND is_leader_task = FALSE AND crew_id IS NULL
		ORDER BY created_at DESC
		LIMIT 1
	`, issueID, fx.LeaderID).Scan(&mentionTaskID); err != nil {
		t.Fatalf("load leader mention task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'running' WHERE id = $1`, mentionTaskID); err != nil {
		t.Fatalf("mark mention task running: %v", err)
	}

	postAgentComment(mentionTaskID, map[string]any{
		"content":   "checked, no action needed",
		"parent_id": trigger.ID,
	})

	if got := countQueuedLeaderTasks(); got != 0 {
		t.Fatalf("leader reply from generic mention task queued %d leader tasks, want 0", got)
	}
}

// TestCreateComment_CrewLeaderThreadParentTaskDoesNotSelfTriggerAssignedFallback
// pins ISS-4024's thread-parent gap: a member reply to the leader's earlier
// comment queues L through EnqueueTaskForThreadParent (is_leader_task=false,
// crew_id=NULL). L's reply from that generic task must not queue L again as
// the assigned crew leader.
func TestCreateComment_CrewLeaderThreadParentTaskDoesNotSelfTriggerAssignedFallback(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newCrewCommentTriggerFixture(t)
	issueID := uuidToString(fx.Issue.ID)

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
	})

	var leaderRuntimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.LeaderID).Scan(&leaderRuntimeID); err != nil {
		t.Fatalf("load leader runtime: %v", err)
	}
	var leaderTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, is_leader_task, crew_id)
		VALUES ($1, $2, $3, 'running', TRUE, $4)
		RETURNING id
	`, fx.LeaderID, leaderRuntimeID, issueID, fx.CrewID).Scan(&leaderTaskID); err != nil {
		t.Fatalf("seed leader task: %v", err)
	}

	postAgentComment := func(taskID string, body map[string]any) CommentResponse {
		t.Helper()
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/issues/"+issueID+"/comments", body)
		r.Header.Set("X-Agent-ID", fx.LeaderID)
		r.Header.Set("X-Task-ID", taskID)
		r = withURLParam(r, "id", issueID)
		testHandler.CreateComment(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateComment(agent): expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp CommentResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode agent comment: %v", err)
		}
		return resp
	}
	postMemberComment := func(body map[string]any) CommentResponse {
		t.Helper()
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/issues/"+issueID+"/comments", body)
		r = withURLParam(r, "id", issueID)
		testHandler.CreateComment(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateComment(member): expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp CommentResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode member comment: %v", err)
		}
		return resp
	}
	countQueuedLeaderTasks := func() int {
		t.Helper()
		var n int
		if err := testPool.QueryRow(ctx, `
			SELECT count(*) FROM agent_task_queue
			WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued' AND is_leader_task = TRUE
		`, issueID, fx.LeaderID).Scan(&n); err != nil {
			t.Fatalf("count queued leader tasks: %v", err)
		}
		return n
	}

	parent := postAgentComment(leaderTaskID, map[string]any{
		"content": "coordinating this issue",
	})
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'completed' WHERE id = $1`, leaderTaskID); err != nil {
		t.Fatalf("complete leader task: %v", err)
	}

	trigger := postMemberComment(map[string]any{
		"content":   "any update?",
		"parent_id": parent.ID,
	})

	var threadParentTaskID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
		  AND is_leader_task = FALSE AND crew_id IS NULL
		ORDER BY created_at DESC
		LIMIT 1
	`, issueID, fx.LeaderID).Scan(&threadParentTaskID); err != nil {
		t.Fatalf("load leader thread-parent task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'running' WHERE id = $1`, threadParentTaskID); err != nil {
		t.Fatalf("mark thread-parent task running: %v", err)
	}

	postAgentComment(threadParentTaskID, map[string]any{
		"content":   "replying from the thread-parent task",
		"parent_id": trigger.ID,
	})

	if got := countQueuedLeaderTasks(); got != 0 {
		t.Fatalf("leader reply from generic thread-parent task queued %d leader tasks, want 0", got)
	}
}

// TestCreateRetryTask_InheritsIsLeaderTask locks the retry-clone contract for
// ISS-2218: auto-retry of a leader-role task must produce a child task that is
// also is_leader_task=true. Without this, MaybeRetryFailedTask silently
// demotes a retried leader task to a worker task, and role-specific claim-time
// briefing/self-mention guards lose the leader provenance.
func TestCreateRetryTask_InheritsIsLeaderTask(t *testing.T) {
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

	cases := []struct {
		name     string
		isLeader bool
	}{
		{name: "leader task retry stays leader", isLeader: true},
		{name: "worker task retry stays worker", isLeader: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var parentID string
			if err := testPool.QueryRow(ctx, `
				INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, attempt, max_attempts, is_leader_task)
				VALUES ($1, $2, $3, 'failed', 1, 3, $4)
				RETURNING id
			`, fx.LeaderID, runtimeID, issueID, tc.isLeader).Scan(&parentID); err != nil {
				t.Fatalf("seed parent task: %v", err)
			}
			t.Cleanup(func() {
				testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1 OR parent_task_id = $1`, parentID)
			})

			child, err := testHandler.Queries.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: util.MustParseUUID(parentID)})
			if err != nil {
				t.Fatalf("CreateRetryTask: %v", err)
			}
			if child.IsLeaderTask != tc.isLeader {
				t.Fatalf("child.IsLeaderTask = %v, want %v (parent role must be inherited)", child.IsLeaderTask, tc.isLeader)
			}
		})
	}
}

// TestCreateComment_CrewMentionPrivateLeaderBlocksPlainMember verifies that
// a plain workspace member cannot trigger a private crew leader via @crew
// mention. This is the regression test for the P1 finding: without the
// canAccessPrivateAgent gate in the crew mention branch, a member could
// bypass the private-agent restriction by mentioning the crew instead of
// the agent directly.
func TestCreateComment_CrewMentionPrivateLeaderBlocksPlainMember(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Use privateAgentTestFixture to get a private agent + plain member.
	agentID, _, memberID := privateAgentTestFixture(t)

	// Create a crew with the private agent as leader.
	var crewID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO crew (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Private Leader Crew', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&crewID); err != nil {
		t.Fatalf("create crew: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM crew WHERE id = $1`, crewID)
	})

	// Create an issue.
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, 'private leader crew mention test')
		RETURNING id
	`, testWorkspaceID, memberID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// Plain member posts a comment mentioning the crew.
	w := httptest.NewRecorder()
	r := newRequestAs(memberID, "POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "[@Crew](mention://crew/" + crewID + ") please handle",
	})
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// The private leader must NOT have a queued task — plain member lacks access.
	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
		issueID, agentID,
	).Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count != 0 {
		t.Fatalf("private leader got %d queued tasks from plain member crew mention; want 0 (access denied)", count)
	}
}

// TestCreateComment_CrewMentionTriggersLeader verifies that @mentioning a
// crew in a comment triggers the crew's leader agent via the mention path,
// even when the issue is NOT assigned to that crew.
func TestCreateComment_CrewMentionTriggersLeader(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Create a crew with a leader agent.
	var leaderID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&leaderID); err != nil {
		t.Fatalf("load leader agent: %v", err)
	}

	var crewID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO crew (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "Mention Trigger Crew", leaderID, testUserID).Scan(&crewID); err != nil {
		t.Fatalf("create crew: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM crew WHERE id = $1`, crewID)
	})

	// Create an issue NOT assigned to the crew (assigned to nobody).
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, 'crew mention trigger test')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	countQueued := func(agentID string) int {
		var n int
		if err := testPool.QueryRow(ctx,
			`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
			issueID, agentID,
		).Scan(&n); err != nil {
			t.Fatalf("count tasks for %s: %v", agentID, err)
		}
		return n
	}

	// Post a comment that @mentions the crew.
	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "[@Crew](mention://crew/" + crewID + ") please handle this",
	})
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// The crew's leader should have a queued task.
	if got := countQueued(leaderID); got != 1 {
		t.Fatalf("after @crew mention: expected 1 leader task, got %d", got)
	}
}
