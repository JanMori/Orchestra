# Crew Source Map

This file records source evidence for `multica-crews/SKILL.md`.

Use this when the task requires exact source paths, edge-case behavior, tests, or contract verification.

## Object Model

### DB shape

Source:

```text
server/migrations/084_crew.up.sql                # base table: name, description, leader_id, creator_id
server/migrations/085_crew_archive.up.sql        # archived_at, archived_by columns
server/migrations/088_crew_instructions.up.sql   # instructions column
server/pkg/db/queries/crew.sql
packages/core/types/crew.ts
```

Key facts:

- `crew` stores `name`, `description`, `leader_id`, `creator_id` (084), archive
  metadata `archived_at`/`archived_by` (085), and `instructions` (088).
- `crew_member` stores `member_type`, `member_id`, and `role`.
- `member_type` is constrained to `agent` or `member`.
- issue `assignee_type` supports `crew`.

## CLI

Source:

```text
server/cmd/multica/cmd_crew.go
```

Commands:

```bash
orchestra crew list
orchestra crew get <crew-id>
orchestra crew create
orchestra crew update <crew-id>
orchestra crew delete <crew-id>
orchestra crew activity <issue-id> <outcome>

orchestra crew member list <crew-id>
orchestra crew member add <crew-id>
orchestra crew member remove <crew-id>
orchestra crew member set-role <crew-id>
```

Use `--help` for exact flags before writes.

## Create / Update

Source:

```text
server/internal/handler/crew.go                  # CreateCrew ~200-272, UpdateCrew ~287-364
server/pkg/db/queries/agent.sql                   # GetAgentInWorkspace ~15-17
server/pkg/db/generated/agent.sql.go              # getAgentInWorkspace ~1261
```

Contracts:

- create requires `leader_id` (crew.go:215-218);
- leader must be a workspace agent — both create (crew.go:230-237) and update
  (crew.go:333-338) validate via `GetAgentInWorkspace`;
- archived leader is NOT rejected at create/update: `GetAgentInWorkspace` is
  `WHERE id = $1 AND workspace_id = $2` (agent.sql:15-17) with no archived
  filter, so an archived agent can be set as leader here. Archived-leader fails
  closed later, at routing/dispatch — see the readiness gate (crew.go:945,
  isCrewLeaderReady → service.AgentReadiness at crew.go:1017), assignment
  validation (issue.go:2625-2627), and autopilot admission (autopilot.go:885-891);
- leader is auto-added as member with role `leader` (crew.go:258-263);
- updating `leader_id` auto-adds new leader as member if missing (crew.go:340-347).

## Leader Briefing

Source:

```text
server/internal/handler/crew_briefing.go         # buildCrewLeaderBriefing ~104, buildCrewRoster ~121, renderMemberRow ~169, agentSkillsRosterSegment, formatRosterRow
server/internal/handler/daemon.go                  # briefing injection ~1187, ~1530
```

Contracts:

- crew leader tasks append briefing to leader agent instructions
  (daemon.go:1187, 1530);
- briefing includes operating protocol, roster, and optional instructions
  (crew_briefing.go:104-117);
- `buildCrewLeaderBriefing` takes an `ownsIssueStatus` argument selecting
  responsibility 6 via `crewOperatingProtocolFor`: the status grant
  (`crewParentStatusOwned`) only when `issue.assignee_type == "crew"` and
  `issue.assignee_id == crew.id`, otherwise an explicit prohibition
  (`crewParentStatusNotOwned`). Quick-create passes `false` — no issue exists
  yet. Injection is broader than authority on purpose: it is keyed off
  `is_leader_task`, which also fires for `@crew` mentions on issues owned by
  someone else (MUL-3724);
- `instructions` section appears only when non-empty (crew_briefing.go:110-112);
- archived agent members are skipped from roster (crew_briefing.go:178-179);
- agent member roster rows list assigned workspace skills via
  `loadCrewMemberSkillNames` (ListAgentSkillNamesByAgentIDs) and
  `agentSkillsRosterSegment` — "skills: a, b" or
  "no skills assigned"; builtin multica-* skills are excluded and human
  members carry no skills segment (crew_briefing.go renderMemberRow);
