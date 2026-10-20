-- Lifecycle facts belong to the existing task JSON. Generated columns keep
-- lookup indexes consistent without a second writable copy of these facts.
CREATE TABLE managed_workspaces (
 id TEXT PRIMARY KEY,
 root TEXT NOT NULL,
 path TEXT NOT NULL UNIQUE,
 identity TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL CHECK(state IN ('allocating','attached','pending')),
 cleanup_allowed INTEGER NOT NULL CHECK(cleanup_allowed IN (0,1)),
 last_error TEXT NOT NULL DEFAULT ''
) STRICT;
ALTER TABLE tasks ADD COLUMN managed_workspace_id TEXT
 GENERATED ALWAYS AS (json_extract(payload_json,'$.managedWorkspaceId')) VIRTUAL
 REFERENCES managed_workspaces(id) ON DELETE RESTRICT;
ALTER TABLE tasks ADD COLUMN parent_task_id TEXT
 GENERATED ALWAYS AS (json_extract(payload_json,'$.parentTaskId')) VIRTUAL;
CREATE INDEX task_workspace ON tasks(managed_workspace_id);
CREATE INDEX task_parent ON tasks(parent_task_id);
PRAGMA user_version=4;
