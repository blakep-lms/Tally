// Package core is Tally's application service: the single set of operations
// that the CLI, the MCP server, and the web UI all call. Keeping every action
// here is what guarantees human/agent parity — there is no capability reachable
// from one surface that isn't reachable from the others.
package core

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/blakep-lms/tally/internal/capture"
	"github.com/blakep-lms/tally/internal/classify"
	"github.com/blakep-lms/tally/internal/config"
	"github.com/blakep-lms/tally/internal/model"
	"github.com/blakep-lms/tally/internal/report"
	"github.com/blakep-lms/tally/internal/store"
)

// App bundles the store and config into the shared service surface.
type App struct {
	Store *store.Store
	Cfg   config.Config
}

// New builds an App.
func New(st *store.Store, cfg config.Config) *App {
	return &App{Store: st, Cfg: cfg}
}

// Provider returns the configured capture provider (ActivityWatch in v1).
func (a *App) Provider() capture.Provider {
	return capture.NewAW(a.Cfg.ActivityWatchURL)
}

// --- Status -------------------------------------------------------------

// Status is a snapshot of the system for `tally status`.
type Status struct {
	Provider          string  `json:"provider"`
	ProviderConnected bool    `json:"provider_connected"`
	ActiveProjects    int     `json:"active_projects"`
	DoneProjects      int     `json:"done_projects"`
	ActiveRules       int     `json:"active_rules"`
	EventsTotal       int     `json:"events_total"`
	UnclassifiedToday float64 `json:"unclassified_hours_today"`
	TrackedToday      float64 `json:"tracked_hours_today"`
	TrackedWeek       float64 `json:"tracked_hours_week"`
	LLMEnabled        bool    `json:"llm_enabled"`
}

// Status computes the current snapshot.
func (a *App) Status(ctx context.Context) (Status, error) {
	var s Status
	p := a.Provider()
	s.Provider = p.Name()
	s.ProviderConnected = p.Available(ctx)
	s.LLMEnabled = a.Cfg.LLMEnabled

	active, err := a.Store.ListProjects(model.StatusActive)
	if err != nil {
		return s, err
	}
	s.ActiveProjects = len(active)
	done, err := a.Store.ListProjects(model.StatusDone)
	if err != nil {
		return s, err
	}
	s.DoneProjects = len(done)

	rules, err := a.Store.ListRules(true)
	if err != nil {
		return s, err
	}
	s.ActiveRules = len(rules)

	all, err := a.Store.ListEvents(store.EventFilter{})
	if err != nil {
		return s, err
	}
	s.EventsTotal = len(all)

	dayFrom, dayTo := TodayRange(time.Now())
	weekFrom, weekTo := WeekRange(time.Now())
	if secs, err := a.Store.UnclassifiedSeconds(dayFrom, dayTo); err == nil {
		s.UnclassifiedToday = secs / 3600
	}
	if secs, err := a.Store.TotalSeconds(dayFrom, dayTo); err == nil {
		s.TrackedToday = secs / 3600
	}
	if secs, err := a.Store.TotalSeconds(weekFrom, weekTo); err == nil {
		s.TrackedWeek = secs / 3600
	}
	return s, nil
}

// --- Projects -----------------------------------------------------------

// AddProject creates a project.
func (a *App) AddProject(name string, typ model.ProjectType, client string) (model.Project, error) {
	return a.Store.CreateProject(name, typ, client)
}

// ListProjects lists projects filtered by status ("" = all).
func (a *App) ListProjects(status model.ProjectStatus) ([]model.Project, error) {
	return a.Store.ListProjects(status)
}

// ResolveProject looks up a project by numeric id or by exact name.
func (a *App) ResolveProject(idOrName string) (model.Project, error) {
	if id, err := strconv.ParseInt(idOrName, 10, 64); err == nil {
		return a.Store.GetProject(id)
	}
	return a.Store.GetProjectByName(idOrName)
}

// MarkDone archives a project.
func (a *App) MarkDone(idOrName string) (model.Project, error) {
	p, err := a.ResolveProject(idOrName)
	if err != nil {
		return model.Project{}, err
	}
	return a.Store.MarkDone(p.ID)
}

// --- Rules --------------------------------------------------------------

// AddRule creates a classification rule for a project.
func (a *App) AddRule(projectIDOrName string, field model.RuleField, match model.MatchKind, pattern string, priority int) (model.Rule, error) {
	p, err := a.ResolveProject(projectIDOrName)
	if err != nil {
		return model.Rule{}, err
	}
	return a.Store.CreateRule(model.Rule{
		ProjectID: p.ID,
		Field:     field,
		Match:     match,
		Pattern:   pattern,
		Priority:  priority,
	})
}

// ListRules lists rules (activeOnly limits to live rules of active projects).
func (a *App) ListRules(activeOnly bool) ([]model.Rule, error) {
	return a.Store.ListRules(activeOnly)
}