- no traced behavior injects `instructions` into every crew member.

## Issue Assignment

Source:

```text
server/internal/handler/issue.go                  # assignee validation ~2614-2632
server/internal/handler/crew.go                   # shouldEnqueueCrewLeaderOnAssign ~990, enqueueCrewLeaderTask ~1027
server/internal/service/task.go
```

Contracts:

- `assignee_type="crew"` routes to `crew.leader_id` (crew.go:1028-1050);
- backlog assignment does not immediately enqueue (crew.go:991-993);
- moving out of backlog can enqueue leader (crew.go:990-994 → isCrewLeaderReady);
- assignee change cancels existing issue tasks first;
- private leader access is checked at assign-time (issue.go:2629-2632) and at
  enqueue-time via `canEnqueueCrewLeader` (crew.go:1037);
- archived crew / archived leader rejected at assign-time (issue.go:2622-2627);
- pending task dedup is applied (crew.go:1042-1048);
- parent status is agent-managed: assignment brief (`writeWorkflowAssignment` with
  `IsCrewLeader`) requires `in_progress` on the first turn and forbids
  unconditional `in_review` on that dispatch turn; Crew Operating Protocol
  (`crew_briefing.go`) owns the ongoing `in_progress` → later `in_review`
  contract. `StartTask` / `CompleteTask` do not write issue status. On
  comment-triggered leader turns `writeWorkflowComment` names that protocol
  responsibility as the one exception to "do not change status unless the
  comment asks" — without it the @mention-dispatch shape (no child issues, so
  no child-done ask) would strand the parent in `in_progress`.

## Comment / Mention

Source:

```text
server/internal/handler/comment.go                # comment triggers ~1057-1199, crew mention branch ~1352
server/internal/handler/crew.go                   # enqueueCrewLeaderTask ~986 (assign/backlog paths), lastTaskWasLeader ~915
server/internal/service/task.go                   # EnqueueTaskForCrewLeader
```

Contracts:

- commenting on a crew-assigned issue can wake the leader — the comment path
  computes triggers via `computeCommentAgentTriggers` (comment.go:1124), whose
  assigned-crew branch is `computeAssignedCrewLeaderCommentTrigger`
  (comment.go:1162-1199); the same computation backs the trigger-preview
  endpoint;
- explicit `mention://crew/<id>` resolves crew and adds the leader trigger
  (comment.go:1352-1391);
- crew mention does not fan out to members — enqueue targets `crew.LeaderID`
  only (comment.go:1104-1112, and crew.go:1007 on the assign/backlog paths);
- leader task uses `is_leader_task=true` (via `EnqueueTaskForCrewLeader`);
- leader self-trigger loops are guarded — same-leader / last-task-was-leader
  guards (comment.go:1173-1176, lastTaskWasLeader at crew.go:915) and member
  explicit-mention skip (comment.go:1177-1179).

## Autopilot

Source:

```text
server/internal/service/autopilot.go              # resolveAutopilotLeader ~617-655, dispatch ~88-111
server/internal/handler/autopilot.go              # save-time validateAutopilotAssignee ~845-893
```

Contracts:

- crew autopilot resolves executable agent from `crew.leader_id` —
  `resolveAutopilotLeader` crew branch (autopilot.go:639-651);
- readiness/admission checks target the leader: save-time validation rejects an
  archived crew/leader (handler/autopilot.go:881-891), and dispatch re-runs
  `resolveAutopilotLeader` + `AgentReadiness`;
- archived crew fails closed / skips dispatch — `errCrewArchived`
  (autopilot.go:644-645);
- `create_issue` keeps the issue assigned to the crew (autopilot.go:88-97);
- `run_only` creates task directly for leader (autopilot.go:99-106, dispatch via
  `resolveAutopilotLeader` at autopilot.go:284).

