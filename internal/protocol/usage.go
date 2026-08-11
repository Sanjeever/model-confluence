package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

func ExtractUsage(body []byte, source string, stream bool) (Usage, string, error) {
	if !stream {
		usage, raw, err := extractDocumentUsage(body, source)
		if err == nil && usage.TotalTokens == nil {
			usage.TotalTokens = sumPointers(usage.InputTokens, usage.CacheReadTokens, usage.CacheWriteTokens, usage.OutputTokens)
		}
		return usage, string(raw), err
	}

	reader := bufio.NewReader(bytes.NewReader(body))
	var result Usage
	for {
		event, err := ReadSSEEvent(reader)
		if len(bytes.TrimSpace(event.Data)) > 0 && !bytes.Equal(bytes.TrimSpace(event.Data), []byte("[DONE]")) {
			usage, _, usageErr := extractDocumentUsage(event.Data, source)
			if usageErr != nil {
				return Usage{}, "", usageErr
			}
			mergeExtractedUsage(&result, usage)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return Usage{}, "", err
			}
			break
		}
	}
	if result.TotalTokens == nil {
		result.TotalTokens = sumPointers(result.InputTokens, result.CacheReadTokens, result.CacheWriteTokens, result.OutputTokens)
	}
	if !usagePresent(result) {
		return Usage{}, "", nil
	}
	raw, err := json.Marshal(usageMap(result))
	if err != nil {
		return Usage{}, "", err
	}
	return result, string(raw), nil
}

func extractDocumentUsage(body []byte, source string) (Usage, json.RawMessage, error) {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(body, &value); err != nil {
		return Usage{}, nil, err
	}
	if source == Responses && len(value["response"]) > 0 {
		if err := json.Unmarshal(value["response"], &value); err != nil {
			return Usage{}, nil, err
		}
	} else if source == Messages && len(value["message"]) > 0 {
		if err := json.Unmarshal(value["message"], &value); err != nil {
			return Usage{}, nil, err
		}
	}
	raw := value["usage"]
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return Usage{}, nil, nil
	}
	var usage Usage
	switch source {
	case Chat:
		var decoded chatStreamUsage
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return Usage{}, nil, err
		}
		usage = decoded.canonical()
	case Responses:
		var decoded responsesStreamUsage
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return Usage{}, nil, err
		}
		usage = decoded.canonical()
	case Messages:
		var decoded messagesStreamUsage
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return Usage{}, nil, err
		}
		usage = decoded.canonical()
	default:
		return Usage{}, nil, errors.New("unsupported usage protocol")
	}
	return usage, raw, nil
}

func mergeExtractedUsage(destination *Usage, source Usage) {
	if source.InputTokens != nil {
		destination.InputTokens = source.InputTokens
	}
	if source.CacheReadTokens != nil {
		destination.CacheReadTokens = source.CacheReadTokens
	}
	if source.CacheWriteTokens != nil {
		destination.CacheWriteTokens = source.CacheWriteTokens
	}
	if source.OutputTokens != nil {
		destination.OutputTokens = source.OutputTokens
	}
	if source.ReasoningTokens != nil {
		destination.ReasoningTokens = source.ReasoningTokens
	}
	if source.TotalTokens != nil {
		destination.TotalTokens = source.TotalTokens
	}
}

func usageMap(usage Usage) map[string]any {
	return map[string]any{
		"input_tokens":       usage.InputTokens,
		"cache_read_tokens":  usage.CacheReadTokens,
		"cache_write_tokens": usage.CacheWriteTokens,
		"output_tokens":      usage.OutputTokens,
		"reasoning_tokens":   usage.ReasoningTokens,
		"total_tokens":       usage.TotalTokens,
	}
}
