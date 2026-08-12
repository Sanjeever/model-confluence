package store

import "time"

type Overview struct {
	AccessKeys    int `json:"access_keys"`
	Providers     int `json:"providers"`
	VirtualModels int `json:"virtual_models"`
	RequestCount  int `json:"request_count"`
}

func (s *Store) Overview(createdFrom, createdTo time.Time) (Overview, error) {
	var result Overview
	err := s.db.QueryRow(`
SELECT
  (SELECT COUNT(*) FROM access_keys WHERE archived_at IS NULL),
  (SELECT COUNT(*) FROM providers WHERE archived_at IS NULL),
  (SELECT COUNT(*) FROM virtual_models WHERE archived_at IS NULL),
  (SELECT COUNT(*) FROM requests WHERE created_at >= ? AND created_at < ?)
`).Scan(&result.AccessKeys, &result.Providers, &result.VirtualModels, &result.RequestCount, formatTime(createdFrom), formatTime(createdTo))
	return result, err
}
