-- Autopilot: support assigning to a crew (ISS-2429).
--
-- Path A "Crew-as-Leader": when an autopilot's assignee is a crew, dispatch
-- still resolves to a single agent (crew.leader_id) — same semantics as a
-- human manually assigning an issue to that crew. We model this by adding an
-- assignee_type column and dropping the hard FK on assignee_id so the same
-- UUID column can reference either agent(id) or crew(id) depending on the
-- type. Referential integrity is enforced in the application layer (handler
-- validates the crew/agent is in the workspace; dispatch re-resolves at run
-- time and skip-records doomed runs instead of crashing).

ALTER TABLE autopilot
    DROP CONSTRAINT IF EXISTS autopilot_assignee_id_fkey;

ALTER TABLE autopilot
    ADD COLUMN IF NOT EXISTS assignee_type TEXT NOT NULL DEFAULT 'agent'
        CHECK (assignee_type IN ('agent', 'crew'));

-- Composite index lets lookups discriminate by type cheaply, e.g. "all
-- autopilots whose assignee is crew X" without scanning the whole table.
-- The legacy idx_autopilot_assignee(assignee_id) stays for plain id lookups.
CREATE INDEX IF NOT EXISTS idx_autopilot_assignee_type_id
    ON autopilot (assignee_type, assignee_id);

-- autopilot_run.crew_id: attribution hook. Populated when ap.assignee_type =
-- 'crew' so reports can group runs by crew even though the executing agent
-- (and the cost it accrues) is the leader. First version does not consume
-- the column; it exists so we never need a backfill.
ALTER TABLE autopilot_run
    ADD COLUMN IF NOT EXISTS crew_id UUID;

CREATE INDEX IF NOT EXISTS idx_autopilot_run_crew_id
    ON autopilot_run (crew_id) WHERE crew_id IS NOT NULL;
