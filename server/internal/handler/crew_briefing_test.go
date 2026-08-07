package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestCrewOperatingProtocolOwnsParentStatus locks the parent-issue status
// contract: first dispatch moves todo→in_progress and stays there; only a
// later confirmation of overall completion may advance to in_review; done is
// left to humans / integrations.
func TestCrewOperatingProtocolOwnsParentStatus(t *testing.T) {
	protocol := crewOperatingProtocolFor(true)
	compact := strings.Join(strings.Fields(protocol), " ")
	for _, want := range []string{
		"Own the parent issue status",
		"move the parent to `in_progress`",
		"successful dispatch is not completion",
		"orchestra issue status <issue-id> in_review",
		"Leave `done` to a human reviewer",
	} {
		if !strings.Contains(compact, want) {
			t.Errorf("expected crew operating protocol to contain %q\n--- protocol ---\n%s", want, protocol)
		}
	}
}

// TestCrewOperatingProtocolScopesParentStatusOwnership is the guard for the
// MUL-5156 review finding: the briefing is injected on every leader path,
// including an @crew mention on an issue assigned to someone else. Status
// ownership must not ride along — a guest leader gets an explicit prohibition
// instead of the grant, so the model never has to infer the boundary.
func TestCrewOperatingProtocolScopesParentStatusOwnership(t *testing.T) {
	guest := crewOperatingProtocolFor(false)
	compactGuest := strings.Join(strings.Fields(guest), " ")

	for _, want := range []string{
		"Do NOT change this issue's status",
		"not assigned to your crew",
		"never run `orchestra issue status` on it",
	} {
		if !strings.Contains(compactGuest, want) {
			t.Errorf("expected guest-leader protocol to contain %q\n--- protocol ---\n%s", want, guest)
		}
	}
	// The grant must be entirely absent — not merely qualified.
	for _, forbidden := range []string{
		"Own the parent issue status",
		"orchestra issue status <issue-id> in_review",
	} {
		if strings.Contains(compactGuest, forbidden) {
			t.Errorf("guest-leader protocol must not contain status grant %q\n--- protocol ---\n%s", forbidden, guest)
		}
	}

	// Everything that is not the status responsibility is identical, so a
	// guest leader still coordinates, delegates, and records activity.
	owner := crewOperatingProtocolFor(true)
	for _, shared := range []string{
		"## Crew Operating Protocol",
		"Delegate by @mention",
		"Record your evaluation",
		"Stop after dispatching",
		"Never both for the same work.",
	} {
		if !strings.Contains(owner, shared) || !strings.Contains(guest, shared) {
			t.Errorf("expected %q in both protocol variants", shared)
		}
	}

	// The IsCrewLeader marker the daemon greps for must survive both ways,
	// or a guest leader silently loses its no_action / silent-exit behavior.
	if !strings.Contains(guest, "## Crew Operating Protocol") {
		t.Error("guest-leader protocol lost the header the daemon keys IsCrewLeader off")
	}
}

// TestCrewOperatingProtocolWarnsAgainstDualTrigger locks in the rule
// added for #3033: the protocol must tell the crew leader that a `todo`
// child issue with an agent assignee already fires that agent, so they
// must not also @mention the same agent on the parent issue for the
// same work. Asserts behavior, not exact wording — keep the substrings
// narrow so harmless rewording doesn't break the test.
func TestCrewOperatingProtocolWarnsAgainstDualTrigger(t *testing.T) {
	protocol := crewOperatingProtocolFor(true)
	compact := strings.Join(strings.Fields(protocol), " ")
	for _, want := range []string{
		"--status todo` and an agent assignee already fires that agent automatically",
		"Never both for the same work.",
	} {
		if !strings.Contains(compact, want) {
			t.Errorf("expected crew operating protocol to contain %q\n--- protocol ---\n%s", want, protocol)
		}
	}
}

// seedCrewForBriefing creates a crew with the seeded test agent as
// leader. Returns the loaded db.Crew and a cleanup-registered ID.
func seedCrewForBriefing(t *testing.T, leaderID string, name, instructions string) db.Crew {
	t.Helper()
	ctx := context.Background()

	var crewID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO crew (workspace_id, name, description, leader_id, creator_id, instructions)
		VALUES ($1, $2, '', $3, $4, $5)
		RETURNING id
	`, testWorkspaceID, name, leaderID, testUserID, instructions).Scan(&crewID); err != nil {
		t.Fatalf("create crew: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM crew WHERE id = $1`, crewID)
	})

	uuid := util.MustParseUUID(crewID)
	crew, err := testHandler.Queries.GetCrewInWorkspace(ctx, db.GetCrewInWorkspaceParams{
		ID:          uuid,
		WorkspaceID: util.MustParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load crew: %v", err)
	}
	return crew
}

