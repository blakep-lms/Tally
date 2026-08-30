// Package model defines the core domain types shared across Tally.
package model

import "time"

// WorkItemKind distinguishes the canonical kinds of work Tally tracks.
type WorkItemKind string

const (
	KindProject WorkItemKind = "project"
	KindProduct WorkItemKind = "product"
	KindGoal    WorkItemKind = "goal"
	KindOther   WorkItemKind = "other"
)

func (k WorkItemKind) Valid() bool {
	switch k {
	case KindProject, KindProduct, KindGoal, KindOther:
		return true
	}
	return false
}

// WorkItemStatus is the lifecycle state of a work item.
type WorkItemStatus string

const (
	StatusActive WorkItemStatus = "active"
	StatusDone   WorkItemStatus = "done"
)

func (s WorkItemStatus) Valid() bool { return s == StatusActive || s == StatusDone }

// WorkItem is the canonical unit of work time is attributed to.
type WorkItem struct {
	ID          int64          `json:"id"`
	Name        string         `json:"name"`
	Kind        WorkItemKind   `json:"kind"`
	Context     string         `json:"context,omitempty"`
	Description string         `json:"description,omitempty"`
	Status      WorkItemStatus `json:"status"`
	CreatedAt   time.Time      `json:"created_at"`
	DoneAt      *time.Time     `json:"done_at,omitempty"`
}

// ProjectType is a legacy CLI/API compatibility shim. Billing intent is stored
// separately in billing_profiles; new code should use WorkItemKind.
type ProjectType string

const (
	TypeBillable ProjectType = "billable"
	TypeInternal ProjectType = "internal"
)

func (t ProjectType) Valid() bool { return t == TypeBillable || t == TypeInternal }

// ProjectStatus is a legacy alias for work item status.
type ProjectStatus = WorkItemStatus

// Project is a legacy compatibility shape backed by WorkItem.
type Project struct {
	ID        int64         `json:"id"`
	Name      string        `json:"name"`
	Type      ProjectType   `json:"type"`
	Client    string        `json:"client,omitempty"`
	Status    ProjectStatus `json:"status"`
	CreatedAt time.Time     `json:"created_at"`
	DoneAt    *time.Time    `json:"done_at,omitempty"`
}

func ProjectFromWorkItem(w WorkItem, typ ProjectType) Project {
	if typ == "" {
		typ = TypeBillable
	}
	return Project{ID: w.ID, Name: w.Name, Type: typ, Client: w.Context, Status: w.Status, CreatedAt: w.CreatedAt, DoneAt: w.DoneAt}
}

func WorkItemFromProject(p Project) WorkItem {
	return WorkItem{ID: p.ID, Name: p.Name, Kind: KindProject, Context: p.Client, Status: p.Status, CreatedAt: p.CreatedAt, DoneAt: p.DoneAt}
}

// RuleField is the event attribute a rule matches against.
type RuleField string

const (
	FieldApp   RuleField = "app"
	FieldTitle RuleField = "title"
	FieldURL   RuleField = "url"
	FieldRepo  RuleField = "repo"
)

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
	MatchContains MatchKind = "contains"
	MatchEquals   MatchKind = "equals"
	MatchRegex    MatchKind = "regex"
)

func (k MatchKind) Valid() bool {
	switch k {
	case MatchContains, MatchEquals, MatchRegex:
		return true
	}
	return false
}

// Rule maps events to a work item. Rules are ordered; first match wins.
type Rule struct {
	ID         int64     `json:"id"`
	WorkItemID int64     `json:"work_item_id"`
	ProjectID  int64     `json:"project_id,omitempty"` // legacy alias
	Field      RuleField `json:"field"`
	Match      MatchKind `json:"match"`
	Pattern    string    `json:"pattern"`
	Priority   int       `json:"priority"`
	Active     bool      `json:"active"`
	CreatedAt  time.Time `json:"created_at"`
}

// Event is a single interval of focused activity captured from the watcher.
type Event struct {
	ID          int64     `json:"id"`
	Start       time.Time `json:"start"`
	Duration    float64   `json:"duration_seconds"`
	App         string    `json:"app"`
	Title       string    `json:"title"`
	URL         string    `json:"url,omitempty"`
	Repo        string    `json:"repo,omitempty"`
	WorkItemID  *int64    `json:"work_item_id,omitempty"`
	ProjectID   *int64    `json:"project_id,omitempty"` // legacy alias
	RuleID      *int64    `json:"rule_id,omitempty"`
	Source      string    `json:"source,omitempty"`
	SourceKey   string    `json:"source_key,omitempty"`
	SourceGroup string    `json:"source_group,omitempty"`
	// CaptureComplete is an internal reconciliation marker emitted by capture
	// providers. It is never persisted or returned by public APIs.
	CaptureComplete bool `json:"-"`
}

// WorkItemHours is an aggregate of tracked time for a work item over a window.
type WorkItemHours struct {
	WorkItem WorkItem `json:"work_item"`
	Seconds  float64  `json:"seconds"`
	Hours    float64  `json:"hours"`
}

type ProjectHours struct {
	Project Project `json:"project"`
	Seconds float64 `json:"seconds"`
	Hours   float64 `json:"hours"`
}

// ClassificationAudit records every classification change, including manual
// corrections and rule/LLM assignments, so reported history can be reviewed.
type ClassificationAudit struct {
	ID            int64     `json:"id"`
	EventID       *int64    `json:"event_id,omitempty"`
	SourceKey     string    `json:"source_key"`
	OldWorkItemID *int64    `json:"old_work_item_id,omitempty"`
	NewWorkItemID *int64    `json:"new_work_item_id,omitempty"`
	OldRuleID     *int64    `json:"old_rule_id,omitempty"`
	NewRuleID     *int64    `json:"new_rule_id,omitempty"`
	OldSource     string    `json:"old_source"`
	NewSource     string    `json:"new_source"`
	CreatedAt     time.Time `json:"created_at"`
}
