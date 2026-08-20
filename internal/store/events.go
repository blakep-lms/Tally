package store

import (
	"database/sql"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/blakep-lms/tally/internal/model"
)

func (s *Store) UpsertEvent(e model.Event) (created bool, err error) {
	return upsertEvent(s.db, e)
}

type eventDB interface {
	Exec(string, ...any) (sql.Result, error)
	QueryRow(string, ...any) *sql.Row
}

func upsertEvent(db eventDB, e model.Event) (created bool, err error) {
	if e.Duration <= 0 || math.IsNaN(e.Duration) || math.IsInf(e.Duration, 0) {
		return false, errors.New("event duration must be finite and greater than zero")
	}
	if e.WorkItemID == nil && e.ProjectID != nil {
		e.WorkItemID = e.ProjectID
	}
	if e.SourceKey != "" {
		var id int64
		row := db.QueryRow(`SELECT id FROM events WHERE source_key = ?`, e.SourceKey)
		switch err := row.Scan(&id); {
		case err == nil:
			_, err := db.Exec(`UPDATE events SET start = ?, duration = ?, app = ?, title = ?, url = ?, repo = ?, source_group = ? WHERE id = ?`, e.Start.UTC(), e.Duration, e.App, e.Title, e.URL, e.Repo, e.SourceGroup, id)
			return false, err
		case errors.Is(err, sql.ErrNoRows):
		default:
			return false, err
		}
	}
	_, err = db.Exec(`INSERT INTO events (start, duration, app, title, url, repo, work_item_id, rule_id, source, source_key, source_group) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, e.Start.UTC(), e.Duration, e.App, e.Title, e.URL, e.Repo, e.WorkItemID, e.RuleID, e.Source, e.SourceKey, e.SourceGroup)
	return err == nil, err
}

type reconciliationEvent struct {
	id         int64
	start      time.Time
	duration   float64
	workItemID sql.NullInt64
	ruleID     sql.NullInt64
	source     string
	sourceKey  string
}

type reconciliationAssignment struct {
	workItemID sql.NullInt64
	ruleID     sql.NullInt64
	source     string
	sourceKey  string
}

// ReplaceEventGroup atomically reconciles every persisted fragment for one
// external capture event. Missing keys are deleted, including when events is empty.
// Classification history is durable, unambiguous assignments are carried to a
// replacement interval, and ambiguous mappings are recorded for manual triage.
func (s *Store) ReplaceEventGroup(group string, events []model.Event) (created, updated, deleted, conflicts int, err error) {
	if group == "" {
		return 0, 0, 0, 0, errors.New("source group is required")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	defer tx.Rollback()
	prior, err := listReconciliationEvents(tx, group)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	keys := make([]string, 0, len(events))
	keySet := make(map[string]bool, len(events))
	for _, e := range events {
		if e.Duration <= 0 || math.IsNaN(e.Duration) || math.IsInf(e.Duration, 0) || e.SourceKey == "" {
			continue
		}
		e.SourceGroup = group
		wasCreated, err := upsertEvent(tx, e)
		if err != nil {
			return 0, 0, 0, 0, err
		}
		if wasCreated {
			created++
		} else {
			updated++
		}
		keys = append(keys, e.SourceKey)
		keySet[e.SourceKey] = true
	}
	targets, err := listReconciliationEvents(tx, group)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	assignments := map[int64][]reconciliationAssignment{}
	for _, old := range prior {
		if keySet[old.sourceKey] || !old.workItemID.Valid {
			continue
		}
		var matches []reconciliationEvent
		for _, target := range targets {
			if keySet[target.sourceKey] && intervalContains(target, old) {
				matches = append(matches, target)
			}
		}
		assignment := reconciliationAssignment{workItemID: old.workItemID, ruleID: old.ruleID, source: old.source, sourceKey: old.sourceKey}
		if len(matches) != 1 {
			if err := insertReconciliationAudit(tx, &old.id, assignment, nil, "reconcile_conflict"); err != nil {
				return 0, 0, 0, 0, err
			}
			conflicts++
			continue
		}
		assignments[matches[0].id] = append(assignments[matches[0].id], assignment)
	}
	for _, target := range targets {
		candidates := assignments[target.id]
		if len(candidates) == 0 {
			continue
		}
		if target.workItemID.Valid {
			candidates = append(candidates, reconciliationAssignment{workItemID: target.workItemID, ruleID: target.ruleID, source: target.source, sourceKey: target.sourceKey})
		}
		chosen, unambiguous := oneAssignment(candidates)
		if !unambiguous {
			if _, err := tx.Exec(`UPDATE events SET work_item_id = NULL, rule_id = NULL, source = '' WHERE id = ?`, target.id); err != nil {
				return 0, 0, 0, 0, err
			}
			for _, candidate := range candidates {
				if err := insertReconciliationAudit(tx, &target.id, candidate, nil, "reconcile_conflict"); err != nil {
					return 0, 0, 0, 0, err
				}
			}
			conflicts++
			continue
		}
		if _, err := tx.Exec(`UPDATE events SET work_item_id = ?, rule_id = ?, source = ? WHERE id = ?`, nullableInt64(chosen.workItemID), nullableInt64(chosen.ruleID), chosen.source, target.id); err != nil {
			return 0, 0, 0, 0, err
		}
		if !target.workItemID.Valid {
			if err := insertReconciliationAudit(tx, &target.id, reconciliationAssignment{sourceKey: target.sourceKey}, &chosen, "reconcile_carry"); err != nil {
				return 0, 0, 0, 0, err
			}
		}
	}
	query := `DELETE FROM events WHERE source_group = ?`
	args := []any{group}
	if len(keys) > 0 {
		query += ` AND source_key NOT IN (` + strings.TrimRight(strings.Repeat("?,", len(keys)), ",") + `)`
		for _, key := range keys {
			args = append(args, key)
		}
	}
	res, err := tx.Exec(query, args...)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	n, _ := res.RowsAffected()
	deleted = int(n)
	if err := tx.Commit(); err != nil {
		return 0, 0, 0, 0, err
	}
	return created, updated, deleted, conflicts, nil
}

func listReconciliationEvents(tx *sql.Tx, group string) ([]reconciliationEvent, error) {
	rows, err := tx.Query(`SELECT id, start, duration, work_item_id, rule_id, source, source_key FROM events WHERE source_group = ?`, group)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []reconciliationEvent
	for rows.Next() {
		var event reconciliationEvent
		if err := rows.Scan(&event.id, &event.start, &event.duration, &event.workItemID, &event.ruleID, &event.source, &event.sourceKey); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func intervalContains(outer, inner reconciliationEvent) bool {
	outerEnd := outer.start.Add(time.Duration(outer.duration * float64(time.Second)))
	innerEnd := inner.start.Add(time.Duration(inner.duration * float64(time.Second)))
	return !inner.start.Before(outer.start) && !innerEnd.After(outerEnd.Add(time.Microsecond))
}

func oneAssignment(candidates []reconciliationAssignment) (reconciliationAssignment, bool) {
	if len(candidates) == 0 {
		return reconciliationAssignment{}, false
	}
	chosen := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.workItemID != chosen.workItemID || candidate.ruleID != chosen.ruleID || candidate.source != chosen.source {
			return reconciliationAssignment{}, false
		}
	}
	return chosen, true
}

func insertReconciliationAudit(tx *sql.Tx, eventID *int64, old reconciliationAssignment, next *reconciliationAssignment, newSource string) error {
	var newWorkItemID, newRuleID any
	if next != nil {
		newWorkItemID = nullableInt64(next.workItemID)
		newRuleID = nullableInt64(next.ruleID)
	}
	_, err := tx.Exec(`INSERT INTO classification_audit (event_id, source_key, old_work_item_id, new_work_item_id, old_rule_id, new_rule_id, old_source, new_source) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, eventID, old.sourceKey, nullableInt64(old.workItemID), newWorkItemID, nullableInt64(old.ruleID), newRuleID, old.source, newSource)
	return err
}

