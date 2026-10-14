package codex

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Sanjeever/model-confluence/internal/protocol"
)

func TestStreamReconstructsOutput(t *testing.T) {
	var s Stream
	for _, data := range []string{
		`{"type":"response.created","response":{"id":"resp_test","model":"real"}}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","call_id":"call_test","name":"lookup","arguments":"{}"}}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}}`,
		`{"type":"response.completed","response":{"id":"resp_test","model":"real","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}`,
	} {
		if _, _, err := s.Consume(protocol.SSEEvent{Data: []byte(data)}, "virtual"); err != nil {
			t.Fatal(err)
		}
	}
	if !s.Done || !strings.Contains(string(s.Response), `"model":"virtual"`) {
		t.Fatal("not completed", string(s.Response))
	}
	var response struct {
		Output []struct {
			Type string `json:"type"`
		} `json:"output"`
	}
	if err := json.Unmarshal(s.Response, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Output) != 2 || response.Output[0].Type != "message" {
		t.Fatalf("bad output order: %s", s.Response)
	}
}

func TestStreamTerminalSemantics(t *testing.T) {
	for _, tc := range []struct {
		body    string
		success bool
		status  int
	}{
		{`{"type":"response.failed","response":{"status":"failed","error":{"code":"rate_limit_exceeded","message":"slow down"}}}`, false, 429},
		{`{"type":"error","code":"unauthorized","message":"expired"}`, false, 401},
		{`{"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`, true, 0},
		{`{"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"server_error"}}}`, false, 0},
		{`{"type":"response.completed","response":{"status":"failed"}}`, false, 0},
		{`[DONE]`, false, 0},
	} {
		var s Stream
		_, _, err := s.Consume(protocol.SSEEvent{Data: []byte(tc.body)}, "virtual")
		if (err == nil) != tc.success {
			t.Fatalf("terminal error: %s %v", tc.body, err)
		}
		if tc.status != 0 {
			var failure *UpstreamError
			if !errors.As(err, &failure) || failure.Status != tc.status {
				t.Fatalf("classification: %v", err)
			}
		}
	}
	for _, body := range []string{`{"error":{"type":"usage_limit_reached","resets_at":2000000000}}`, `{"error":{"type":"usage_limit_reached","resets_in_seconds":60}}`} {
		if e := ClassifyError([]byte(body), 429, time.Now()); e.RecoverAt == nil || e.Code != "usage_limit_reached" {
			t.Fatalf("missing reset: %+v", e)
		}
	}
}
