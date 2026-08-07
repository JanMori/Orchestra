-- Crew names need not be unique within a workspace — different teams may
-- legitimately want the same label (e.g. "Research"), and the leader/UUID
-- already disambiguates them.
ALTER TABLE crew DROP CONSTRAINT IF EXISTS crew_workspace_id_name_key;
