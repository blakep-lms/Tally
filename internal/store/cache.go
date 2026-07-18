package store

import (
	"database/sql"
	"errors"
)

// LLMCacheGet returns the cached project id for a signal signature, if any.
func (s *Store) LLMCacheGet(signature string) (projectID int64, ok bool, err error) {
	var pid sql.NullInt64
	row := s.db.QueryRow(`SELECT project_id FROM llm_cache WHERE signature = ?`, signature)
	switch err := row.Scan(&pid); {
	case errors.Is(err, sql.ErrNoRows):
		return 0, false, nil
	case err != nil:
		return 0, false, err
	}
	if !pid.Valid {
		return 0, true, nil // cached "no match"
	}
	return pid.Int64, true, nil
}

// LLMCachePut records an LLM decision. A nil projectID caches a "no match".
func (s *Store) LLMCachePut(signature string, projectID *int64) error {
	_, err := s.db.Exec(
		`INSERT INTO llm_cache (signature, project_id) VALUES (?, ?)
		 ON CONFLICT(signature) DO UPDATE SET project_id = excluded.project_id`,
		signature, projectID,
	)
	return err
}
