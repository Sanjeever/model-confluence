package store

type Overview struct {
	AccessKeys    int `json:"access_keys"`
	Providers     int `json:"providers"`
	VirtualModels int `json:"virtual_models"`
}

func (s *Store) Overview() (Overview, error) {
	var result Overview
	err := s.db.QueryRow(`
SELECT
  (SELECT COUNT(*) FROM access_keys WHERE archived_at IS NULL),
  (SELECT COUNT(*) FROM providers WHERE archived_at IS NULL),
  (SELECT COUNT(*) FROM virtual_models WHERE archived_at IS NULL)
`).Scan(&result.AccessKeys, &result.Providers, &result.VirtualModels)
	return result, err
}