const eventCols = `id, start, duration, app, title, url, repo, work_item_id, rule_id, source, source_key, source_group`

func scanEvent(row interface{ Scan(...any) error }) (model.Event, error) {
	var e model.Event
	var workItemID, ruleID sql.NullInt64
	if err := row.Scan(&e.ID, &e.Start, &e.Duration, &e.App, &e.Title, &e.URL, &e.Repo, &workItemID, &ruleID, &e.Source, &e.SourceKey, &e.SourceGroup); err != nil {
		return model.Event{}, err
	}
	if workItemID.Valid {
		e.WorkItemID = &workItemID.Int64
		e.ProjectID = &workItemID.Int64
	}
	if ruleID.Valid {
		e.RuleID = &ruleID.Int64
	}
	return e, nil
}

type EventFilter struct {
	From        time.Time
	To          time.Time
	Overlap     bool
	UnclassOnly bool
	WorkItemID  *int64
	ProjectID   *int64 // legacy alias
	Limit       int
}

func (s *Store) ListEvents(f EventFilter) ([]model.Event, error) {
	if f.WorkItemID == nil && f.ProjectID != nil {
		f.WorkItemID = f.ProjectID
	}
	q := `SELECT ` + eventCols + ` FROM events WHERE 1=1`
	var args []any
	if !f.From.IsZero() {
		if f.Overlap {
			q += ` AND (julianday(substr(start, 1, 19)) + duration / 86400.0) > julianday(substr(?, 1, 19))`
		} else {
			q += ` AND start >= ?`
		}
		args = append(args, f.From.UTC())
	}
	if !f.To.IsZero() {
		q += ` AND start < ?`
		args = append(args, f.To.UTC())
	}
	if f.UnclassOnly {
		q += ` AND work_item_id IS NULL`
	}
	if f.WorkItemID != nil {
		q += ` AND work_item_id = ?`
		args = append(args, *f.WorkItemID)
	}
	q += ` ORDER BY start DESC`
	if f.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, f.Limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) GetEvent(id int64) (model.Event, error) {
	row := s.db.QueryRow(`SELECT `+eventCols+` FROM events WHERE id = ?`, id)
	event, err := scanEvent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Event{}, ErrNotFound
	}
	return event, err
}

