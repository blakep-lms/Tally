// Package core is Tally's application service.
package core

import (
	"context"
	"encoding/json"
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

type App struct {
	Store *store.Store
	Cfg   config.Config
}

func New(st *store.Store, cfg config.Config) *App { return &App{Store: st, Cfg: cfg} }
func (a *App) Provider() capture.Provider {
	return capture.NewAWWithPrivacy(a.Cfg.ActivityWatchURL, a.Cfg.IgnoredApps, a.Cfg.StoreURLPaths)
}

type Status struct {
	Provider          string  `json:"provider"`
	ProviderConnected bool    `json:"provider_connected"`
	ActiveWorkItems   int     `json:"active_work_items"`
	DoneWorkItems     int     `json:"done_work_items"`
	ActiveProjects    int     `json:"active_projects"`
	DoneProjects      int     `json:"done_projects"`
	ActiveRules       int     `json:"active_rules"`
	EventsTotal       int     `json:"events_total"`
	UnclassifiedToday float64 `json:"unclassified_hours_today"`
	TrackedToday      float64 `json:"tracked_hours_today"`
	TrackedWeek       float64 `json:"tracked_hours_week"`
	LLMEnabled        bool    `json:"llm_enabled"`
}

func (a *App) Status(ctx context.Context) (Status, error) {
	var s Status
	p := a.Provider()
	s.Provider = p.Name()
	s.ProviderConnected = p.Available(ctx)
	s.LLMEnabled = a.Cfg.LLMEnabled
	active, err := a.Store.ListWorkItems(model.StatusActive)
	if err != nil {
		return s, err
	}
	done, err := a.Store.ListWorkItems(model.StatusDone)
	if err != nil {
		return s, err
	}
	s.ActiveWorkItems = len(active)
	s.DoneWorkItems = len(done)
	for _, w := range active {
		if w.Kind == model.KindProject {
			s.ActiveProjects++
		}
	}
	for _, w := range done {
		if w.Kind == model.KindProject {
			s.DoneProjects++
		}
	}
	rules, err := a.Store.ListRules(true)
	if err != nil {
		return s, err
	}
	s.ActiveRules = len(rules)
	eventCount, err := a.Store.CountEvents()
	if err != nil {
		return s, err
	}
	s.EventsTotal = eventCount
	dayFrom, dayTo := TodayRange(time.Now())
	weekFrom, weekTo := WeekRange(time.Now())
	if _, unclassified, total, err := a.Store.ReportWindow(dayFrom, dayTo); err == nil {
		s.UnclassifiedToday = unclassified / 3600
		s.TrackedToday = total / 3600
	}
	if _, _, total, err := a.Store.ReportWindow(weekFrom, weekTo); err == nil {
		s.TrackedWeek = total / 3600
	}
	return s, nil
}

func (a *App) AddWorkItem(name string, kind model.WorkItemKind, context, description string) (model.WorkItem, error) {
	return a.Store.CreateWorkItem(name, kind, context, description)
}
func (a *App) ListWorkItems(status model.WorkItemStatus) ([]model.WorkItem, error) {
	return a.Store.ListWorkItems(status)
}
func (a *App) UpdateWorkItem(id int64, name string, kind model.WorkItemKind, context, description string) (model.WorkItem, error) {
	return a.Store.UpdateWorkItem(id, name, kind, context, description)
}
func (a *App) ResolveWorkItem(idOrName string) (model.WorkItem, error) {
	if id, err := strconv.ParseInt(idOrName, 10, 64); err == nil {
		return a.Store.GetWorkItem(id)
	}
	return a.Store.GetWorkItemByName(idOrName)
}
func (a *App) MarkWorkItemDone(idOrName string) (model.WorkItem, error) {
	w, err := a.ResolveWorkItem(idOrName)
	if err != nil {
		return model.WorkItem{}, err
	}
	return a.Store.MarkWorkItemDone(w.ID)
}
func (a *App) ReactivateWorkItem(idOrName string) (model.WorkItem, error) {
	w, err := a.ResolveWorkItem(idOrName)
	if err != nil {
		return model.WorkItem{}, err
	}
	return a.Store.ReactivateWorkItem(w.ID)
}

func (a *App) AddProject(name string, typ model.ProjectType, client string) (model.Project, error) {
	return a.Store.CreateProject(name, typ, client)
}
func (a *App) ListProjects(status model.ProjectStatus) ([]model.Project, error) {
	return a.Store.ListProjects(status)
}
func (a *App) ResolveProject(idOrName string) (model.Project, error) {
	if id, err := strconv.ParseInt(idOrName, 10, 64); err == nil {
		return a.Store.GetProject(id)
	}
	return a.Store.GetProjectByName(idOrName)
}
func (a *App) MarkDone(idOrName string) (model.Project, error) {
	p, err := a.ResolveProject(idOrName)
	if err != nil {
		return model.Project{}, err
	}
	return a.Store.MarkDone(p.ID)
}

func (a *App) AddRule(workItemIDOrName string, field model.RuleField, match model.MatchKind, pattern string, priority int) (model.Rule, error) {
	w, err := a.ResolveWorkItem(workItemIDOrName)
	if err != nil {
		return model.Rule{}, err
	}
	return a.Store.CreateRule(model.Rule{WorkItemID: w.ID, Field: field, Match: match, Pattern: pattern, Priority: priority})
}
func (a *App) ListRules(activeOnly bool) ([]model.Rule, error) { return a.Store.ListRules(activeOnly) }
func (a *App) DeleteRule(id int64) error                       { return a.Store.DeleteRule(id) }
func (a *App) TestRule(field model.RuleField, match model.MatchKind, pattern string, limit int) ([]model.Event, error) {
	probe := model.Rule{WorkItemID: -1, Field: field, Match: match, Pattern: pattern, Priority: 1, Active: true}
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

type SyncResult struct {
	Pulled    int `json:"pulled"`
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Deleted   int `json:"deleted"`
	Conflicts int `json:"conflicts"`
}

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
	return a.IngestEvents(events)
}
func (a *App) IngestEvents(events []model.Event) (SyncResult, error) {
	var res SyncResult
	groups := map[string][]model.Event{}
	complete := map[string]bool{}
	for _, e := range events {
		if e.SourceGroup != "" {
			if e.Duration > 0 && e.SourceKey != "" {
				groups[e.SourceGroup] = append(groups[e.SourceGroup], e)
				res.Pulled++
			}
			if e.CaptureComplete {
				complete[e.SourceGroup] = true
			}
			continue
		}
		if e.Duration <= 0 {
			continue
		}
		res.Pulled++
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
	for group := range complete {
		created, updated, deleted, conflicts, err := a.Store.ReplaceEventGroup(group, groups[group])
		if err != nil {
			return res, err
		}
		res.Created += created
		res.Updated += updated
		res.Deleted += deleted
		res.Conflicts += conflicts
	}
	// A grouped event without the completion marker is intentionally not used
	// for destructive reconciliation; ingest it conservatively instead.
	for group, pending := range groups {
		if complete[group] {
			continue
		}
		for _, e := range pending {
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
	}
	return res, nil
}

type ClassifyResult struct {
	Considered        int `json:"considered"`
	MatchedByRule     int `json:"matched_by_rule"`
	MatchedByLLM      int `json:"matched_by_llm"`
	StillUnclassified int `json:"still_unclassified"`
}

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
			if err := a.Store.ClassifyEvent(e.ID, &m.WorkItemID, &rid, m.Source); err != nil {
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
	items, err := a.Store.ListWorkItems(model.StatusActive)
	if err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, nil
	}
	type sigInfo struct {
		signal   classify.Signal
		ptr      *int64
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
		if id, hit, err := a.Store.LLMCacheGet(k); err == nil && hit {
			info.resolved = true
			if id != 0 {
				info.ptr = &id
			}
		}
		uniq[k] = info
		order = append(order, k)
	}
	var askKeys []string
	var askSignals []classify.Signal
	for _, k := range order {
		if !uniq[k].resolved {
			askKeys = append(askKeys, k)
			askSignals = append(askSignals, uniq[k].signal)
		}
	}
	if len(askSignals) > 0 {
		llm := classify.NewLLMClassifier(key, a.Cfg.LLMModel, a.Cfg.LLMMinConfidence)
		suggestions, err := llm.Suggest(ctx, items, askSignals)
		if err != nil {
			return 0, err
		}
		for i, k := range askKeys {
			uniq[k].resolved = true
			uniq[k].ptr = suggestions[i]
			if err := a.Store.LLMCachePut(k, suggestions[i]); err != nil {
				return 0, err
			}
		}
	}
	matched := 0
	for _, e := range events {
		info := uniq[classify.SignalOf(e).Key()]
		if info == nil || info.ptr == nil {
			continue
		}
		if err := a.Store.ClassifyEvent(e.ID, info.ptr, nil, "llm"); err != nil {
			return matched, err
		}
		matched++
	}
	return matched, nil
}

func (a *App) AssignEvent(eventID int64, workItemIDOrName string, makeRule bool, ruleField model.RuleField) (model.Rule, bool, error) {
	if workItemIDOrName == "" {
		return model.Rule{}, false, a.Store.ClassifyEvent(eventID, nil, nil, "")
	}
	w, err := a.ResolveWorkItem(workItemIDOrName)
	if err != nil {
		return model.Rule{}, false, err
	}
	if err := a.Store.ClassifyEvent(eventID, &w.ID, nil, "manual"); err != nil {
		return model.Rule{}, false, err
	}
	if !makeRule {
		return model.Rule{}, false, nil
	}
	ev, err := a.Store.GetEvent(eventID)
	if err != nil {
		return model.Rule{}, false, err
	}
	pattern := ruleValueFor(ruleField, ev)
	if pattern == "" {
		return model.Rule{}, false, fmt.Errorf("event has no %s value to build a rule from", ruleField)
	}
	rule, err := a.Store.CreateRule(model.Rule{WorkItemID: w.ID, Field: ruleField, Match: model.MatchContains, Pattern: pattern, Priority: 100})
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
func (a *App) ListUnclassified(limit int) ([]model.Event, error) {
	return a.Store.ListEvents(store.EventFilter{UnclassOnly: true, Limit: limit})
}
func (a *App) Report(from, to time.Time) (report.Report, error) {
	return a.ReportWithBilling(from, to, false)
}

func (a *App) ReportWithBilling(from, to time.Time, includeBilling bool) (report.Report, error) {
	hours, unclassified, total, err := a.Store.ReportWindow(from, to)
	if err != nil {
		return report.Report{}, err
	}
	var profiles map[int64]model.BillingProfile
	if includeBilling {
		profiles = map[int64]model.BillingProfile{}
		for _, wh := range hours {
			resolved, err := a.Store.ResolveBillingProfile(wh.WorkItem)
			if err != nil {
				return report.Report{}, err
			}
			profiles[wh.WorkItem.ID] = resolved.Profile
		}
	}
	return report.BuildWorkItemsChecked(from, to, hours, unclassified, total, profiles)
}

func (a *App) SetBillingProfile(p model.BillingProfile) (model.BillingProfile, error) {
	return a.Store.SetBillingProfile(p)
}
func (a *App) PatchBillingProfile(patch model.BillingProfilePatch) (model.BillingProfile, error) {
	if !patch.ScopeType.Valid() {
		return model.BillingProfile{}, fmt.Errorf("invalid billing scope %q", patch.ScopeType)
	}
	base, err := a.Store.GetBillingProfile(patch.ScopeType, patch.ScopeKey)
	if errors.Is(err, store.ErrNotFound) {
		base = model.DefaultBillingProfile()
	} else if err != nil {
		return model.BillingProfile{}, err
	}
	return a.Store.SetBillingProfile(patch.Apply(base))
}
func (a *App) ResolveBillingProfile(itemIDOrName string) (store.ResolvedBillingProfile, error) {
	w, err := a.ResolveWorkItem(itemIDOrName)
	if err != nil {
		return store.ResolvedBillingProfile{}, err
	}
	return a.Store.ResolveBillingProfile(w)
}

func (a *App) ReportWorkItemWithBilling(item model.WorkItem, from, to time.Time, includeBilling bool) (report.Report, error) {
	hours, err := a.Store.HoursByWorkItem(from, to)
	if err != nil {
		return report.Report{}, err
	}
	var selected []model.WorkItemHours
	var total float64
	for _, line := range hours {
		if line.WorkItem.ID == item.ID {
			selected = append(selected, line)
			total = line.Seconds
			break
		}
	}
	var profiles map[int64]model.BillingProfile
	if includeBilling {
		resolved, err := a.Store.ResolveBillingProfile(item)
		if err != nil {
			return report.Report{}, err
		}
		profiles = map[int64]model.BillingProfile{item.ID: resolved.Profile}
	}
	return report.BuildWorkItemsChecked(from, to, selected, 0, total, profiles)
}

func FinalRange(item model.WorkItem, now time.Time) (time.Time, time.Time) {
	to := now
	if item.DoneAt != nil {
		to = *item.DoneAt
	}
	return item.CreatedAt, to
}

func (a *App) FinalizeReport(rep report.Report, label string, period model.PeriodMode, timezone string) (model.ReportSnapshot, error) {
	payload, err := json.Marshal(rep)
	if err != nil {
		return model.ReportSnapshot{}, err
	}
	return a.Store.SaveReportSnapshot(model.ReportSnapshot{Label: label, PeriodMode: period, From: rep.From, To: rep.To, Timezone: timezone, Payload: payload})
}

func PeriodRange(mode model.PeriodMode, anchor string, now time.Time) (time.Time, time.Time) {
	switch mode {
	case model.PeriodWeekly:
		return WeekRange(now)
	case model.PeriodBiweekly:
		start, _ := WeekRange(now)
		y, m, d := start.Date()
		civilStart := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
		ref := time.Date(1970, 1, 5, 0, 0, 0, 0, time.UTC)
		days := int(civilStart.Sub(ref).Hours() / 24)
		if (days/7)%2 != 0 {
			start = start.AddDate(0, 0, -7)
		}
		return start, start.AddDate(0, 0, 14)
	case model.PeriodSemimonthly:
		y, m, d := now.Date()
		if d <= 15 {
			s := time.Date(y, m, 1, 0, 0, 0, 0, now.Location())
			return s, time.Date(y, m, 16, 0, 0, 0, 0, now.Location())
		}
		s := time.Date(y, m, 16, 0, 0, 0, 0, now.Location())
		return s, time.Date(y, m+1, 1, 0, 0, 0, 0, now.Location())
	case model.PeriodMonthly:
		y, m, _ := now.Date()
		s := time.Date(y, m, 1, 0, 0, 0, 0, now.Location())
		return s, s.AddDate(0, 1, 0)
	default:
		return time.Time{}, now
	}
}
func TodayRange(t time.Time) (time.Time, time.Time) {
	y, m, d := t.Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, t.Location())
	return start, start.AddDate(0, 0, 1)
}
func WeekRange(t time.Time) (time.Time, time.Time) {
	y, m, d := t.Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, t.Location())
	offset := (int(start.Weekday()) + 6) % 7
	start = start.AddDate(0, 0, -offset)
	return start, start.AddDate(0, 0, 7)
}
