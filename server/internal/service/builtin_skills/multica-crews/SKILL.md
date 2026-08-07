---
name: multica-crews
description: "Use when creating, inspecting, updating, assigning to, or debugging a Multica crew, including how leader routing picks who runs."
user-invocable: false
allowed-tools: Bash(multica *)
---

# Multica Crews

## Quick start

If debugging why a crew did or did not run, inspect first:

```bash
orchestra issue get <issue-id> --output json
orchestra crew get <crew-id> --output json
orchestra crew member list <crew-id> --output json
orchestra issue comment list <issue-id> --roots-only --summary --output json
orchestra issue comment list <issue-id> --thread <thread-id> --tail 30 --output json
```

The two comment reads are a sequence: scan the roots first, then open the threads that look relevant — mention triggers, failure reasons, and user instructions usually live in the replies, which the roots scan never returns.

If the command shape is unclear, check help instead of guessing:

```bash
orchestra crew --help
orchestra crew member --help
orchestra issue update --help
orchestra issue comment add --help
```

Do not assign, comment, mention, update, delete, or record crew activity just
to test. These can mutate workspace state or trigger agent runs.

## Core model

A Multica crew is a workspace routing and coordination object.

A crew is not an agent. It does not run work by itself. Current behavior:
crew-routed work runs through the crew's `leader_id` agent.

Important consequences:

- assigning an issue to a crew routes to the leader;
- mentioning a crew routes to the leader;
- crew-assigned autopilot resolves to the leader;
- crew members are not automatically fanned out;
- crew `instructions` are leader briefing content, not member prompts.

## CLI

Crew commands:

```bash
orchestra crew list --output json
orchestra crew get <crew-id> --output json
orchestra crew create --name <name> --leader <agent-name-or-id> --output json
orchestra crew update <crew-id> --instructions "<leader coordination policy>" --output json
orchestra crew delete <crew-id>
```

Member commands:

```bash
orchestra crew member list <crew-id> --output json
orchestra crew member add <crew-id> --member-id <id> --type agent|member --role <role> --output json
orchestra crew member remove <crew-id> --member-id <id> --type agent|member
orchestra crew member set-role <crew-id> --member-id <id> --member-type agent|member --role <role> --output json
```

Crew leader evaluation command:

```bash
orchestra crew activity <issue-id> action|no_action|failed --reason "<why>" --output json
```

`activity` is a write: it records the leader's evaluation decision on an issue.
Use it only when acting as the crew leader after evaluating a trigger.

Issue/comment commands often needed with crews:

```bash
orchestra issue get <issue-id> --output json
orchestra issue update <issue-id> --help
orchestra issue comment list <issue-id> --roots-only --summary --output json
orchestra issue comment add <issue-id> --help
```

Comment reads stay bounded — the scan-then-expand sequence from the quick
start above — never one unbounded `issue comment list` pull.

Prefer `--output json` for reads. Use `--help` before writes.

## Crew fields

- `id` — crew UUID.
- `workspace_id` — workspace the crew belongs to.
- `name` — display name; unique per workspace.
- `description` — human-facing metadata/display text. Do not assume runtime
  prompt impact unless source proves a consumer.
- `instructions` — crew-level instructions added to the crew leader briefing.
  They are not directly injected into every crew member.
- `avatar_url` — optional crew avatar URL.
- `leader_id` — agent ID of the crew leader; the runtime target for
  crew-routed work.
- `creator_id` — creator of the crew.
- `archived_at` / `archived_by` — archive metadata. Archived crews are rejected
  by assignment/autopilot routing paths.
- `member_count` — list response count of crew members.
- `member_preview` — list response preview of crew members.

Use `instructions` for leader-facing coordination policy: crew responsibility,
delegation expectations, when to ask humans, and review/handoff rules. Do not
write it as if every member automatically receives it.

## Crew member fields

- `member_type` — `agent` or `member`.
- `member_id` — ID of the agent or workspace member.
- `role` — roster role label. Current behavior: non-empty `role` appears in the
  leader briefing roster. Do not assume it creates scheduling, permissions, or
  routing behavior.

## Creation and leader membership

Creating a crew requires `leader_id`. The leader must be a workspace agent.
Create/update does not reject an archived leader: the lookup only checks the
agent exists in the workspace. An archived leader fails closed later, at
routing/dispatch — assignment, autopilot admission, and the comment/mention
readiness gate all reject an archived leader before any task is enqueued.

On create, the backend attempts to add the leader as a crew member with role
`leader`. When updating `leader_id`, if the new leader is not already a member,
the backend adds the new leader as a crew member with role `leader`.

## Leader briefing

For crew leader tasks, Multica appends a crew leader briefing to the leader
agent instructions. The briefing includes:

- Crew Operating Protocol;
- Crew Roster;
- Crew Instructions, only when `instructions` is non-empty.

Roster entries include member name, member type, mention markdown, and non-empty
role. For agent members the roster also lists their assigned skills
(`skills: a, b`, or `no skills assigned` when the agent has none) so the leader
can delegate by capability instead of guessing from the role label; human
members carry no skills segment. Builtin `multica-*` skills are not listed —
only the workspace skills explicitly attached to the agent. Archived agent
members are skipped from the briefing roster.

## Issue assignment behavior

Issues can be assigned to crews with:

```text
assignee_type = "crew"
assignee_id = <crew-id>
```

Current behavior:

- assignment routes work to `crew.leader_id`;
- it does not enqueue every crew member;
- assignment while status is `backlog` does not immediately start work;
- moving a crew-assigned issue out of `backlog` can trigger the leader;
- changing assignee cancels existing tasks for the issue before enqueueing the
  new assignee path;
- parent issue status is agent-managed (same model as direct agent assignment):
  the leader's first assignment turn should move the parent to `in_progress`
  and keep it there while members work; the leader moves the parent to
  `in_review` only when a later re-trigger confirms the overall goal is met.
  Completing a leader `task` (including the first dispatch) does not itself
  change issue status;
- that status authority is granted only when the issue's `assignee_type` /
  `assignee_id` point at THIS crew. The leader briefing is injected on every
  leader path, including an `@crew` mention on an issue owned by a plain agent
  — on those paths the protocol instead carries an explicit "do not change this
  issue's status".

Assignment validation rejects a missing type/id pair, non-existent crew,
archived crew, archived leader, and private leader when the actor cannot access
it.

## Comment and mention behavior

If an issue is assigned to a crew, a new comment can wake the crew leader. This
is leader routing, not member fan-out.

Crew mention format:

```md
[@Crew Name](mention://crew/<crew-id>)
```

Current behavior: resolve the crew, read `leader_id`, enqueue a leader task,
and use the current comment as the trigger comment. It does not enqueue every
crew member.

## Autopilot behavior

Autopilots can be assigned to crews. For `assignee_type = "crew"`:

- executable agent resolves from `crew.leader_id`;
- admission/readiness checks run against the leader;
- archived crews fail closed / skip dispatch;
- run attribution records crew id where applicable.

For `create_issue` autopilots, the created issue keeps `assignee_type = "crew"`
and `assignee_id = <crew-id>`, while the actual executing agent is the resolved
leader. For `run_only` autopilots, no issue is created; the task is created
directly for the resolved leader agent.

## Handling complaints or product gaps

When the user says crew behavior is wrong, confusing, or disappointing, do not
immediately assume code is broken and do not defend current behavior just because
it exists. Classify first:

- expected current behavior;
- configuration issue;
- product limitation;
- actual bug.

Explain the current source-backed behavior. If the behavior is technically
correct but product-wise bad, say so and propose a scoped product/code change.

Do not silently change crew routing, member fan-out, leader briefing, autopilot
behavior, or comment-trigger behavior without confirmation. These are product
contract changes with side effects.

## Side effects

These actions can trigger agent work or mutate durable state:

- creating a crew;
- updating crew fields;
- changing `leader_id`;
- adding/removing members;
- changing member roles;
- assigning an issue to a crew;
- moving a crew-assigned issue out of backlog;
- commenting on a crew-assigned issue;
- mentioning a crew;
- creating or triggering crew-assigned autopilots;
- recording crew activity with `orchestra crew activity`;
- deleting/archive crew.

Do not perform side-effecting actions as tests unless the user explicitly
authorizes them.

## Common wrong assumptions

- A crew is not an agent.
- Crew work routes to `leader_id`, not every member.
- Crew mention routes to the leader, not every member.
- Crew assignment routes to the leader, not every member.
- Crew autopilot resolves to the leader as executable agent.
- `instructions` are leader briefing content, not automatic member prompts.
- `description` is not proven runtime prompt content.
- `role` is roster context, not automatic scheduling.
- Backlog assignment does not immediately start work.
- First leader dispatch is not parent completion — parent stays `in_progress`
  until the leader later confirms the overall goal and moves it to `in_review`.
- The server does not auto-flip parent status when child issues finish; it only
  wakes the leader with an explicit ask (including `in_review` when wrapping up).
- Getting the leader briefing does NOT imply status authority. A crew
  `@`-mentioned into an issue assigned to someone else is a guest: roster and
  delegation rules yes, `orchestra issue status` no.

## References

For source paths, tests, edge cases, and exact routing details, see:

```text
references/crew-source-map.md
```
