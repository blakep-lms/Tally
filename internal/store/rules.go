package store

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"

	"github.com/blakep-lms/tally/internal/model"
)

// CreateRule inserts a rule. A regex pattern is validated before insert.
func (s *Store) CreateRule(r model.Rule) (model.Rule, error) {
	if !r.Field.Valid() {
		return model.Rule{}, fmt.Errorf("invalid rule field %q", r.Field)
	}
	if !r.Match.Valid() {
		return model.Rule{}, fmt.Errorf("invalid match kind %q", r.Match)
	}
	if r.Pattern == "" {
		return model.Rule{}, errors.New("rule pattern is required")
	}
	if r.Match == model.MatchRegex {
		if _, err := regexp.Compile(r.Pattern); err != nil {
			return model.Rule{}, fmt.Errorf("invalid regex: %w", err)
		}
	}
	if r.Priority == 0 {
		r.Priority = 100
	}
	if _, err := s.GetProject(r.ProjectID); err != nil {
		return model.Rule{}, fmt.Errorf("project %d: %w", r.ProjectID, err)
	}
	res, err := s.db.Exec(
		`INSERT INTO rules (project_id, field, match, pattern, priority, active)
		 VALUES (?, ?, ?, ?, ?, 1)`,
		r.ProjectID, string(r.Field), string(r.Match), r.Pattern, r.Priority,
	)
	if err != nil {
		return model.Rule{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetRule(id)
}

const ruleCols = `id, project_id, field, match, pattern, priority, active, created_at`

func scanRule(row interface{ Scan(...any) error }) (model.Rule, error) {
	var r model.Rule
	var field, match string
	var active int
	if err := row.Scan(&r.ID, &r.ProjectID, &field, &match, &r.Pattern, &r.Priority, &active, &r.CreatedAt); err != nil {
		return model.Rule{}, err
	}
	r.Field = model.RuleField(field)
	r.Match = model.MatchKind(match)
	r.Active = active != 0
	return r, nil
}

// GetRule fetches a rule by id.
func (s *Store) GetRule(id int64) (model.Rule, error) {
	row := s.db.QueryRow(`SELECT `+ruleCols+` FROM rules WHERE id = ?`, id)
	r, err := scanRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Rule{}, ErrNotFound
	}
	return r, err
}

// ListRules returns rules. When activeOnly is true, only active rules of
// active projects are returned, ordered by priority then id (match order).
func (s *Store) ListRules(activeOnly bool) ([]model.Rule, error) {
	q := `SELECT ` + ruleCols + ` FROM rules`
	if activeOnly {
		q += ` WHERE active = 1 AND project_id IN (SELECT id FROM projects WHERE status = 'active')`
	}
	q += ` ORDER BY priority ASC, id ASC`
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Rule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteRule removes a rule by id.
func (s *Store) DeleteRule(id int64) error {
	res, err := s.db.Exec(`DELETE FROM rules WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
