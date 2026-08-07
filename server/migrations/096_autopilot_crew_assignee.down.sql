-- Reverts 096_autopilot_crew_assignee.up.sql.
-- Restoring the agent FK requires every assignee_id to reference a real
-- agent. Crew-assigned autopilots would dangle, so they are deleted here.
-- Operators should drain crew-assigned autopilots before rolling back if
-- they want to preserve the rows.

DROP INDEX IF EXISTS idx_autopilot_run_crew_id;

ALTER TABLE autopilot_run
    DROP COLUMN IF EXISTS crew_id;

DROP INDEX IF EXISTS idx_autopilot_assignee_type_id;

DELETE FROM autopilot WHERE assignee_type = 'crew';

ALTER TABLE autopilot
    DROP COLUMN IF EXISTS assignee_type;

ALTER TABLE autopilot
    ADD CONSTRAINT autopilot_assignee_id_fkey
        FOREIGN KEY (assignee_id) REFERENCES agent(id) ON DELETE CASCADE;
