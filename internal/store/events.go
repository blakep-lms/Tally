package store

import (
	"database/sql"
	"errors"
	"time"

	"github.com/blakep-lms/tally/internal/model"
)

// UpsertEvent inserts an event, or updates the mutable capture fields when an
// event with the same non-empty source_key already exists. It returns whether
// a new row was created. Classification fields are never clobbered on update.
func (s *Store) UpsertEvent(e model.Event) (created bool, err error) {
	if e.SourceKey != "" {
		var id int64
		row := s.db.QueryRow(`SELECT id FROM events WHERE source_key = ?`, e.SourceKey)
		switch err := row.Scan(&id); {
		case err == nil:
			_, err := s.db.Exec(
				`UPDATE events SET start = ?, duration = ?, app = ?, title = ?, url = ?, repo = ? WHERE id = ?`,
				e.Start.UTC(), e.Duration, e.App, e.Title, e.URL, e.Repo, id,
			)
			return false, err
		case errors.Is(err, sql.ErrNoRows):
			// fall through to insert
		default:
			return false, err
		}
	}
	_, err = s.db.Exec(
		`INSERT INTO events (start, duration, app, title, url, repo, project_id, rule_id, source, source_key)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Start.UTC(), e.Duration, e.App, e.Title, e.URL, e.Repo,
		e.ProjectID, e.RuleID, e.Source, e.SourceKey,
	)
	return err == nil, err
}

const eventCols = `id, start, duration, app, title, url, repo, project_id, rule_id, source, source_key`

func scanEvent(row interface{ Scan(...any) error }) (model.Event, error) {
	var e model.Event
	var projectID, ruleID sql.NullInt64
	if err := row.Scan(&e.ID, &e.Start, &e.Duration, &e.App, &e.Title, &e.URL, &e.Repo,
		&projectID, &ruleID, &e.Source, &e.SourceKey); err != nil {
		return model.Event{}, err
	}
	if projectID.Valid {
		e.ProjectID = &projectID.Int64
	}
	if ruleID.Valid {
		e.RuleID = &ruleID.Int64
	}
	return e, nil
}

// EventFilter narrows event queries.
type EventFilter struct {
	From        time.Time
	To          time.Time
	UnclassOnly bool
	ProjectID   *int64
	Limit       int
}

// ListEvents returns events matching the filter, newest first.
func (s *Store) ListEvents(f EventFilter) ([]model.Event, error) {
	q := `SELECT ` + eventCols + ` FROM events WHERE 1=1`
	var args []any
	if !f.From.IsZero() {
		q += ` AND start >= ?`
		args = append(args, f.From.UTC())
	}
	if !f.To.IsZero() {
		q += ` AND start < ?`
		args = append(args, f.To.UTC())
	}
	if f.UnclassOnly {
		q += ` AND project_id IS NULL`
	}
	if f.ProjectID != nil {
		q += ` AND project_id = ?`
		args = append(args, *f.ProjectID)
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

// ClassifyEvent attributes an event to a project (or clears it when projectID
// is nil) recording the source and matched rule.
func (s *Store) ClassifyEvent(eventID int64, projectID *int64, ruleID *int64, source string) error {
	res, err := s.db.Exec(
		`UPDATE events SET project_id = ?, rule_id = ?, source = ? WHERE id = ?`,
		projectID, ruleID, source, eventID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ClearRuleClassifications un-classifies every event that was classified by
// the given rule, returning them to the unclassified queue. Used when a
// project is marked done and its rules should stop attracting time going
// forward, while leaving history intact — callers decide whether to invoke it.
func (s *Store) ClearRuleClassifications(ruleID int64) (int64, error) {
	res, err := s.db.Exec(
		`UPDATE events SET project_id = NULL, rule_id = NULL, source = '' WHERE rule_id = ?`,
		ruleID,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// HoursByProject aggregates classified seconds per project within [from, to).
// Zero-valued bounds are treated as open. Projects with no time are omitted.
func (s *Store) HoursByProject(from, to time.Time) ([]model.ProjectHours, error) {
	q := `
SELECT ` + prefixCols("p") + `, COALESCE(SUM(e.duration), 0) AS secs
FROM projects p
JOIN events e ON e.project_id = p.id
WHERE 1=1`
	var args []any
	if !from.IsZero() {
		q += ` AND e.start >= ?`
		args = append(args, from.UTC())
	}
	if !to.IsZero() {
		q += ` AND e.start < ?`
		args = append(args, to.UTC())
	}
	q += ` GROUP BY p.id HAVING secs > 0 ORDER BY secs DESC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ProjectHours
	for rows.Next() {
		var ph model.ProjectHours
		var typ, status string
		var doneAt sql.NullTime
		if err := rows.Scan(
			&ph.Project.ID, &ph.Project.Name, &typ, &ph.Project.Client,
			&status, &ph.Project.CreatedAt, &doneAt, &ph.Seconds,
		); err != nil {
			return nil, err
		}
		ph.Project.Type = model.ProjectType(typ)
		ph.Project.Status = model.ProjectStatus(status)
		if doneAt.Valid {
			ph.Project.DoneAt = &doneAt.Time
		}
		ph.Hours = ph.Seconds / 3600
		out = append(out, ph)
	}
	return out, rows.Err()
}

// UnclassifiedSeconds returns total unclassified seconds in [from, to).
func (s *Store) UnclassifiedSeconds(from, to time.Time) (float64, error) {
	q := `SELECT COALESCE(SUM(duration), 0) FROM events WHERE project_id IS NULL`
	var args []any
	if !from.IsZero() {
		q += ` AND start >= ?`
		args = append(args, from.UTC())
	}
	if !to.IsZero() {
		q += ` AND start < ?`
		args = append(args, to.UTC())
	}
	var secs float64
	err := s.db.QueryRow(q, args...).Scan(&secs)
	return secs, err
}

// TotalSeconds returns all captured seconds in [from, to).
func (s *Store) TotalSeconds(from, to time.Time) (float64, error) {
	q := `SELECT COALESCE(SUM(duration), 0) FROM events WHERE 1=1`
	var args []any
	if !from.IsZero() {
		q += ` AND start >= ?`
		args = append(args, from.UTC())
	}
	if !to.IsZero() {
		q += ` AND start < ?`
		args = append(args, to.UTC())
	}
	var secs float64
	err := s.db.QueryRow(q, args...).Scan(&secs)
	return secs, err
}

func prefixCols(p string) string {
	return p + ".id, " + p + ".name, " + p + ".type, " + p + ".client, " +
		p + ".status, " + p + ".created_at, " + p + ".done_at"
}
