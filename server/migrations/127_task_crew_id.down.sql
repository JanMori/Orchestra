DROP INDEX IF EXISTS agent_task_queue_crew_id_idx;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS crew_id;