func addAgentMember(t *testing.T, crewID pgtype.UUID, agentID, role string) {
	t.Helper()
	if _, err := testHandler.Queries.AddCrewMember(context.Background(), db.AddCrewMemberParams{
		CrewID:    crewID,
		MemberType: "agent",
		MemberID:   util.MustParseUUID(agentID),
		Role:       role,
	}); err != nil {
		t.Fatalf("add agent member: %v", err)
	}
}

func addHumanMember(t *testing.T, crewID pgtype.UUID, userID, role string) {
	t.Helper()
	if _, err := testHandler.Queries.AddCrewMember(context.Background(), db.AddCrewMemberParams{
		CrewID:    crewID,
		MemberType: "member",
		MemberID:   util.MustParseUUID(userID),
		Role:       role,
	}); err != nil {
		t.Fatalf("add human member: %v", err)
	}
}

// seededLeaderAgent loads the first seeded agent in the test workspace.
func seededLeaderAgent(t *testing.T) (id, name string) {
	t.Helper()
	if err := testPool.QueryRow(context.Background(), `
		SELECT id, name FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&id, &name); err != nil {
		t.Fatalf("load seeded agent: %v", err)
	}
	return id, name
}

// seededHumanMember returns the (member_row_id, user_id, user_name) of the
// test fixture's human member in the workspace.
func seededHumanMember(t *testing.T) (memberID, userID, userName string) {
	t.Helper()
	if err := testPool.QueryRow(context.Background(), `
		SELECT m.id, u.id, u.name
		FROM member m JOIN "user" u ON u.id = m.user_id
		WHERE m.workspace_id = $1 ORDER BY m.created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&memberID, &userID, &userName); err != nil {
		t.Fatalf("load seeded member: %v", err)
	}
	return
}

func TestBuildCrewLeaderBriefing_FullCrew(t *testing.T) {
	ctx := context.Background()
	leaderID, leaderName := seededLeaderAgent(t)

	crew := seedCrewForBriefing(t, leaderID, "Full Crew", "Always write tests.")

	helper1 := createHandlerTestAgent(t, "Helper One", []byte("[]"))
	helper2 := createHandlerTestAgent(t, "Helper Two", []byte("[]"))
	addAgentMember(t, crew.ID, helper1, "implementer")
	addAgentMember(t, crew.ID, helper2, "")

	memberRowID, userID, userName := seededHumanMember(t)
	_ = memberRowID
	addHumanMember(t, crew.ID, userID, "reviewer")

	out := buildCrewLeaderBriefing(ctx, testHandler.Queries, crew, true)

	for _, want := range []string{
		"## Crew Operating Protocol",
		"## Crew Roster",
		"Leader (you):",
		leaderName,
		"## Crew Instructions (Full Crew)",
		"Always write tests.",
		"`[@Helper One](mention://agent/" + helper1 + ")`",
		"`[@Helper Two](mention://agent/" + helper2 + ")`",
		`role: "implementer"`,
		`role: "reviewer"`,
		"`[@" + userName + "](mention://member/" + userID + ")`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected briefing to contain %q\n--- briefing ---\n%s", want, out)
		}
	}

	// Helper Two has no role — must NOT render an empty role: "" segment.
	if strings.Contains(out, `Helper Two — agent, role: ""`) {
		t.Errorf("expected empty role to be omitted, got: %s", out)
	}
}

