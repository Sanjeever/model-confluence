package store

import (
	"database/sql"
	"encoding/json"
)

type RoutingEvent struct {
	CandidateID int64  `json:"candidate_id"`
	KeyID       int64  `json:"key_id"`
	Action      string `json:"action"`
}

func (s *Store) SuccessfulRequestRoute(id string) (int64, int64, error) {
	var candidateID, keyID int64
	err := s.db.QueryRow(`SELECT a.candidate_id, a.upstream_key_id FROM attempts a JOIN requests r ON r.id = a.request_id WHERE r.id = ? AND r.status = 'completed' AND a.status = 'completed' ORDER BY a.position DESC LIMIT 1`, id).Scan(&candidateID, &keyID)
	return candidateID, keyID, err
}

func (s *Store) SaveRequestRouting(id, replayOf string, decisions []RouteDecision) error {
	if decisions == nil {
		decisions = []RouteDecision{}
	}
	data, err := json.Marshal(decisions)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO request_routing(request_id, replay_of, decisions_json) VALUES (?, ?, ?)`, id, replayOf, string(data))
	return err
}

func (s *Store) AddRoutingEvent(id string, event RoutingEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE request_routing SET events_json = json_insert(events_json, '$[#]', json(?)) WHERE request_id = ?`, string(data), id)
	return err
}

func (s *Store) requestRouting(detail *RequestDetail) error {
	var decisions, events string
	err := s.db.QueryRow(`SELECT replay_of, decisions_json, events_json FROM request_routing WHERE request_id = ?`, detail.ID).Scan(&detail.ReplayOf, &decisions, &events)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(decisions), &detail.RouteDecisions); err != nil {
		return err
	}
	return json.Unmarshal([]byte(events), &detail.RoutingEvents)
}
