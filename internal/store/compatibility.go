package store

import "encoding/json"

func (s *Store) SaveCompatibilityCheck(modelID int64, result any) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO compatibility_checks(model_id, result_json) VALUES (?, ?)`, modelID, string(data))
	return err
}

func (s *Store) CompatibilityHistory(modelID int64) ([]json.RawMessage, error) {
	rows, err := s.db.Query(`SELECT result_json FROM compatibility_checks WHERE model_id = ? ORDER BY id DESC LIMIT 20`, modelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []json.RawMessage{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		result = append(result, json.RawMessage(raw))
	}
	return result, rows.Err()
}
