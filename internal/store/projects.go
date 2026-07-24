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

// CreateProject inserts a new active project and returns it with its id set.
func (s *Store) CreateProject(name string, typ model.ProjectType, client string) (model.Project, error) {
	if name == "" {
		return model.Project{}, errors.New("project name is required")
	}
	if !typ.Valid() {
		return model.Project{}, fmt.Errorf("invalid project type %q", typ)
	}
	res, err := s.db.Exec(
		`INSERT INTO projects (name, type, client, status) VALUES (?, ?, ?, 'active')`,
		name, string(typ), client,
	)
	if err != nil {
		return model.Project{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetProject(id)
}

func scanProject(row interface{ Scan(...any) error }) (model.Project, error) {
	var p model.Project
	var typ, status string
	var doneAt sql.NullTime
	if err := row.Scan(&p.ID, &p.Name, &typ, &p.Client, &status, &p.CreatedAt, &doneAt); err != nil {
		return model.Project{}, err
	}
	p.Type = model.ProjectType(typ)
	p.Status = model.ProjectStatus(status)
	if doneAt.Valid {
		p.DoneAt = &doneAt.Time
	}
	return p, nil
}

const projectCols = `id, name, type, client, status, created_at, done_at`

// GetProject fetches a project by id.
func (s *Store) GetProject(id int64) (model.Project, error) {
	row := s.db.QueryRow(`SELECT `+projectCols+` FROM projects WHERE id = ?`, id)
	p, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Project{}, ErrNotFound
	}
	return p, err
}

// GetProjectByName fetches a project by its unique name.
func (s *Store) GetProjectByName(name string) (model.Project, error) {
	row := s.db.QueryRow(`SELECT `+projectCols+` FROM projects WHERE name = ?`, name)
	p, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Project{}, ErrNotFound
	}
	return p, err
}

// ListProjects returns all projects, optionally filtered by status. Pass an
// empty status to return every project. Active projects sort before done.
func (s *Store) ListProjects(status model.ProjectStatus) ([]model.Project, error) {
	q := `SELECT ` + projectCols + ` FROM projects`
	var args []any
	if status != "" {
		q += ` WHERE status = ?`
		args = append(args, string(status))
	}
	q += ` ORDER BY (status = 'active') DESC, name ASC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdateProject writes name, type, and client for an existing project.
func (s *Store) UpdateProject(id int64, name string, typ model.ProjectType, client string) (model.Project, error) {
	if !typ.Valid() {
		return model.Project{}, fmt.Errorf("invalid project type %q", typ)
	}
	res, err := s.db.Exec(
		`UPDATE projects SET name = ?, type = ?, client = ? WHERE id = ?`,
		name, string(typ), client, id,
	)
	if err != nil {
		return model.Project{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return model.Project{}, ErrNotFound
	}
	return s.GetProject(id)
}

// MarkDone archives a project: status becomes done, done_at is stamped, and
// its rules are deactivated so no new time is attributed to it.
func (s *Store) MarkDone(id int64) (model.Project, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return model.Project{}, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(
		`UPDATE projects SET status = 'done', done_at = ? WHERE id = ? AND status = 'active'`,
		time.Now().UTC(), id,
	)
	if err != nil {
		return model.Project{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Either it does not exist or it is already done.
		if _, err := s.GetProject(id); err != nil {
			return model.Project{}, err
		}
	}
	if _, err := tx.Exec(`UPDATE rules SET active = 0 WHERE project_id = ?`, id); err != nil {
		return model.Project{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Project{}, err
	}
	return s.GetProject(id)
}

// Reactivate flips a done project back to active without re-enabling rules.
func (s *Store) Reactivate(id int64) (model.Project, error) {
	res, err := s.db.Exec(
		`UPDATE projects SET status = 'active', done_at = NULL WHERE id = ?`, id,
	)
	if err != nil {
		return model.Project{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return model.Project{}, ErrNotFound
	}
	return s.GetProject(id)
}
