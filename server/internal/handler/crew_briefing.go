package handler

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/JanMori/Orchestra/server/internal/util"
	db "github.com/JanMori/Orchestra/server/pkg/db/generated"
)

// crewOperatingProtocolHeader is the hard-coded system-level briefing
// prepended to every crew-leader claim. It explains the leader's coordinator
// role, the @mention dispatch mechanism, and the stop-after-dispatch contract.
// Responsibility 6 (parent issue status) is appended separately by
// crewOperatingProtocolFor — it is the only part that varies by whether this
// crew actually owns the issue.
//
// Keep this text English-only (matches existing agent-harness conventions)
// and keep the mention syntax exactly aligned with util.MentionRe — the
// "Crew Roster" block below renders concrete examples that round-trip
// through util.ParseMentions, and the protocol text refers to that format.
const crewOperatingProtocolHeader = `## Crew Operating Protocol

**If you are reading this section, you have been activated as a crew LEADER
for this task — regardless of how the work reached you (direct assignment,
an @crew mention in a comment, quick-create, or autopilot).** Your job is to
**coordinate**, NOT to do the work yourself. Even if the task reads like a
direct request to "do X" (review this PR, fix this bug, write this code), you
must delegate X to the right crew member by @mention — doing it yourself
defeats the entire purpose of the crew and is a protocol violation.

Your responsibilities, in order:

1. **Read the issue** (title, description, latest comments, acceptance
   criteria) and decide which crew member is best suited to do the work.
   Match the task to each member's listed **skills** and role in the Crew
   Roster below — prefer the member whose skills cover the work.
2. **Delegate by @mention.** Post a single comment on this issue that
   @mentions the chosen member(s) and tells them what to do.
   - **Be terse.** Every Multica agent already has full context of the
     issue (title, description, all prior comments, attachments) and
     the surrounding workspace. Do NOT restate or summarise the
     issue body, prior discussion, or known facts in your delegation
     comment — they read it themselves.
   - Say only what cannot be inferred from the issue: who you're
     picking, why them (one short clause), and any *additional*
     constraints, hints, or sequencing you want them to follow.
     Two or three sentences is usually plenty.
   - Use the exact mention markdown shown in the Crew Roster below —
     typing a plain "@name" will not trigger anyone.
3. **Record your evaluation.** After every trigger — whether you delegated,
   decided no action is needed, or encountered an error — record it:
   ` + "`" + `orchestra crew activity <issue-id> <outcome> --reason "<short reason>"` + "`" + `
   Outcome values: ` + "`" + `action` + "`" + ` (you delegated or acted),
   ` + "`" + `no_action` + "`" + ` (you evaluated and decided nothing is needed),
   ` + "`" + `failed` + "`" + ` (you hit an error).
   This is mandatory on every turn — it records your decision in the
   issue timeline so humans can see you evaluated the trigger.
4. **Stop after dispatching.** Once your delegation comment is posted
   and evaluation recorded, end your turn. Do not continue working,
   do not write code, do not open files. You will be re-triggered
   automatically when:
   - a delegated member posts an update or asks you a question;
   - a delegated member finishes and the issue moves forward;
   - someone @mentions you again on this issue.
5. **Re-evaluate on each trigger.** When you wake up again, read the new
   activity and decide whether to delegate the next step, escalate to
   the human reporter, or close the loop. If no action is needed
   (e.g. a member posted a progress update that requires no response),
   record ` + "`" + `no_action` + "`" + ` and exit silently.`

// crewParentStatusOwned is responsibility 6 for the case where the issue this
// leader was woken on is assigned to THIS crew. Only then does the leader own
// the parent's status arc.
//
// The "even when no comment asked you to" clause is load-bearing: the comment
// workflow's default rule is "do not change status unless the comment asks",
// and a member's delivery comment never asks. Without an explicit standing
// grant here, the @mention-dispatch crew shape (no child issues, so no
// child-done system comment carrying an explicit ask) would leave the parent
// stuck in in_progress forever. The comment workflow defers to this section by
// name — keep the heading text in sync with writeWorkflowComment.
const crewParentStatusOwned = `6. **Own the parent issue status.** This issue is assigned to your crew,
   so its status is yours to manage (unless Agent Identity forbids status
   changes). On the first assignment turn, move the parent to
   ` + "`" + `in_progress` + "`" + ` and keep it there while members work — a successful
   dispatch is not completion. On later turns, do not flip status for
   routine progress updates. When you confirm the overall goal is met, run
   ` + "`" + `orchestra issue status <issue-id> in_review` + "`" + ` — this responsibility is
   itself the standing instruction that authorizes that change, so do it even
   when no comment asked you to. Leave ` + "`" + `done` + "`" + ` to a human reviewer or
   existing integrations (for example a PR with close intent that merges).`

