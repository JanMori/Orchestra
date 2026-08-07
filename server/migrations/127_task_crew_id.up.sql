-- agent_task_queue.crew_id records the crew a leader-task belongs to. The
-- daemon uses it at claim time to locate the crew whose briefing (Operating
-- Protocol + Roster + Instructions) should be injected onto the leader agent's
-- instructions, instead of inferring the crew by reverse-looking-up
-- "which crew is this agent the leader of" (which is ambiguous when one
-- agent leads multiple crews).
--
-- No FK to crew(id) on purpose: agent_task_queue is a hot, high-write task
-- queue, and we don't want crew maintenance (archive / hard-delete) to take
-- cross-table locks against it. If a crew is hard-deleted and a stale UUID
-- lingers here, the daemon's GetCrewInWorkspace lookup simply returns no row
-- and the claim path skips injection (err != nil branch) — exactly the same
-- observable behavior as "injection condition not matched". No stale briefing
-- is ever emitted.
ALTER TABLE agent_task_queue
    ADD COLUMN crew_id UUID NULL;

-- Partial index over leader-task rows only: small, high hit-rate. It serves
-- admin / debug queries ("which leader tasks is this crew currently
-- running"). The daemon claim path does NOT use it — that goes through the
-- task_id primary-key path.
CREATE INDEX agent_task_queue_crew_id_idx
    ON agent_task_queue (crew_id)
    WHERE crew_id IS NOT NULL;
