package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

type SSEEvent struct {
	Name string
	Data []byte
	Raw  []byte
}

func ReadSSEEvent(reader *bufio.Reader) (SSEEvent, error) {
	var event SSEEvent
	var raw bytes.Buffer
	var data []string
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			raw.WriteString(line)
			trimmed := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
			if trimmed == "" {
				event.Raw = raw.Bytes()
				event.Data = []byte(strings.Join(data, "\n"))
				return event, nil
			}
			if !strings.HasPrefix(trimmed, ":") {
				field, value, _ := strings.Cut(trimmed, ":")
				value = strings.TrimPrefix(value, " ")
				switch field {
				case "event":
					event.Name = value
				case "data":
					data = append(data, value)
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) && raw.Len() > 0 {
				event.Raw = raw.Bytes()
				event.Data = []byte(strings.Join(data, "\n"))
				return event, nil
			}
			return SSEEvent{}, err
		}
	}
}

type streamKind string

const (
	streamStart     streamKind = "start"
	streamText      streamKind = "text"
	streamReasoning streamKind = "reasoning"
	streamToolStart streamKind = "tool_start"
	streamToolDelta streamKind = "tool_delta"
	streamToolEnd   streamKind = "tool_end"
	streamFinish    streamKind = "finish"
	streamUsage     streamKind = "usage"
	streamDone      streamKind = "done"
)

type streamEvent struct {
	Kind       streamKind
	ID         string
	Model      string
	Index      int
	CallID     string
	Name       string
	Delta      string
	StopReason string
	Usage      Usage
}

type StreamConverter struct {
	decoder *streamDecoder
	encoder *streamEncoder
}

func NewStreamConverter(from, to, virtualModel string) (*StreamConverter, error) {
	if from != Chat && from != Responses && from != Messages {
		return nil, fmt.Errorf("unsupported source stream protocol %q", from)
	}
	if to != Chat && to != Responses && to != Messages {
		return nil, fmt.Errorf("unsupported target stream protocol %q", to)
	}
	if from == to {
		return nil, errors.New("stream converter requires different protocols")
	}
	return &StreamConverter{
		decoder: newStreamDecoder(from),
		encoder: newStreamEncoder(to, virtualModel),
	}, nil
}

func (c *StreamConverter) Convert(input SSEEvent) (output [][]byte, semantic, done bool, err error) {
	events, err := c.decoder.Decode(input)
	if err != nil {
		return nil, false, false, err
	}
	for _, event := range events {
		encoded, err := c.encoder.Encode(event)
		if err != nil {
			return nil, false, false, err
		}
		output = append(output, encoded...)
		if event.Kind == streamText && event.Delta != "" || event.Kind == streamReasoning && event.Delta != "" && len(encoded) > 0 || event.Kind == streamToolStart || event.Kind == streamToolDelta && event.Delta != "" {
			semantic = true
		}
		if event.Kind == streamDone {
			done = true
		}
	}
	return output, semantic, done, nil
}

func encodeSSE(name string, value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result bytes.Buffer
	if name != "" {
		result.WriteString("event: ")
		result.WriteString(name)
		result.WriteByte('\n')
	}
	result.WriteString("data: ")
	result.Write(payload)
	result.WriteString("\n\n")
	return result.Bytes(), nil
}

func dataSSE(value any) ([]byte, error) {
	return encodeSSE("", value)
}