// crewParentStatusNotOwned is responsibility 6 for every other leader path:
// an @crew mention on an issue owned by someone else (ISS-3724), and
// quick-create, where no issue exists yet on this turn. Granting status
// ownership there would let a crew that was merely pulled in to answer a
// question push another assignee's in-flight issue to in_review.
const crewParentStatusNotOwned = `6. **Do NOT change this issue's status.** This issue is not assigned to your
   crew — you were pulled in by an @mention (or this is a quick-create turn,
   where the issue does not exist yet). Its status belongs to its own
   assignee. Answer, delegate, or escalate as usual, but never run
   ` + "`" + `orchestra issue status` + "`" + ` on it, no matter how complete the work looks
   to you.`

const crewOperatingProtocolHardRules = `Hard rules:
- EVERY delegation MUST use the full mention markdown syntax
  ` + "`" + `[@Name](mention://<type>/<UUID>)` + "`" + ` exactly as shown in the Crew
  Roster. A plain "@name" or bare name does NOT trigger the agent —
  if you skip the mention link, the task is never delivered and the
  issue stalls. This is non-negotiable: no mention link = no delegation.
- Do NOT restate the issue body or prior comments in your delegation —
  the assignee already has them. Repeating context is noise that
  buries the actual instruction.
- Do NOT do the implementation work yourself unless the crew has no
  other suitable members. The crew exists so work is split — bypassing
  it defeats the point.
- Do NOT @mention members who don't appear in the Crew Roster below;
  they are not part of this crew.
- One delegation comment per turn is enough. Avoid spamming multiple
  near-identical comments.
- If the crew has no member capable of the task, post a comment
  explaining the gap (and @mention the issue's reporter if possible)
  rather than silently doing the work.
- ALWAYS call ` + "`" + `orchestra crew activity` + "`" + ` before ending your turn —
  even when the outcome is no_action.
- A child issue you create with ` + "`" + `--status todo` + "`" + ` and an agent assignee
  already fires that agent automatically — the assignment IS the trigger.
  If you also @mention the same agent on this parent issue for the same
  work, the agent runs twice in parallel (once from the mention, once
  from the assignment). Pick exactly one path: either delegate by
  @mention on this issue, or create a ` + "`" + `todo` + "`" + ` child issue assigned to
  them. Never both for the same work.`

// crewOperatingProtocolFor assembles the protocol, selecting the parent-status
// responsibility that matches this leader's actual authority over the issue.
func crewOperatingProtocolFor(ownsIssueStatus bool) string {
	status := crewParentStatusNotOwned
	if ownsIssueStatus {
		status = crewParentStatusOwned
	}
	return crewOperatingProtocolHeader + "\n" + status + "\n\n" + crewOperatingProtocolHardRules
}

// buildCrewLeaderBriefing composes the full system briefing appended to a
// crew leader's Instructions when it claims a task on a crew-assigned
// issue. The returned string contains three sections:
//
//  1. Crew Operating Protocol (constant, system-level rules).
//  2. Crew Roster (data — leader self-row + members with literal
//     `[@Name](mention://<type>/<UUID>)` strings ready to paste).
//  3. Crew Instructions (user-defined `crew.instructions`, omitted when
//     empty so we don't leave a dangling heading).
//
// ownsIssueStatus must be true only when the issue this task is bound to is
// assigned to this very crew. The briefing is injected on every leader path,
// including ones where the crew is a guest on someone else's issue, so this
// flag is what keeps status authority from leaking along with the roster.
//
// Archived agent members are skipped — there's no point asking the leader
// to delegate to a retired agent. Members whose underlying record can't be
// loaded (deleted user/agent races, FK weirdness) are also skipped silently.
func buildCrewLeaderBriefing(ctx context.Context, q *db.Queries, crew db.Crew, ownsIssueStatus bool) string {
	var sb strings.Builder
	sb.WriteString(crewOperatingProtocolFor(ownsIssueStatus))
	sb.WriteString("\n\n")
	sb.WriteString(buildCrewRoster(ctx, q, crew))

	if trimmed := strings.TrimSpace(crew.Instructions); trimmed != "" {
		sb.WriteString("\n\n## Crew Instructions (")
		sb.WriteString(crew.Name)
		sb.WriteString(")\n\n")
		sb.WriteString(trimmed)
	}
	return sb.String()
}