// DeleteRule removes a rule by id.
func (a *App) DeleteRule(id int64) error { return a.Store.DeleteRule(id) }

// TestRule reports which of the current unclassified events a candidate rule
// would match, without persisting the rule. Useful for `rules test`.
func (a *App) TestRule(field model.RuleField, match model.MatchKind, pattern string, limit int) ([]model.Event, error) {
	probe := model.Rule{ProjectID: -1, Field: field, Match: match, Pattern: pattern, Priority: 1, Active: true}
	eng := classify.NewEngine([]model.Rule{probe})
	events, err := a.Store.ListEvents(store.EventFilter{})
	if err != nil {
		return nil, err
	}
	var hits []model.Event
	for _, e := range events {
		if _, ok := eng.Classify(e); ok {
			hits = append(hits, e)
			if limit > 0 && len(hits) >= limit {
				break
			}
		}
	}
	return hits, nil
}

// --- Capture / sync -----------------------------------------------------

// SyncResult reports what a sync pulled.
type SyncResult struct {
	Pulled  int `json:"pulled"`
	Created int `json:"created"`
	Updated int `json:"updated"`
}

// Sync pulls events from the provider for [from, to) and upserts them. It is
// idempotent: re-running over the same window creates no duplicates.
func (a *App) Sync(ctx context.Context, from, to time.Time) (SyncResult, error) {
	var res SyncResult
	p := a.Provider()
	if !p.Available(ctx) {
		return res, fmt.Errorf("capture provider %q is not reachable at %s", p.Name(), a.Cfg.ActivityWatchURL)
	}
	events, err := p.Pull(ctx, from, to)
	if err != nil {
		return res, err
	}
	res.Pulled = len(events)
	for _, e := range events {
		created, err := a.Store.UpsertEvent(e)
		if err != nil {
			return res, err
		}
		if created {
			res.Created++
		} else {
			res.Updated++
		}
	}
	return res, nil
}

// IngestEvents upserts externally-supplied events (used for seeding/testing and
// by capture backends that push rather than pull).
func (a *App) IngestEvents(events []model.Event) (SyncResult, error) {
	var res SyncResult
	res.Pulled = len(events)
	for _, e := range events {
		created, err := a.Store.UpsertEvent(e)
		if err != nil {
			return res, err
		}
		if created {
			res.Created++
		} else {
			res.Updated++
		}
	}
	return res, nil
}

// --- Classification -----------------------------------------------------

// ClassifyResult reports how a classification pass bucketed events.
type ClassifyResult struct {
	Considered        int `json:"considered"`
	MatchedByRule     int `json:"matched_by_rule"`
	MatchedByLLM      int `json:"matched_by_llm"`
	StillUnclassified int `json:"still_unclassified"`
}

// Classify runs the deterministic rule engine over all unclassified events and,
// when useLLM is set and LLM classification is enabled, falls back to the model
// for whatever the rules didn't catch.
func (a *App) Classify(ctx context.Context, useLLM bool) (ClassifyResult, error) {
	var res ClassifyResult
	rules, err := a.Store.ListRules(true)
	if err != nil {
		return res, err
	}
	eng := classify.NewEngine(rules)

	pending, err := a.Store.ListEvents(store.EventFilter{UnclassOnly: true})
	if err != nil {
		return res, err
	}
	res.Considered = len(pending)

	var remaining []model.Event
	for _, e := range pending {
		if m, ok := eng.Classify(e); ok {
			rid := m.RuleID
			if err := a.Store.ClassifyEvent(e.ID, &m.ProjectID, &rid, m.Source); err != nil {
				return res, err
			}
			res.MatchedByRule++
		} else {
			remaining = append(remaining, e)
		}
	}

	if useLLM && a.Cfg.LLMEnabled {
		n, err := a.classifyWithLLM(ctx, remaining)
		if err != nil {
			return res, fmt.Errorf("llm classification: %w", err)
		}
		res.MatchedByLLM = n
	}

	res.StillUnclassified = len(remaining) - res.MatchedByLLM
	return res, nil
}

