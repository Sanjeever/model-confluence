package store

import "context"

type LiveRoutingNode struct {
	Kind string `json:"kind"`
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func (s *Store) LiveRoutingCatalog(ctx context.Context) ([]LiveRoutingNode, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT 'access_key' AS kind, id, name FROM access_keys WHERE enabled = 1 AND archived_at IS NULL
UNION ALL
SELECT 'virtual_model', id, name FROM virtual_models WHERE enabled = 1 AND archived_at IS NULL
UNION ALL
SELECT 'provider', id, name FROM providers WHERE enabled = 1 AND archived_at IS NULL
ORDER BY kind, name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := make([]LiveRoutingNode, 0)
	for rows.Next() {
		var node LiveRoutingNode
		if err := rows.Scan(&node.Kind, &node.ID, &node.Name); err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func (s *Store) ExistingRequestIDs(ctx context.Context, ids []string) (map[string]bool, error) {
	existing := make(map[string]bool, len(ids))
	if len(ids) == 0 {
		return existing, nil
	}
	names, args := idQuery(ids)
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM requests WHERE id IN (`+names+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		existing[id] = true
	}
	return existing, rows.Err()
}
