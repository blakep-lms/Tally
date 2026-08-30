package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/blakep-lms/tally/internal/model"
)

// ErrNotFound is returned when a lookup by id or name matches nothing.
var ErrNotFound = errors.New("not found")

const workItemCols = `id, name, kind, context, description, status, created_at, done_at`

// CreateWorkItem inserts a new active work item.
func (s *Store) CreateWorkItem(name string, kind model.WorkItemKind, context, description string) (model.WorkItem, error) {
	if name == "" {
		return model.WorkItem{}, errors.New("work item name is required")
	}
	if !kind.Valid() {
		return model.WorkItem{}, fmt.Errorf("invalid work item kind %q", kind)
	}
	res, err := s.db.Exec(
		`INSERT INTO work_items (name, kind, context, description, status) VALUES (?, ?, ?, ?, 'active')`,
		name, string(kind), context, description,
	)
	if err != nil {
		return model.WorkItem{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetWorkItem(id)
}

func scanWorkItem(row interface{ Scan(...any) error }) (model.WorkItem, error) {
	var w model.WorkItem
	var kind, status string
	var doneAt sql.NullTime
	if err := row.Scan(&w.ID, &w.Name, &kind, &w.Context, &w.Description, &status, &w.CreatedAt, &doneAt); err != nil {
		return model.WorkItem{}, err
	}
	w.Kind = model.WorkItemKind(kind)
	w.Status = model.WorkItemStatus(status)
	if doneAt.Valid {
		w.DoneAt = &doneAt.Time
	}
	return w, nil
}

func (s *Store) GetWorkItem(id int64) (model.WorkItem, error) {
	row := s.db.QueryRow(`SELECT `+workItemCols+` FROM work_items WHERE id = ?`, id)
	w, err := scanWorkItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.WorkItem{}, ErrNotFound
	}
	return w, err
}

func (s *Store) GetWorkItemByName(name string) (model.WorkItem, error) {
	row := s.db.QueryRow(`SELECT `+workItemCols+` FROM work_items WHERE name = ?`, name)
	w, err := scanWorkItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.WorkItem{}, ErrNotFound
	}
	return w, err
}

