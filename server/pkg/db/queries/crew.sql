-- name: CreateCrew :one
INSERT INTO crew (workspace_id, name, description, leader_id, creator_id, avatar_url)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetCrew :one
SELECT * FROM crew WHERE id = $1;

-- name: GetCrewInWorkspace :one
SELECT * FROM crew WHERE id = $1 AND workspace_id = $2;

-- name: LockCrewForAutopilotAssignment :one
-- Stabilizes the crew-to-leader resolution while an active Autopilot is
-- created, retargeted, or resumed. FOR SHARE conflicts with an ordinary
-- leader_id update, so the caller subsequently locks the same leader Agent
-- whose row Runtime teardown serializes against.
SELECT * FROM crew
WHERE id = $1 AND workspace_id = $2
FOR SHARE;

-- name: LockCrewForUpdate :one
-- Crew leader changes take the exclusive side of the same lock used by
-- Autopilot assignment. The handler then locks the proposed leader Agent and
-- pauses active crew Autopilots when that Agent is unbound.
SELECT * FROM crew
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: ListCrews :many
SELECT * FROM crew WHERE workspace_id = $1 AND archived_at IS NULL ORDER BY created_at ASC;

-- name: ListCrewMemberPreviewRows :many
-- Static crew membership summary for list/hover previews. This deliberately
-- excludes derived runtime/task status; the crew detail members-status
-- endpoint owns live state.
SELECT
    sm.crew_id,
    sm.member_type,
    sm.member_id,
    sm.role
FROM crew_member sm
JOIN crew s ON s.id = sm.crew_id
WHERE s.workspace_id = $1 AND s.archived_at IS NULL
ORDER BY
    sm.crew_id ASC,
    (sm.member_type = 'agent' AND sm.member_id = s.leader_id) DESC,
    sm.created_at ASC;

-- name: ListCrewMemberPreviewRowsByCrew :many
SELECT
    sm.crew_id,
    sm.member_type,
    sm.member_id,
    sm.role
FROM crew_member sm
JOIN crew s ON s.id = sm.crew_id
WHERE sm.crew_id = $1
ORDER BY
    (sm.member_type = 'agent' AND sm.member_id = s.leader_id) DESC,
    sm.created_at ASC;

-- name: ListAllCrews :many
SELECT * FROM crew WHERE workspace_id = $1 ORDER BY created_at ASC;

-- name: UpdateCrew :one
UPDATE crew SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    leader_id = COALESCE(sqlc.narg('leader_id'), leader_id),
    avatar_url = COALESCE(sqlc.narg('avatar_url'), avatar_url),
    instructions = COALESCE(sqlc.narg('instructions'), instructions),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ArchiveCrew :one
UPDATE crew SET archived_at = now(), archived_by = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: AddCrewMember :one
INSERT INTO crew_member (crew_id, member_type, member_id, role)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: RemoveCrewMember :execrows
DELETE FROM crew_member
WHERE crew_id = $1 AND member_type = $2 AND member_id = $3;

-- name: ListCrewMembers :many
SELECT * FROM crew_member WHERE crew_id = $1 ORDER BY created_at ASC;

-- name: UpdateCrewMemberRole :one
UPDATE crew_member SET role = $4
WHERE crew_id = $1 AND member_type = $2 AND member_id = $3
RETURNING *;

-- name: IsCrewMember :one
SELECT EXISTS(
    SELECT 1 FROM crew_member
    WHERE crew_id = $1 AND member_type = $2 AND member_id = $3
) AS is_member;

-- name: CountCrewMembers :one
SELECT count(*) FROM crew_member WHERE crew_id = $1;

-- name: GetCrewByAssignee :one
-- Look up the crew when an issue is assigned to a crew.
SELECT s.* FROM crew s WHERE s.id = $1 AND s.workspace_id = $2;

-- name: ListCrewsByMember :many
-- Find all crews a given entity belongs to in a workspace.
SELECT s.* FROM crew s
JOIN crew_member sm ON sm.crew_id = s.id
WHERE s.workspace_id = $1 AND sm.member_type = $2 AND sm.member_id = $3
ORDER BY s.created_at ASC;

-- name: TransferCrewAssignees :exec
-- Transfer all issues assigned to a crew to the crew's leader agent.
UPDATE issue SET assignee_type = 'agent', assignee_id = $2, updated_at = now()
WHERE assignee_type = 'crew' AND assignee_id = $1;

-- name: TransferCrewAutopilotsToLeader :exec
-- Mirrors TransferCrewAssignees for autopilot rows: when a crew is archived,
-- any autopilot still pointing at the crew would otherwise dangle and the
-- admission gate would skip every subsequent dispatch with "assignee crew
-- cannot be resolved". Rewrite the assignee in place to the leader agent so
-- the autopilot keeps firing under the same leader-only execution semantics
-- it had a moment before the archive (Path A from MUL-2429).
UPDATE autopilot
SET assignee_type = 'agent',
    assignee_id = $2,
    updated_at = now()
WHERE assignee_type = 'crew' AND assignee_id = $1;

-- name: ListCrewMemberStatusRows :many
-- Per-row join used to build the crew-members status view. One row per
-- (crew_member × active_task); members with no active task return a
-- single row with NULL task_* columns. Human members and agent members
-- with no agent row also return one row with NULL agent_/runtime_ columns.
-- The handler aggregates rows by member_id.
SELECT
    sm.id              AS crew_member_id,
    sm.member_type     AS member_type,
    sm.member_id       AS member_id,
    a.archived_at      AS agent_archived_at,
    ar.status          AS runtime_status,
    ar.last_seen_at    AS runtime_last_seen_at,
    atq.id             AS task_id,
    atq.status         AS task_status,
    atq.issue_id       AS task_issue_id,
    atq.dispatched_at  AS task_dispatched_at,
    i.number           AS issue_number,
    i.title            AS issue_title,
    i.status           AS issue_status
FROM crew_member sm
LEFT JOIN agent a
       ON sm.member_type = 'agent' AND a.id = sm.member_id
LEFT JOIN agent_runtime ar
       ON ar.id = a.runtime_id
LEFT JOIN agent_task_queue atq
       ON sm.member_type = 'agent'
      AND atq.agent_id = sm.member_id
      AND atq.status IN ('dispatched', 'running', 'waiting_local_directory')
LEFT JOIN issue i
       ON i.id = atq.issue_id
WHERE sm.crew_id = $1
ORDER BY sm.created_at ASC, atq.dispatched_at DESC NULLS LAST;