func (s *Store) CountEvents() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&count)
	return count, err
}

func (s *Store) ClassifyEvent(eventID int64, workItemID *int64, ruleID *int64, source string) error {
	if workItemID != nil {
		var status string
		err := s.db.QueryRow(`SELECT status FROM work_items WHERE id = ?`, *workItemID).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if status != string(model.StatusActive) {
			return errors.New("cannot classify to done work item")
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var oldWorkItemID, oldRuleID sql.NullInt64
	var oldSource, sourceKey string
	if err := tx.QueryRow(`SELECT work_item_id, rule_id, source, source_key FROM events WHERE id = ?`, eventID).Scan(&oldWorkItemID, &oldRuleID, &oldSource, &sourceKey); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if _, err := tx.Exec(`UPDATE events SET work_item_id = ?, rule_id = ?, source = ? WHERE id = ?`, workItemID, ruleID, source, eventID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO classification_audit (event_id, source_key, old_work_item_id, new_work_item_id, old_rule_id, new_rule_id, old_source, new_source) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, eventID, sourceKey, nullableInt64(oldWorkItemID), workItemID, nullableInt64(oldRuleID), ruleID, oldSource, source); err != nil {
		return err
	}
	return tx.Commit()
}

func nullableInt64(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

func (s *Store) ListClassificationAudit(eventID int64) ([]model.ClassificationAudit, error) {
	q := `SELECT id, event_id, source_key, old_work_item_id, new_work_item_id, old_rule_id, new_rule_id, old_source, new_source, created_at FROM classification_audit WHERE 1=1`
	var args []any
	if eventID != 0 {
		q += ` AND event_id = ?`
		args = append(args, eventID)
	}
	q += ` ORDER BY id ASC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ClassificationAudit
	for rows.Next() {
		var a model.ClassificationAudit
		var auditEventID, oldWorkItemID, newWorkItemID, oldRuleID, newRuleID sql.NullInt64
		if err := rows.Scan(&a.ID, &auditEventID, &a.SourceKey, &oldWorkItemID, &newWorkItemID, &oldRuleID, &newRuleID, &a.OldSource, &a.NewSource, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.EventID = nullableInt64(auditEventID)
		a.OldWorkItemID = nullableInt64(oldWorkItemID)
		a.NewWorkItemID = nullableInt64(newWorkItemID)
		a.OldRuleID = nullableInt64(oldRuleID)
		a.NewRuleID = nullableInt64(newRuleID)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) ClearRuleClassifications(ruleID int64) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT id, work_item_id, rule_id, source, source_key FROM events WHERE rule_id = ?`, ruleID)
	if err != nil {
		return 0, err
	}
	type prior struct {
		id, workItemID, ruleID sql.NullInt64
		source, sourceKey      string
	}
	var events []prior
	for rows.Next() {
		var p prior
		if err := rows.Scan(&p.id, &p.workItemID, &p.ruleID, &p.source, &p.sourceKey); err != nil {
			rows.Close()
			return 0, err
		}
		events = append(events, p)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, event := range events {
		if _, err := tx.Exec(`UPDATE events SET work_item_id = NULL, rule_id = NULL, source = 'rule_clear' WHERE id = ?`, event.id.Int64); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(`INSERT INTO classification_audit (event_id, source_key, old_work_item_id, new_work_item_id, old_rule_id, new_rule_id, old_source, new_source) VALUES (?, ?, ?, NULL, ?, NULL, ?, 'rule_clear')`, event.id.Int64, event.sourceKey, nullableInt64(event.workItemID), nullableInt64(event.ruleID), event.source); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(events)), nil
}

func (s *Store) ReportWindow(from, to time.Time) ([]model.WorkItemHours, float64, float64, error) {
	items, err := s.ListWorkItems("")
	if err != nil {
		return nil, 0, 0, err
	}
	byID := make(map[int64]model.WorkItem, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	events, err := s.ListEvents(EventFilter{From: from, To: to, Overlap: true})
	if err != nil {
		return nil, 0, 0, err
	}
	seconds := map[int64]float64{}
	var unclassified, total float64
	for _, event := range events {
		overlap := overlapSeconds(event, from, to)
		total += overlap
		if event.WorkItemID != nil {
			seconds[*event.WorkItemID] += overlap
		} else {
			unclassified += overlap
		}
	}
	out := make([]model.WorkItemHours, 0, len(seconds))
	for id, secs := range seconds {
		if secs <= 0 {
			continue
		}
		if item, ok := byID[id]; ok {
			out = append(out, model.WorkItemHours{WorkItem: item, Seconds: secs, Hours: secs / 3600})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Seconds == out[j].Seconds {
			return out[i].WorkItem.Name < out[j].WorkItem.Name
		}
		return out[i].Seconds > out[j].Seconds
	})
	return out, unclassified, total, nil
}

func (s *Store) HoursByWorkItem(from, to time.Time) ([]model.WorkItemHours, error) {
	hours, _, _, err := s.ReportWindow(from, to)
	return hours, err
}

func (s *Store) HoursByProject(from, to time.Time) ([]model.ProjectHours, error) {
	items, err := s.HoursByWorkItem(from, to)
	if err != nil {
		return nil, err
	}
	out := make([]model.ProjectHours, 0, len(items))
	for _, wh := range items {
		if wh.WorkItem.Kind == model.KindProject {
			out = append(out, model.ProjectHours{Project: model.ProjectFromWorkItem(wh.WorkItem, s.projectType(wh.WorkItem.ID)), Seconds: wh.Seconds, Hours: wh.Hours})
		}
	}
	return out, nil
}

func (s *Store) UnclassifiedSeconds(from, to time.Time) (float64, error) {
	return s.sumEventSeconds(from, to, true)
}
func (s *Store) TotalSeconds(from, to time.Time) (float64, error) {
	return s.sumEventSeconds(from, to, false)
}

func (s *Store) sumEventSeconds(from, to time.Time, unclassifiedOnly bool) (float64, error) {
	_, unclassified, total, err := s.ReportWindow(from, to)
	if err != nil {
		return 0, err
	}
	if unclassifiedOnly {
		return unclassified, nil
	}
	return total, nil
}

func overlapSeconds(event model.Event, from, to time.Time) float64 {
	start := event.Start
	end := event.Start.Add(time.Duration(math.Round(event.Duration * float64(time.Second))))
	if !from.IsZero() && start.Before(from) {
		start = from
	}
	if !to.IsZero() && end.After(to) {
		end = to
	}
	if !end.After(start) {
		return 0
	}
	return end.Sub(start).Seconds()
}

func prefixCols(p string) string {
	return p + ".id, " + p + ".name, " + p + ".kind, " + p + ".context, " + p + ".description, " + p + ".status, " + p + ".created_at, " + p + ".done_at"
}