func (a *App) classifyWithLLM(ctx context.Context, events []model.Event) (int, error) {
	key := a.Cfg.APIKey()
	if key == "" {
		return 0, errors.New("LLM enabled but no API key configured (set anthropic_api_key or ANTHROPIC_API_KEY)")
	}
	if len(events) == 0 {
		return 0, nil
	}
	projects, err := a.Store.ListProjects(model.StatusActive)
	if err != nil {
		return 0, err
	}
	if len(projects) == 0 {
		return 0, nil
	}

	// Deduplicate to unique signals; resolve as many as possible from cache.
	type sigInfo struct {
		signal   classify.Signal
		projPtr  *int64
		resolved bool
	}
	uniq := map[string]*sigInfo{}
	var order []string
	for _, e := range events {
		s := classify.SignalOf(e)
		k := s.Key()
		if _, ok := uniq[k]; ok {
			continue
		}
		info := &sigInfo{signal: s}
		if pid, hit, err := a.Store.LLMCacheGet(k); err == nil && hit {
			info.resolved = true
			if pid != 0 {
				info.projPtr = &pid
			}
		}
		uniq[k] = info
		order = append(order, k)
	}

	// Collect the still-unresolved signals and ask the model in one batch.
	var askKeys []string
	var askSignals []classify.Signal
	for _, k := range order {
		if !uniq[k].resolved {
			askKeys = append(askKeys, k)
			askSignals = append(askSignals, uniq[k].signal)
		}
	}
	if len(askSignals) > 0 {
		llm := classify.NewLLMClassifier(key, a.Cfg.LLMModel)
		suggestions, err := llm.Suggest(ctx, projects, askSignals)
		if err != nil {
			return 0, err
		}
		for i, k := range askKeys {
			uniq[k].resolved = true
			uniq[k].projPtr = suggestions[i]
			if err := a.Store.LLMCachePut(k, suggestions[i]); err != nil {
				return 0, err
			}
		}
	}

	// Apply resolved signals back to every matching event.
	matched := 0
	for _, e := range events {
		info := uniq[classify.SignalOf(e).Key()]
		if info == nil || info.projPtr == nil {
			continue
		}
		if err := a.Store.ClassifyEvent(e.ID, info.projPtr, nil, "llm"); err != nil {
			return matched, err
		}
		matched++
	}
	return matched, nil
}

// AssignEvent manually attributes an event to a project (source "manual"), and
// optionally generates a rule so similar events classify automatically. Passing
// projectIDOrName == "" clears the classification.
func (a *App) AssignEvent(eventID int64, projectIDOrName string, makeRule bool, ruleField model.RuleField) (model.Rule, bool, error) {
	if projectIDOrName == "" {
		return model.Rule{}, false, a.Store.ClassifyEvent(eventID, nil, nil, "")
	}
	p, err := a.ResolveProject(projectIDOrName)
	if err != nil {
		return model.Rule{}, false, err
	}
	if err := a.Store.ClassifyEvent(eventID, &p.ID, nil, "manual"); err != nil {
		return model.Rule{}, false, err
	}
	if !makeRule {
		return model.Rule{}, false, nil
	}
	events, err := a.Store.ListEvents(store.EventFilter{})
	if err != nil {
		return model.Rule{}, false, err
	}
	var ev *model.Event
	for i := range events {
		if events[i].ID == eventID {
			ev = &events[i]
			break
		}
	}
	if ev == nil {
		return model.Rule{}, false, store.ErrNotFound
	}
	pattern := ruleValueFor(ruleField, *ev)
	if pattern == "" {
		return model.Rule{}, false, fmt.Errorf("event has no %s value to build a rule from", ruleField)
	}
	rule, err := a.Store.CreateRule(model.Rule{
		ProjectID: p.ID,
		Field:     ruleField,
		Match:     model.MatchContains,
		Pattern:   pattern,
		Priority:  100,
	})
	return rule, err == nil, err
}

func ruleValueFor(f model.RuleField, e model.Event) string {
	switch f {
	case model.FieldApp:
		return e.App
	case model.FieldTitle:
		return e.Title
	case model.FieldURL:
		return e.URL
	case model.FieldRepo:
		return e.Repo
	}
	return ""
}

// ListUnclassified returns the unclassified triage queue.
func (a *App) ListUnclassified(limit int) ([]model.Event, error) {
	return a.Store.ListEvents(store.EventFilter{UnclassOnly: true, Limit: limit})
}

// --- Reports ------------------------------------------------------------

// Report computes an hours report for [from, to).
func (a *App) Report(from, to time.Time) (report.Report, error) {
	hours, err := a.Store.HoursByProject(from, to)
	if err != nil {
		return report.Report{}, err
	}
	unclassified, err := a.Store.UnclassifiedSeconds(from, to)
	if err != nil {
		return report.Report{}, err
	}
	total, err := a.Store.TotalSeconds(from, to)
	if err != nil {
		return report.Report{}, err
	}
	return report.Build(from, to, hours, unclassified, total), nil
}

// --- Time windows -------------------------------------------------------

// TodayRange returns [midnight, next midnight) in local time for t.
func TodayRange(t time.Time) (time.Time, time.Time) {
	y, m, d := t.Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, t.Location())
	return start, start.AddDate(0, 0, 1)
}

// WeekRange returns the Monday-anchored week [start, start+7d) containing t.
func WeekRange(t time.Time) (time.Time, time.Time) {
	y, m, d := t.Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, t.Location())
	// Go: Sunday=0..Saturday=6; anchor to Monday.
	offset := (int(start.Weekday()) + 6) % 7
	start = start.AddDate(0, 0, -offset)
	return start, start.AddDate(0, 0, 7)
}