## Child-done Parent Trigger

Source:

```text
server/internal/handler/issue_child_done.go       # dispatchParentAssigneeTrigger ~246, triggerChildDoneCrew ~304
```

Contracts:

- when a child issue closes a stage barrier and the parent is assigned to a
  crew, the parent crew leader is triggered (triggerChildDoneCrew in
  issue_child_done.go);
- routing is leader-only — one `EnqueueTaskForCrewLeader` on the leader, no
  member fan-out (triggerChildDoneCrew / dispatchParentAssigneeTrigger);
- no self-trigger guard: a same-crew or shared-leader child still wakes the
  parent crew leader — the wake is a serial handoff onto the PARENT and is the
  only carrier of the stage-barrier "advance / wrap up" instruction (MUL-3969,
  mirrors the agent path from MUL-2808). Re-triggering is bounded only by
  `HasPendingTaskForIssueAndAgent` (idempotent per parent issue + agent).
- no leader-invocation gate: child-done does NOT re-check whether the child's
  completer can invoke the leader. The parent was already permission-checked at
  crew-assign time (`validateAssigneePair`), so waking its own leader is a
  coordination handoff, not a fresh invocation. Re-checking it here failed
  closed for the DEFAULT private leader (the child's completer is an
  agent/system actor with no resolvable human originator), stranding every
  process-crew pipeline after stage 1 while direct-to-leader-agent parents
  advanced fine (MUL-4063 / GH #4928). Agent and crew child-done now share one
  ungated path; any future invocation gate must be added to BOTH together.
- parent status is not auto-advanced by the barrier: the system comment asks the
  leader to continue or — when the overall goal is met — run
  `orchestra issue status <parent-id> in_review`. That explicit ask is what lets a
  comment-triggered leader turn change status (the comment workflow otherwise
  forbids status flips unless asked). `done` remains human / integration owned.

## Private Leader Access

Source:

```text
server/internal/handler/agent_access.go           # canInvokeAgent ~48-108, canEnqueueCrewLeader ~261-267
server/internal/handler/crew.go                   # enqueueCrewLeaderTask gate ~955-974
```

Contracts (invocation gate, MUL-3963 — this is the *trigger* gate, distinct from
the view gate `canAccessPrivateAgent`):

- `canEnqueueCrewLeader` loads the leader and delegates to `canInvokeAgent`
  (agent_access.go:261-267);
- `canInvokeAgent` judges by the *effective invoking user*: a member actor is
  itself; an agent/system actor is the top-of-chain human originator
  (`originatorUserID`), which is `""` when none resolved (agent_access.go:48-54);
- the agent owner may always invoke their own agent (agent_access.go:57-59);
- `permission_mode != "public_to"` (i.e. private) is deny-by-default — no admin
  bypass, no A2A bypass; only the owner branch passes (agent_access.go:61-65);
- `public_to` consults the invocation-target allow-list: a `workspace` target
  admits any workspace member AND workspace-internal agent/system principals even
  with no resolved human (`workspaceBroad`); `member` targets require the
  resolved human to match; `team` targets are inert in V1 (agent_access.go:82-106);
- wired into `enqueueCrewLeaderTask` (crew.go:955-974): the crew
  assign/promote path denies the enqueue when the actor cannot invoke the leader
  (member authors are their own originator; agent-authored triggers pass `""`).
- NOTE: the child-done wake does NOT use this gate anymore — see "Child-done
  Parent Trigger" above (MUL-4063).

## Tests

Relevant test groups:

```text
server/internal/handler/crew_assign_trigger_test.go
server/internal/handler/crew_comment_trigger_test.go
server/internal/handler/crew_briefing_test.go
server/internal/handler/crew_private_leader_test.go
server/internal/handler/autopilot_private_leader_test.go
server/internal/handler/crew_no_action_test.go
```

Verification command:

```bash
go test ./internal/handler -run 'Test.*Crew|Test.*crew|Test.*Autopilot.*Crew|Test.*ChildDone.*Crew'
```
