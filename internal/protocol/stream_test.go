package protocol

import (
	"bytes"
	"testing"
)

func TestMessagesToChatPreservesThinking(t *testing.T) {
	converter, err := NewStreamConverter(Messages, Chat, "virtual-model")
	if err != nil {
		t.Fatal(err)
	}

	events := []SSEEvent{
		{Name: "message_start", Data: []byte(`{"type":"message_start","message":{"id":"msg_test","model":"upstream-model","usage":{"input_tokens":5,"output_tokens":1}}}`)},
		{Name: "content_block_start", Data: []byte(`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`)},
		{Name: "content_block_delta", Data: []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"分析中"}}`)},
		{Name: "content_block_delta", Data: []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"opaque"}}`)},
		{Name: "content_block_stop", Data: []byte(`{"type":"content_block_stop","index":0}`)},
		{Name: "content_block_start", Data: []byte(`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`)},
		{Name: "content_block_delta", Data: []byte(`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"我是模型"}}`)},
		{Name: "content_block_stop", Data: []byte(`{"type":"content_block_stop","index":1}`)},
		{Name: "message_delta", Data: []byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":8}}`)},
		{Name: "message_stop", Data: []byte(`{"type":"message_stop"}`)},
	}

	var output bytes.Buffer
	done := false
	for index, event := range events {
		chunks, _, eventDone, convertErr := converter.Convert(event)
		if convertErr != nil {
			t.Fatalf("event %d: %v", index, convertErr)
		}
		if index < 2 && len(chunks) != 0 {
			t.Fatalf("thinking event %d unexpectedly produced output", index)
		}
		for _, chunk := range chunks {
			output.Write(chunk)
		}
		done = done || eventDone
	}

	if !done {
		t.Fatal("stream did not complete")
	}
	if !bytes.Contains(output.Bytes(), []byte(`"content":"我是模型"`)) {
		t.Fatalf("converted stream does not contain text: %s", output.Bytes())
	}
	if !bytes.Contains(output.Bytes(), []byte(`"reasoning_content":"分析中"`)) {
		t.Fatalf("converted stream does not contain reasoning: %s", output.Bytes())
	}
	if bytes.Contains(output.Bytes(), []byte("opaque")) {
		t.Fatalf("converted stream leaked thinking signature: %s", output.Bytes())
	}
	if !bytes.Contains(output.Bytes(), []byte("data: [DONE]")) {
		t.Fatalf("converted stream does not contain [DONE]: %s", output.Bytes())
	}
}

func TestMessagesToChatResponsePreservesThinking(t *testing.T) {
	body := []byte(`{"id":"msg_test","model":"upstream-model","content":[{"type":"thinking","thinking":"分析中","signature":"opaque"},{"type":"redacted_thinking","data":"encrypted"},{"type":"text","text":"我是模型"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":8}}`)

	converted, err := ConvertResponse(body, Messages, Chat, "virtual-model")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(converted, []byte(`"reasoning_content":"分析中"`)) {
		t.Fatalf("converted response does not contain reasoning: %s", converted)
	}
	if !bytes.Contains(converted, []byte(`"content":"我是模型"`)) {
		t.Fatalf("converted response does not contain text: %s", converted)
	}
	if bytes.Contains(converted, []byte("opaque")) || bytes.Contains(converted, []byte("encrypted")) {
		t.Fatalf("converted response leaked protected thinking data: %s", converted)
	}
}

func TestMessagesToResponsesPreservesThinking(t *testing.T) {
	converter, err := NewStreamConverter(Messages, Responses, "virtual-model")
	if err != nil {
		t.Fatal(err)
	}

	events := []SSEEvent{
		{Name: "message_start", Data: []byte(`{"type":"message_start","message":{"id":"msg_test","model":"upstream-model","usage":{"input_tokens":5,"output_tokens":1}}}`)},
		{Name: "content_block_start", Data: []byte(`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`)},
		{Name: "content_block_delta", Data: []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"分析中"}}`)},
		{Name: "content_block_delta", Data: []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"opaque"}}`)},
		{Name: "content_block_stop", Data: []byte(`{"type":"content_block_stop","index":0}`)},
		{Name: "content_block_start", Data: []byte(`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`)},
		{Name: "content_block_delta", Data: []byte(`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"我是模型"}}`)},
		{Name: "content_block_stop", Data: []byte(`{"type":"content_block_stop","index":1}`)},
		{Name: "message_delta", Data: []byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":8}}`)},
		{Name: "message_stop", Data: []byte(`{"type":"message_stop"}`)},
	}

	var output bytes.Buffer
	for index, event := range events {
		chunks, _, _, convertErr := converter.Convert(event)
		if convertErr != nil {
			t.Fatalf("event %d: %v", index, convertErr)
		}
		for _, chunk := range chunks {
			output.Write(chunk)
		}
	}

	result := output.Bytes()
	markers := [][]byte{
		[]byte(`"type":"response.output_item.added"`),
		[]byte(`"type":"response.reasoning_summary_part.added"`),
		[]byte(`"type":"response.reasoning_summary_text.delta"`),
		[]byte(`"type":"response.reasoning_summary_text.done"`),
		[]byte(`"type":"response.reasoning_summary_part.done"`),
		[]byte(`"type":"response.output_text.delta"`),
		[]byte(`"type":"response.completed"`),
	}
	position := -1
	for _, marker := range markers {
		next := bytes.Index(result[position+1:], marker)
		if next < 0 {
			t.Fatalf("converted stream does not contain %s: %s", marker, result)
		}
		position += next + 1
	}
	if !bytes.Contains(result, []byte(`"delta":"分析中"`)) || !bytes.Contains(result, []byte(`"summary":[{"text":"分析中","type":"summary_text"}]`)) {
		t.Fatalf("converted stream does not contain reasoning summary: %s", result)
	}
	if !bytes.Contains(result, []byte(`"delta":"我是模型"`)) {
		t.Fatalf("converted stream does not contain text: %s", result)
	}
	if bytes.Contains(result, []byte("opaque")) {
		t.Fatalf("converted stream leaked thinking signature: %s", result)
	}
}

func TestMessagesToResponsesResponsePreservesThinking(t *testing.T) {
	body := []byte(`{"id":"msg_test","model":"upstream-model","content":[{"type":"thinking","thinking":"分析中","signature":"opaque"},{"type":"redacted_thinking","data":"encrypted"},{"type":"text","text":"我是模型"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":8}}`)

	converted, err := ConvertResponse(body, Messages, Responses, "virtual-model")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(converted, []byte(`"type":"reasoning"`)) || !bytes.Contains(converted, []byte(`"summary":[{"text":"分析中","type":"summary_text"}]`)) {
		t.Fatalf("converted response does not contain reasoning summary: %s", converted)
	}
	if !bytes.Contains(converted, []byte(`"type":"output_text"`)) || !bytes.Contains(converted, []byte(`"text":"我是模型"`)) {
		t.Fatalf("converted response does not contain text: %s", converted)
	}
	if bytes.Contains(converted, []byte("opaque")) || bytes.Contains(converted, []byte("encrypted")) {
		t.Fatalf("converted response leaked protected thinking data: %s", converted)
	}
}