func (s *Store) ListWorkItems(status model.WorkItemStatus) ([]model.WorkItem, error) {
	q := `SELECT ` + workItemCols + ` FROM work_items`
	var args []any
	if status != "" {
		if !status.Valid() {
			return nil, fmt.Errorf("invalid work item status %q", status)
		}
		q += ` WHERE status = ?`
		args = append(args, string(status))
	}
	q += ` ORDER BY (status = 'active') DESC, name ASC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.WorkItem
	for rows.Next() {
		w, err := scanWorkItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) UpdateWorkItem(id int64, name string, kind model.WorkItemKind, context, description string) (model.WorkItem, error) {
	if name == "" {
		return model.WorkItem{}, errors.New("work item name is required")
	}
	if !kind.Valid() {
		return model.WorkItem{}, fmt.Errorf("invalid work item kind %q", kind)
	}
	res, err := s.db.Exec(`UPDATE work_items SET name = ?, kind = ?, context = ?, description = ? WHERE id = ?`, name, string(kind), context, description, id)
	if err != nil {
		return model.WorkItem{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return model.WorkItem{}, ErrNotFound
	}
	return s.GetWorkItem(id)
}

// MarkWorkItemDone is idempotent. All existence checks stay inside tx so stores
// with MaxOpenConns(1) cannot self-deadlock while a transaction is open.
func (s *Store) MarkWorkItemDone(id int64) (model.WorkItem, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return model.WorkItem{}, err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	if _, err := tx.Exec(`UPDATE work_items SET status = 'done', done_at = COALESCE(done_at, ?) WHERE id = ? AND status = 'active'`, now, id); err != nil {
		return model.WorkItem{}, err
	}
	if _, err := tx.Exec(`UPDATE rules SET active = 0 WHERE work_item_id = ?`, id); err != nil {
		return model.WorkItem{}, err
	}
	row := tx.QueryRow(`SELECT `+workItemCols+` FROM work_items WHERE id = ?`, id)
	w, err := scanWorkItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.WorkItem{}, ErrNotFound
	}
	if err != nil {
		return model.WorkItem{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.WorkItem{}, err
	}
	return w, nil
}

func (s *Store) ReactivateWorkItem(id int64) (model.WorkItem, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return model.WorkItem{}, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE work_items SET status = 'active', done_at = NULL WHERE id = ?`, id)
	if err != nil {
		return model.WorkItem{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return model.WorkItem{}, ErrNotFound
	}
	if _, err := tx.Exec(`UPDATE rules SET active = 1 WHERE work_item_id = ?`, id); err != nil {
		return model.WorkItem{}, err
	}
	row := tx.QueryRow(`SELECT `+workItemCols+` FROM work_items WHERE id = ?`, id)
	w, err := scanWorkItem(row)
	if err != nil {
		return model.WorkItem{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.WorkItem{}, err
	}
	return w, nil
}

// Legacy project wrappers.
func (s *Store) CreateProject(name string, typ model.ProjectType, client string) (model.Project, error) {
	if !typ.Valid() {
		return model.Project{}, fmt.Errorf("invalid project type %q", typ)
	}
	w, err := s.CreateWorkItem(name, model.KindProject, client, "")
	if err != nil {
		return model.Project{}, err
	}
	_, err = s.SetBillingProfile(model.BillingProfile{ScopeType: model.BillingScopeWorkItem, ScopeKey: fmt.Sprint(w.ID), Enabled: typ == model.TypeBillable, Currency: "USD", RoundingMode: model.RoundingUp, RoundingIncrementMinutes: 15, RoundingScope: model.RoundingScopePeriodWorkItem, PeriodMode: model.PeriodCustom, LegacyType: typ})
	if err != nil {
		return model.Project{}, err
	}
	return model.ProjectFromWorkItem(w, typ), nil
}

func (s *Store) projectType(id int64) model.ProjectType {
	var typ string
	_ = s.db.QueryRow(`SELECT COALESCE(legacy_type, '') FROM billing_profiles WHERE scope_type = 'work_item' AND scope_key = ?`, fmt.Sprint(id)).Scan(&typ)
	return model.ProjectType(typ)
}

func (s *Store) GetProject(id int64) (model.Project, error) {
	w, err := s.GetWorkItem(id)
	if err != nil {
		return model.Project{}, err
	}
	return model.ProjectFromWorkItem(w, s.projectType(id)), nil
}

func (s *Store) GetProjectByName(name string) (model.Project, error) {
	w, err := s.GetWorkItemByName(name)
	if err != nil {
		return model.Project{}, err
	}
	return model.ProjectFromWorkItem(w, s.projectType(w.ID)), nil
}

func (s *Store) ListProjects(status model.ProjectStatus) ([]model.Project, error) {
	items, err := s.ListWorkItems(status)
	if err != nil {
		return nil, err
	}
	out := make([]model.Project, 0, len(items))
	for _, w := range items {
		if w.Kind == model.KindProject {
			out = append(out, model.ProjectFromWorkItem(w, s.projectType(w.ID)))
		}
	}
	return out, nil
}

func (s *Store) UpdateProject(id int64, name string, typ model.ProjectType, client string) (model.Project, error) {
	if !typ.Valid() {
		return model.Project{}, fmt.Errorf("invalid project type %q", typ)
	}
	w, err := s.UpdateWorkItem(id, name, model.KindProject, client, "")
	if err != nil {
		return model.Project{}, err
	}
	_, err = s.SetBillingProfile(model.BillingProfile{ScopeType: model.BillingScopeWorkItem, ScopeKey: fmt.Sprint(id), Enabled: typ == model.TypeBillable, Currency: "USD", RoundingMode: model.RoundingUp, RoundingIncrementMinutes: 15, RoundingScope: model.RoundingScopePeriodWorkItem, PeriodMode: model.PeriodCustom, LegacyType: typ})
	if err != nil {
		return model.Project{}, err
	}
	return model.ProjectFromWorkItem(w, typ), nil
}

func (s *Store) MarkDone(id int64) (model.Project, error) {
	w, err := s.MarkWorkItemDone(id)
	if err != nil {
		return model.Project{}, err
	}
	return model.ProjectFromWorkItem(w, s.projectType(id)), nil
}

func (s *Store) Reactivate(id int64) (model.Project, error) {
	w, err := s.ReactivateWorkItem(id)
	if err != nil {
		return model.Project{}, err
	}
	return model.ProjectFromWorkItem(w, s.projectType(id)), nil
}