// assignSkillToAgent creates a workspace skill and attaches it to the agent.
func assignSkillToAgent(t *testing.T, agentID, skillName string) {
	t.Helper()
	ctx := context.Background()
	var skillID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO skill (workspace_id, name, description, content, created_by)
		VALUES ($1, $2, '', '', $3)
		RETURNING id
	`, testWorkspaceID, skillName, testUserID).Scan(&skillID); err != nil {
		t.Fatalf("create skill %s: %v", skillName, err)
	}
	t.Cleanup(func() {
		if _, err := testPool.Exec(ctx, `DELETE FROM agent_skill WHERE agent_id = $1 AND skill_id = $2`, agentID, skillID); err != nil {
			t.Errorf("cleanup agent skill %s/%s: %v", agentID, skillName, err)
		}
		if _, err := testPool.Exec(ctx, `DELETE FROM skill WHERE id = $1`, skillID); err != nil {
			t.Errorf("cleanup skill %s: %v", skillName, err)
		}
	})
	if _, err := testPool.Exec(ctx,
		`INSERT INTO agent_skill (agent_id, skill_id) VALUES ($1, $2)`,
		agentID, skillID,
	); err != nil {
		t.Fatalf("assign skill %s to agent: %v", skillName, err)
	}
}

// TestBuildCrewLeaderBriefing_MemberSkillsInRoster locks in the delegation
// fix: an agent member's assigned skills appear in the leader roster so the
// leader can route by capability. Agents with no skills get an explicit
// marker; human members never carry a skills segment.
func TestBuildCrewLeaderBriefing_MemberSkillsInRoster(t *testing.T) {
	ctx := context.Background()
	leaderID, _ := seededLeaderAgent(t)
	crew := seedCrewForBriefing(t, leaderID, "Skilled Crew", "")

	skilled := createHandlerTestAgent(t, "Skilled Bot", []byte("[]"))
	addAgentMember(t, crew.ID, skilled, "backend")
	// ListAgentSkillNamesByAgentIDs orders by name ASC → "polars" before "stat…".
	assignSkillToAgent(t, skilled, "polars")
	assignSkillToAgent(t, skilled, "statistical-analysis")

	plain := createHandlerTestAgent(t, "Plain Bot", []byte("[]"))
	addAgentMember(t, crew.ID, plain, "")

	memberRowID, userID, userName := seededHumanMember(t)
	_ = memberRowID
	addHumanMember(t, crew.ID, userID, "reviewer")

	out := buildCrewLeaderBriefing(ctx, testHandler.Queries, crew, true)

	if !strings.Contains(out, "skills: polars, statistical-analysis") {
		t.Errorf("expected skilled member skills in roster, got:\n%s", out)
	}
	if !strings.Contains(out, "Plain Bot — agent — no skills assigned") {
		t.Errorf("expected no-skills marker for skill-less agent, got:\n%s", out)
	}
	if strings.Contains(out, userName+" — member (human), role: \"reviewer\" — skills:") ||
		strings.Contains(out, userName+" — member (human), role: \"reviewer\" — no skills") {
		t.Errorf("human member must not render a skills segment, got:\n%s", out)
	}
}

func TestBuildCrewLeaderBriefing_OnlyLeader(t *testing.T) {
	ctx := context.Background()
	leaderID, _ := seededLeaderAgent(t)
	crew := seedCrewForBriefing(t, leaderID, "Solo Crew", "")

	out := buildCrewLeaderBriefing(ctx, testHandler.Queries, crew, true)
	if !strings.Contains(out, "Members: (none — you are the only member of this crew)") {
		t.Errorf("expected lone-leader fallback line, got:\n%s", out)
	}
	// No user instructions → no Crew Instructions section.
	if strings.Contains(out, "## Crew Instructions") {
		t.Errorf("expected no Crew Instructions section when empty, got:\n%s", out)
	}
}

func TestBuildCrewLeaderBriefing_SkipsArchivedAgent(t *testing.T) {
	ctx := context.Background()
	leaderID, _ := seededLeaderAgent(t)
	crew := seedCrewForBriefing(t, leaderID, "Archive Crew", "")

	archived := createHandlerTestAgent(t, "Retired Bot", []byte("[]"))
	addAgentMember(t, crew.ID, archived, "")
	if _, err := testPool.Exec(ctx,
		`UPDATE agent SET archived_at = now(), archived_by = $1 WHERE id = $2`,
		testUserID, archived,
	); err != nil {
		t.Fatalf("archive agent: %v", err)
	}

	out := buildCrewLeaderBriefing(ctx, testHandler.Queries, crew, true)
	if strings.Contains(out, "Retired Bot") {
		t.Errorf("archived agent should not appear in roster:\n%s", out)
	}
	if strings.Contains(out, archived) {
		t.Errorf("archived agent UUID should not appear in roster:\n%s", out)
	}
}

// TestBuildCrewLeaderBriefing_MentionsRoundTrip is the contract test
// guaranteeing every emitted mention markdown string parses back through
// util.ParseMentions to its (type, id). If this ever breaks, the leader's
// dispatch comments will silently fail to trigger anyone.
func TestBuildCrewLeaderBriefing_MentionsRoundTrip(t *testing.T) {
	ctx := context.Background()
	leaderID, _ := seededLeaderAgent(t)
	crew := seedCrewForBriefing(t, leaderID, "Mention Round Trip", "")

	helper := createHandlerTestAgent(t, "Round Trip Bot", []byte("[]"))
	addAgentMember(t, crew.ID, helper, "")

	memberRowID, userID, _ := seededHumanMember(t)
	_ = memberRowID
	addHumanMember(t, crew.ID, userID, "")

	out := buildCrewLeaderBriefing(ctx, testHandler.Queries, crew, true)
	mentions := util.ParseMentions(out)

	wantIDs := map[string]string{
		leaderID: "agent",
		helper:   "agent",
		userID:   "member",
	}
	got := make(map[string]string, len(mentions))
	for _, m := range mentions {
		got[m.ID] = m.Type
	}
	for id, kind := range wantIDs {
		if got[id] != kind {
			t.Errorf("expected %s mention for id %s, got %q (all parsed: %#v)", kind, id, got[id], mentions)
		}
	}
}

// claimAndDecodeAgent runs ClaimTaskByRuntime for the given runtime and
// returns the agent block of the response. Fails the test on non-200.
func claimAndDecodeAgent(t *testing.T, runtimeID string) *TaskAgentData {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, "test-claim-crew-briefing")
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Task *struct {
			Agent *TaskAgentData `json:"agent"`
		} `json:"task"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Task == nil || resp.Task.Agent == nil {
		t.Fatalf("expected task.agent in response, got: %s", w.Body.String())
	}
	return resp.Task.Agent
}

