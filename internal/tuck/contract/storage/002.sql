ALTER TABLE platform_session ADD COLUMN format_version INTEGER NOT NULL DEFAULT 2 CHECK(format_version=2);
CREATE TABLE mcp_catalog_v2(resource_key TEXT PRIMARY KEY,fingerprint TEXT NOT NULL,catalog_json TEXT NOT NULL CHECK(json_valid(catalog_json) AND json_type(catalog_json)='array' AND length(CAST(catalog_json AS BLOB))<=4194304),verified_at_ms INTEGER NOT NULL) STRICT;
INSERT INTO mcp_catalog_v2 SELECT * FROM mcp_catalog WHERE json_type(catalog_json)='array' AND length(CAST(catalog_json AS BLOB))<=4194304;
DROP TABLE mcp_catalog;
ALTER TABLE mcp_catalog_v2 RENAME TO mcp_catalog;
PRAGMA user_version=2;
