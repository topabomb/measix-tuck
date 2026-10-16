CREATE TABLE tasks(id TEXT PRIMARY KEY,created_at_ms INTEGER NOT NULL,status TEXT NOT NULL,archived INTEGER NOT NULL CHECK(archived IN(0,1)),native_session_id TEXT,payload_json TEXT NOT NULL CHECK(json_valid(payload_json) AND json_type(payload_json)='object')) STRICT;
CREATE INDEX task_listing ON tasks(archived,created_at_ms DESC,id);
CREATE TABLE messages(task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE RESTRICT,position INTEGER NOT NULL CHECK(position>=0),revision INTEGER NOT NULL CHECK(revision>=0),payload_json TEXT NOT NULL CHECK(json_valid(payload_json) AND json_type(payload_json)='object'),PRIMARY KEY(task_id,position)) STRICT;
CREATE TABLE events(seq INTEGER PRIMARY KEY AUTOINCREMENT,task_id TEXT REFERENCES tasks(id) ON DELETE RESTRICT,type TEXT NOT NULL,created_at_ms INTEGER NOT NULL,payload_json TEXT NOT NULL CHECK(json_valid(payload_json) AND json_type(payload_json)='object'),
 turn_id TEXT GENERATED ALWAYS AS (json_extract(payload_json,'$.task.turnTiming.turnId')) STORED,
 turn_started_ms REAL GENERATED ALWAYS AS (COALESCE(json_extract(payload_json,'$.task.turnTiming.startedAt'),0)) STORED,
 turn_end_ms REAL GENERATED ALWAYS AS (COALESCE(NULLIF(json_extract(payload_json,'$.task.turnTiming.endedAt'),0),json_extract(payload_json,'$.task.turnTiming.observedAt'),0)) STORED,
 turn_incomplete INTEGER GENERATED ALWAYS AS (COALESCE(json_extract(payload_json,'$.task.turnTiming.incomplete'),0)) STORED) STRICT;
CREATE INDEX event_turns ON events(task_id,turn_id,turn_started_ms,turn_end_ms,turn_incomplete) WHERE type IN ('created','state') AND turn_id IS NOT NULL;
CREATE INDEX task_events ON events(task_id,type,seq);
CREATE TABLE task_deliveries(id TEXT PRIMARY KEY,task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE RESTRICT,version INTEGER NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),turn_id TEXT NOT NULL,created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),summary TEXT NOT NULL CHECK(length(trim(summary)) BETWEEN 1 AND 1000),file_count INTEGER NOT NULL CHECK(file_count BETWEEN 0 AND 10),total_bytes INTEGER NOT NULL CHECK(total_bytes BETWEEN 0 AND 2147483648),files_json TEXT NOT NULL CHECK(json_valid(files_json) AND json_type(files_json)='array' AND json_array_length(files_json)=file_count),UNIQUE(task_id,version)) STRICT;
CREATE INDEX delivery_turn ON task_deliveries(task_id,turn_id);
CREATE TABLE web_sessions(token_hash TEXT PRIMARY KEY,identity TEXT NOT NULL,csrf TEXT NOT NULL,expires_at INTEGER NOT NULL) STRICT;
CREATE TABLE login_limits(key TEXT PRIMARY KEY,failures INTEGER NOT NULL,until_at INTEGER NOT NULL,expires_at INTEGER NOT NULL) STRICT;
CREATE TABLE platform_session(id INTEGER PRIMARY KEY CHECK(id=1),installation_id TEXT NOT NULL,encrypted_payload BLOB NOT NULL) STRICT;
CREATE TABLE mcp_catalog(resource_key TEXT PRIMARY KEY,fingerprint TEXT NOT NULL,catalog_json TEXT NOT NULL CHECK(json_valid(catalog_json)),verified_at_ms INTEGER NOT NULL) STRICT;
PRAGMA application_id=1414873931;
PRAGMA user_version=1;
