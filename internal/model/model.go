// Package model defines the core domain types shared across Tally.
package model

import "time"

// ProjectType distinguishes client-billable work from internal/personal builds.
type ProjectType string

const (
	// TypeBillable is client work that ends up on an invoice.
	TypeBillable ProjectType = "billable"
	// TypeInternal is internal tooling or personal builds.
	TypeInternal ProjectType = "internal"
)

// Valid reports whether t is a recognized project type.
func (t ProjectType) Valid() bool {
	return t == TypeBillable || t == TypeInternal
}

// ProjectStatus is the lifecycle state of a project.
type ProjectStatus string

const (
	// StatusActive projects accept new time and their rules are live.
	StatusActive ProjectStatus = "active"
	// StatusDone projects are archived: rules deactivate, history is frozen.
	StatusDone ProjectStatus = "done"
)

// Valid reports whether s is a recognized status.
func (s ProjectStatus) Valid() bool {
	return s == StatusActive || s == StatusDone
}

// Project is a unit of work time is attributed to.
type Project struct {
	ID        int64         `json:"id"`
	Name      string        `json:"name"`
	Type      ProjectType   `json:"type"`
	Client    string        `json:"client,omitempty"`
	Status    ProjectStatus `json:"status"`
	CreatedAt time.Time     `json:"created_at"`
	DoneAt    *time.Time    `json:"done_at,omitempty"`
}

// RuleField is the event attribute a rule matches against.
type RuleField string

const (
	// FieldApp matches the application name (e.g. "Code", "iTerm2").
	FieldApp RuleField = "app"
	// FieldTitle matches the window title.
	FieldTitle RuleField = "title"
	// FieldURL matches a browser tab URL.
	FieldURL RuleField = "url"
	// FieldRepo matches a detected git repository path or name.
	FieldRepo RuleField = "repo"
)

// Valid reports whether f is a recognized rule field.
func (f RuleField) Valid() bool {
	switch f {
	case FieldApp, FieldTitle, FieldURL, FieldRepo:
		return true
	}
	return false
}

// MatchKind is how a rule's pattern is compared against a field value.
type MatchKind string

const (
	// MatchContains is a case-insensitive substring match.
	MatchContains MatchKind = "contains"
	// MatchEquals is a case-insensitive exact match.
	MatchEquals MatchKind = "equals"
	// MatchRegex is a Go regular expression match.
	MatchRegex MatchKind = "regex"
)

// Valid reports whether k is a recognized match kind.
func (k MatchKind) Valid() bool {
	switch k {
	case MatchContains, MatchEquals, MatchRegex:
		return true
	}
	return false
}

// Rule maps events to a project. Rules are ordered; first match wins.
type Rule struct {
	ID        int64     `json:"id"`
	ProjectID int64     `json:"project_id"`
	Field     RuleField `json:"field"`
	Match     MatchKind `json:"match"`
	Pattern   string    `json:"pattern"`
	Priority  int       `json:"priority"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

// Event is a single interval of focused activity captured from the watcher.
type Event struct {
	ID       int64     `json:"id"`
	Start    time.Time `json:"start"`
	Duration float64   `json:"duration_seconds"`
	App      string    `json:"app"`
	Title    string    `json:"title"`
	URL      string    `json:"url,omitempty"`
	Repo     string    `json:"repo,omitempty"`
	// ProjectID is the classified project, or nil when unclassified.
	ProjectID *int64 `json:"project_id,omitempty"`
	// RuleID records which rule classified the event, when applicable.
	RuleID *int64 `json:"rule_id,omitempty"`
	// Source records how it was classified: "rule", "llm", "manual", or "".
	Source string `json:"source,omitempty"`
	// SourceKey is a stable external id (e.g. AW bucket:event) for dedupe.
	SourceKey string `json:"source_key,omitempty"`
}

// ProjectHours is an aggregate of tracked time for a project over a window.
type ProjectHours struct {
	Project Project `json:"project"`
	Seconds float64 `json:"seconds"`
	Hours   float64 `json:"hours"`
}
