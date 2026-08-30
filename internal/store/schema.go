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

CREATE TABLE llm_cache (
	signature   TEXT PRIMARY KEY,
	project_id  INTEGER REFERENCES projects(id) ON DELETE CASCADE,
	created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`,
	// 0002 — canonical work items. Projects are preserved as legacy_projects;
	// billing intent is moved to billing_profiles. Rules, events, and LLM cache
	// are rebuilt with canonical work_item_id foreign keys without touching event
	// durations/source/classification ids.
	`
CREATE TABLE work_items (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	name        TEXT NOT NULL UNIQUE,
	kind        TEXT NOT NULL CHECK (kind IN ('project','product','goal','other')),
	context     TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	status      TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','done')),
	created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	done_at     TIMESTAMP
);

INSERT INTO work_items (id, name, kind, context, description, status, created_at, done_at)
SELECT id, name, 'project', client, '', status, created_at, done_at FROM projects;

CREATE TABLE billing_profiles (
	work_item_id INTEGER PRIMARY KEY REFERENCES work_items(id) ON DELETE CASCADE,
	legacy_type  TEXT NOT NULL CHECK (legacy_type IN ('billable','internal')),
	created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO billing_profiles (work_item_id, legacy_type)
SELECT id, type FROM projects;

ALTER TABLE projects RENAME TO legacy_projects;

CREATE TABLE rules_new (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	work_item_id INTEGER NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
	field        TEXT NOT NULL CHECK (field IN ('app','title','url','repo')),
	match        TEXT NOT NULL CHECK (match IN ('contains','equals','regex')),
	pattern      TEXT NOT NULL,
	priority     INTEGER NOT NULL DEFAULT 100,
	active       INTEGER NOT NULL DEFAULT 1,
	created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO rules_new (id, work_item_id, field, match, pattern, priority, active, created_at)
SELECT id, project_id, field, match, pattern, priority, active, created_at FROM rules;

CREATE TABLE events_new (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	start        TIMESTAMP NOT NULL,
	duration     REAL NOT NULL,
	app          TEXT NOT NULL DEFAULT '',
	title        TEXT NOT NULL DEFAULT '',
	url          TEXT NOT NULL DEFAULT '',
	repo         TEXT NOT NULL DEFAULT '',
	work_item_id INTEGER REFERENCES work_items(id) ON DELETE SET NULL,
	rule_id      INTEGER REFERENCES rules_new(id) ON DELETE SET NULL,
	source       TEXT NOT NULL DEFAULT '',
	source_key   TEXT NOT NULL DEFAULT ''
);
INSERT INTO events_new (id, start, duration, app, title, url, repo, work_item_id, rule_id, source, source_key)
SELECT id, start, duration, app, title, url, repo, project_id, rule_id, source, source_key FROM events;

-- Drop children before parents. events_new points at rules_new during the
-- rebuild, so dropping the legacy pair cannot null historical rule ids.
DROP TABLE events;
DROP TABLE rules;
ALTER TABLE rules_new RENAME TO rules;
ALTER TABLE events_new RENAME TO events;
CREATE INDEX idx_rules_active ON rules(active, priority);
CREATE INDEX idx_rules_work_item ON rules(work_item_id);
CREATE UNIQUE INDEX idx_events_source_key ON events(source_key) WHERE source_key <> '';
CREATE INDEX idx_events_start ON events(start);
CREATE INDEX idx_events_work_item ON events(work_item_id);

CREATE TABLE llm_cache_new (
	signature    TEXT PRIMARY KEY,
	work_item_id INTEGER REFERENCES work_items(id) ON DELETE CASCADE,
	created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO llm_cache_new (signature, work_item_id, created_at)
SELECT signature, project_id, created_at FROM llm_cache;
DROP TABLE llm_cache;
ALTER TABLE llm_cache_new RENAME TO llm_cache;
`,
	// 0003 — audit classification changes for correction traceability.
	`
CREATE TABLE classification_audit (
	id               INTEGER PRIMARY KEY AUTOINCREMENT,
	event_id         INTEGER NOT NULL REFERENCES events(id) ON DELETE CASCADE,
	old_work_item_id INTEGER REFERENCES work_items(id) ON DELETE SET NULL,
	new_work_item_id INTEGER REFERENCES work_items(id) ON DELETE SET NULL,
	old_rule_id      INTEGER REFERENCES rules(id) ON DELETE SET NULL,
	new_rule_id      INTEGER REFERENCES rules(id) ON DELETE SET NULL,
	old_source       TEXT NOT NULL DEFAULT '',
	new_source       TEXT NOT NULL DEFAULT '',
	created_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_classification_audit_event ON classification_audit(event_id, created_at);
`,
	// 0004 — capture groups allow complete replacement of AFK-derived fragments.
	`
ALTER TABLE events ADD COLUMN source_group TEXT NOT NULL DEFAULT '';
UPDATE events SET source_group = source_key WHERE source_key LIKE 'aw:%';
CREATE INDEX idx_events_source_group ON events(source_group);
`,
	// 0005 — full optional billing profiles. Existing legacy billing intent is
	// preserved as project-scoped profiles with default exact/no-rate policy.
	`
ALTER TABLE billing_profiles RENAME TO billing_profiles_legacy;
CREATE TABLE billing_profiles (
	id                         INTEGER PRIMARY KEY AUTOINCREMENT,
	work_item_id              INTEGER,
	scope_type                 TEXT NOT NULL CHECK (scope_type IN ('global','client','work_item')),
	scope_key                  TEXT NOT NULL DEFAULT '',
	enabled                    INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0,1)),
	currency                   TEXT NOT NULL DEFAULT 'USD',
	hourly_rate_minor          INTEGER NOT NULL DEFAULT 0 CHECK (hourly_rate_minor >= 0),
	rounding_mode              TEXT NOT NULL DEFAULT 'up' CHECK (rounding_mode = 'up'),
	rounding_increment_minutes INTEGER NOT NULL DEFAULT 15 CHECK (rounding_increment_minutes > 0),
	rounding_scope             TEXT NOT NULL DEFAULT 'period_work_item' CHECK (rounding_scope = 'period_work_item'),
	period_mode                TEXT NOT NULL DEFAULT 'custom' CHECK (period_mode IN ('weekly','biweekly','semimonthly','monthly','final','custom')),
	period_anchor              TEXT NOT NULL DEFAULT '',
	legacy_type                TEXT CHECK (legacy_type IN ('billable','internal')),
	created_at                 TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at                 TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(scope_type, scope_key)
);
INSERT INTO billing_profiles (work_item_id, scope_type, scope_key, enabled, legacy_type)
SELECT work_item_id, 'work_item', CAST(work_item_id AS TEXT), CASE WHEN legacy_type='billable' THEN 1 ELSE 0 END, legacy_type FROM billing_profiles_legacy;
DROP TABLE billing_profiles_legacy;

CREATE TABLE report_snapshots (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	label       TEXT NOT NULL DEFAULT '',
	period_mode TEXT NOT NULL CHECK (period_mode IN ('weekly','biweekly','semimonthly','monthly','final','custom')),
	from_time   TIMESTAMP NOT NULL,
	to_time     TIMESTAMP NOT NULL,
	timezone    TEXT NOT NULL,
	payload     TEXT NOT NULL,
	created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_report_snapshots_created ON report_snapshots(created_at DESC);
`,
	// 0006 — classification history survives replacement of disposable capture
	// fragments and remains attributable by stable source key.
	`
CREATE TABLE classification_audit_new (
	id               INTEGER PRIMARY KEY AUTOINCREMENT,
	event_id         INTEGER REFERENCES events(id) ON DELETE SET NULL,
	source_key       TEXT NOT NULL DEFAULT '',
	old_work_item_id INTEGER REFERENCES work_items(id) ON DELETE SET NULL,
	new_work_item_id INTEGER REFERENCES work_items(id) ON DELETE SET NULL,
	old_rule_id      INTEGER REFERENCES rules(id) ON DELETE SET NULL,
	new_rule_id      INTEGER REFERENCES rules(id) ON DELETE SET NULL,
	old_source       TEXT NOT NULL DEFAULT '',
	new_source       TEXT NOT NULL DEFAULT '',
	created_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO classification_audit_new (id, event_id, source_key, old_work_item_id, new_work_item_id, old_rule_id, new_rule_id, old_source, new_source, created_at)
SELECT a.id, a.event_id, COALESCE(e.source_key, ''), a.old_work_item_id, a.new_work_item_id, a.old_rule_id, a.new_rule_id, a.old_source, a.new_source, a.created_at
FROM classification_audit a
LEFT JOIN events e ON e.id = a.event_id;
DROP TABLE classification_audit;
ALTER TABLE classification_audit_new RENAME TO classification_audit;
CREATE INDEX idx_classification_audit_event ON classification_audit(event_id, created_at);
CREATE INDEX idx_classification_audit_source_key ON classification_audit(source_key, created_at);
`,
}