// queueCrewIssueTaskFor creates an issue assigned to the crew and a queued
// task for the given (agentID, runtimeID). Returns the issue + task IDs.
func queueCrewIssueTaskFor(t *testing.T, crewID, agentID, runtimeID string, issueNumber int) (issueID, taskID string) {
	t.Helper()
	ctx := context.Background()
	if err := testPool.QueryRow(ctx, `
INSERT INTO issue (
workspace_id, title, status, priority, creator_id, creator_type,
assignee_type, assignee_id, number, position
) VALUES ($1, 'Crew briefing claim test', 'todo', 'medium', $2, 'member',
'crew', $3, $4, 0)
RETURNING id
`, testWorkspaceID, testUserID, crewID, issueNumber).Scan(&issueID); err != nil {
		t.Fatalf("create crew-assigned issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, is_leader_task, crew_id)
VALUES ($1, $2, $3, 'queued', 0,
        ($1::uuid = (SELECT leader_id FROM crew WHERE id = $4::uuid)),
        $4::uuid)
RETURNING id
`, agentID, runtimeID, issueID, crewID).Scan(&taskID); err != nil {
		t.Fatalf("queue task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return
}

// TestClaimTask_LeaderGetsBriefing — when the crew leader claims a task on
// a crew-assigned issue, the response's agent.instructions must include
// the Operating Protocol + Roster + user instructions.
func TestClaimTask_LeaderGetsBriefing(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var leaderID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&leaderID, &runtimeID); err != nil {
		t.Fatalf("get leader agent: %v", err)
	}

	crew := seedCrewForBriefing(t, leaderID, "Briefing Claim Crew", "Be terse.")

	helper := createHandlerTestAgent(t, "Briefing Helper", []byte("[]"))
	addAgentMember(t, crew.ID, helper, "implementer")

	queueCrewIssueTaskFor(t, util.UUIDToString(crew.ID), leaderID, runtimeID, 95001)

	agent := claimAndDecodeAgent(t, runtimeID)
	for _, want := range []string{
		"## Crew Operating Protocol",
		"## Crew Roster",
		"Leader (you):",
		"## Crew Instructions (Briefing Claim Crew)",
		"Be terse.",
		"`[@Briefing Helper](mention://agent/" + helper + ")`",
	} {
		if !strings.Contains(agent.Instructions, want) {
			t.Errorf("expected agent.instructions to contain %q\n--- instructions ---\n%s", want, agent.Instructions)
		}
	}
}

// TestClaimTask_NonLeaderGetsNoBriefing — when a non-leader crew member
// claims a task on a crew-assigned issue, NO briefing is injected.
func TestClaimTask_NonLeaderGetsNoBriefing(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var leaderID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&leaderID); err != nil {
		t.Fatalf("get leader agent: %v", err)
	}

	crew := seedCrewForBriefing(t, leaderID, "Non-Leader Crew", "Crew guidance.")

	// Create a second agent (NOT the leader) with its own runtime so the
	// claim path picks its task without ambiguity.
	helperID := createHandlerTestAgent(t, "Non Leader Helper", []byte("[]"))
	addAgentMember(t, crew.ID, helperID, "")
	var helperRuntime string
	if err := testPool.QueryRow(ctx,
		`SELECT runtime_id FROM agent WHERE id = $1`, helperID,
	).Scan(&helperRuntime); err != nil {
		t.Fatalf("get helper runtime: %v", err)
	}

	queueCrewIssueTaskFor(t, util.UUIDToString(crew.ID), helperID, helperRuntime, 95002)

	agent := claimAndDecodeAgent(t, helperRuntime)
	for _, mustNot := range []string{
		"Crew Operating Protocol",
		"Crew Roster",
		"Crew Instructions (Non-Leader Crew)",
	} {
		if strings.Contains(agent.Instructions, mustNot) {
			t.Errorf("non-leader claim should NOT contain %q\n--- instructions ---\n%s", mustNot, agent.Instructions)
		}
	}
}

// Avoid "imported and not used: pgtype" if helpers above are the only users.
var _ pgtype.UUID
