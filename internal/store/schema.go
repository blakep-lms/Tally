package store

// schema is the full set of migrations, applied in order. Each entry is run
// once and recorded in schema_migrations; adding a new migration is additive.
var migrations = []string{
	// 0001 — core tables.
	`
CREATE TABLE projects (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	name        TEXT NOT NULL UNIQUE,
	type        TEXT NOT NULL CHECK (type IN ('billable','internal')),
	client      TEXT NOT NULL DEFAULT '',
	status      TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','done')),
	created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	done_at     TIMESTAMP
);

CREATE TABLE rules (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
	field       TEXT NOT NULL CHECK (field IN ('app','title','url','repo')),
	match       TEXT NOT NULL CHECK (match IN ('contains','equals','regex')),
	pattern     TEXT NOT NULL,
	priority    INTEGER NOT NULL DEFAULT 100,
	active      INTEGER NOT NULL DEFAULT 1,
	created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_rules_active ON rules(active, priority);

CREATE TABLE events (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	start       TIMESTAMP NOT NULL,
	duration    REAL NOT NULL,
	app         TEXT NOT NULL DEFAULT '',
	title       TEXT NOT NULL DEFAULT '',
	url         TEXT NOT NULL DEFAULT '',
	repo        TEXT NOT NULL DEFAULT '',
	project_id  INTEGER REFERENCES projects(id) ON DELETE SET NULL,
	rule_id     INTEGER REFERENCES rules(id) ON DELETE SET NULL,
	source      TEXT NOT NULL DEFAULT '',
	source_key  TEXT NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX idx_events_source_key ON events(source_key) WHERE source_key <> '';
CREATE INDEX idx_events_start ON events(start);
CREATE INDEX idx_events_project ON events(project_id);

-- Cache for LLM classification decisions, keyed by a signature of the event
-- signals so repeated ambiguous titles are not re-sent to the API.
CREATE TABLE llm_cache (
	signature   TEXT PRIMARY KEY,
	project_id  INTEGER REFERENCES projects(id) ON DELETE CASCADE,
	created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`,
}