// buildCrewRoster renders the "## Crew Roster" section: a leader self-row
// plus one row per non-archived member, with literal mention markdown.
func buildCrewRoster(ctx context.Context, q *db.Queries, crew db.Crew) string {
	var sb strings.Builder
	sb.WriteString("## Crew Roster\n\n")

	// Leader self-row. Leaders are always agents (FK enforced in schema).
	leaderName := "Leader"
	if leader, err := q.GetAgent(ctx, crew.LeaderID); err == nil {
		leaderName = leader.Name
	}
	sb.WriteString("Leader (you):\n")
	sb.WriteString("- ")
	sb.WriteString(leaderName)
	sb.WriteString(" — agent — `")
	sb.WriteString(formatMention(leaderName, "agent", util.UUIDToString(crew.LeaderID)))
	sb.WriteString("`\n")

	members, err := q.ListCrewMembers(ctx, crew.ID)
	if err != nil {
		members = nil
	}

	skillNamesByAgentID, skillsLoaded := loadCrewMemberSkillNames(ctx, q, members, util.UUIDToString(crew.LeaderID))

	rows := make([]string, 0, len(members))
	for _, m := range members {
		// Skip the leader if they happen to also be in the member list —
		// they're already shown above and we don't want self-delegation.
		if m.MemberType == "agent" && util.UUIDToString(m.MemberID) == util.UUIDToString(crew.LeaderID) {
			continue
		}
		row := renderMemberRow(ctx, q, m, skillNamesByAgentID, skillsLoaded)
		if row != "" {
			rows = append(rows, row)
		}
	}

	if len(rows) == 0 {
		sb.WriteString("\nMembers: (none — you are the only member of this crew)\n")
		return sb.String()
	}

	sb.WriteString("\nMembers:\n")
	for _, r := range rows {
		sb.WriteString(r)
	}
	return sb.String()
}

func loadCrewMemberSkillNames(ctx context.Context, q *db.Queries, members []db.CrewMember, leaderID string) (map[string][]string, bool) {
	agentIDs := make([]pgtype.UUID, 0)
	seen := make(map[string]struct{}, len(members))
	for _, m := range members {
		if m.MemberType != "agent" {
			continue
		}
		id := util.UUIDToString(m.MemberID)
		if id == leaderID {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		agentIDs = append(agentIDs, m.MemberID)
	}
	if len(agentIDs) == 0 {
		return map[string][]string{}, true
	}
	rows, err := q.ListAgentSkillNamesByAgentIDs(ctx, agentIDs)
	if err != nil {
		return nil, false
	}
	byAgentID := make(map[string][]string, len(agentIDs))
	for _, row := range rows {
		id := util.UUIDToString(row.AgentID)
		byAgentID[id] = append(byAgentID[id], row.Name)
	}
	return byAgentID, true
}

// renderMemberRow renders a single roster row, returning "" if the member
// can't be resolved or should be skipped (e.g. archived agent).
func renderMemberRow(ctx context.Context, q *db.Queries, m db.CrewMember, skillNamesByAgentID map[string][]string, skillsLoaded bool) string {
	id := util.UUIDToString(m.MemberID)
	role := strings.TrimSpace(m.Role)
	switch m.MemberType {
	case "agent":
		ag, err := q.GetAgent(ctx, m.MemberID)
		if err != nil {
			return ""
		}
		if ag.ArchivedAt.Valid {
			return ""
		}
		// Agents carry skills; surfacing them lets the leader delegate by
		// capability instead of guessing from the free-text role label.
		return formatRosterRow(ag.Name, "agent", role, agentSkillsRosterSegment(skillNamesByAgentID, skillsLoaded, id), formatMention(ag.Name, "agent", id))
	case "member":
		user, err := q.GetUser(ctx, m.MemberID)
		if err != nil {
			return ""
		}
		// Mention syntax for humans uses the user_id (matches the rest of
		// the product — see util.MentionRe and frontend mention payloads).
		// Humans have no Multica skills, so no skills segment is rendered.
		userID := util.UUIDToString(m.MemberID)
		return formatRosterRow(user.Name, "member (human)", role, "", formatMention(user.Name, "member", userID))
	default:
		return ""
	}
}

// agentSkillsRosterSegment returns the roster segment describing an agent
// member's assigned skills. "skills: a, b" when the agent has skills (the
// names are pre-sorted by ListAgentSkillNamesByAgentIDs), "no skills assigned"
// when it has none so the leader knows the capability is genuinely absent, and
// "" only when the lookup fails — a transient DB error degrades to the prior
// name+role row rather than asserting a misleading "no skills".
func agentSkillsRosterSegment(skillNamesByAgentID map[string][]string, skillsLoaded bool, agentID string) string {
	if !skillsLoaded {
		return ""
	}
	names := skillNamesByAgentID[agentID]
	if len(names) == 0 {
		return "no skills assigned"
	}
	return "skills: " + strings.Join(names, ", ")
}

func formatRosterRow(name, kind, role, skills, mention string) string {
	var sb strings.Builder
	sb.WriteString("- ")
	sb.WriteString(name)
	sb.WriteString(" — ")
	sb.WriteString(kind)
	if role != "" {
		sb.WriteString(`, role: "`)
		sb.WriteString(role)
		sb.WriteString(`"`)
	}
	if skills != "" {
		sb.WriteString(" — ")
		sb.WriteString(skills)
	}
	sb.WriteString(" — `")
	sb.WriteString(mention)
	sb.WriteString("`\n")
	return sb.String()
}

// formatMention emits a mention markdown string that round-trips through
// util.ParseMentions. The label is the human display name; the link target
// uses the mention:// scheme with the entity type and UUID.
func formatMention(name, mentionType, id string) string {
	return "[@" + name + "](mention://" + mentionType + "/" + id + ")"
}
