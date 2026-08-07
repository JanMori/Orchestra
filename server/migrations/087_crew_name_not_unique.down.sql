-- Restore unique(workspace_id, name) on crew.
ALTER TABLE crew ADD CONSTRAINT crew_workspace_id_name_key UNIQUE (workspace_id, name);
