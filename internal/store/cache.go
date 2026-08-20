package store

import (
	"database/sql"
	"errors"
)

func (s *Store) LLMCacheGet(signature string) (workItemID int64, ok bool, err error) {
	var pid sql.NullInt64
	var active int
	row := s.db.QueryRow(`
SELECT c.work_item_id,
       CASE WHEN c.work_item_id IS NULL OR w.status = 'active' THEN 1 ELSE 0 END
FROM llm_cache c
LEFT JOIN work_items w ON w.id = c.work_item_id
WHERE c.signature = ?`, signature)
	switch err := row.Scan(&pid, &active); {
	case errors.Is(err, sql.ErrNoRows):
		return 0, false, nil
	case err != nil:
		return 0, false, err
	}
	if active == 0 {
		_, _ = s.db.Exec(`DELETE FROM llm_cache WHERE signature = ?`, signature)
		return 0, false, nil
	}
	if !pid.Valid {
		return 0, true, nil
	}
	return pid.Int64, true, nil
}

func (s *Store) LLMCachePut(signature string, workItemID *int64) error {
	_, err := s.db.Exec(`INSERT INTO llm_cache (signature, work_item_id) VALUES (?, ?) ON CONFLICT(signature) DO UPDATE SET work_item_id = excluded.work_item_id`, signature, workItemID)
	return err
}
