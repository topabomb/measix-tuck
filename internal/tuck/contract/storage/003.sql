-- Business facts are extracted by the offline migration before this step.
-- Replay is disposable, but its high-water mark must survive an empty cache.
CREATE TEMP TABLE event_high_water AS
 SELECT COALESCE((SELECT seq FROM sqlite_sequence WHERE name='events'),0) AS seq;
DROP TABLE events;
CREATE TABLE events (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 task_id TEXT REFERENCES tasks(id) ON DELETE RESTRICT,
 type TEXT NOT NULL,
 created_at_ms INTEGER NOT NULL,
 payload_json TEXT NOT NULL CHECK(json_valid(payload_json) AND json_type(payload_json)='object'),
 payload_bytes INTEGER GENERATED ALWAYS AS (length(CAST(payload_json AS BLOB))) STORED
) STRICT;
CREATE INDEX event_retention ON events(seq,payload_bytes,created_at_ms);
INSERT INTO sqlite_sequence(name,seq) SELECT 'events',seq FROM event_high_water;
DROP TABLE event_high_water;
PRAGMA user_version=3;
