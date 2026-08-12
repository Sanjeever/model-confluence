package store

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
)

const (
	payloadEncodingIdentity = "identity"
	payloadEncodingGzip     = "gzip"
	payloadCompressionLimit = 4 << 10
)

func encodePayload(body []byte) ([]byte, string, error) {
	if len(body) < payloadCompressionLimit {
		return body, payloadEncodingIdentity, nil
	}

	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(body); err != nil {
		return nil, "", fmt.Errorf("compress payload: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("finish payload compression: %w", err)
	}
	if compressed.Len() >= len(body) {
		return body, payloadEncodingIdentity, nil
	}
	return compressed.Bytes(), payloadEncodingGzip, nil
}

func decodePayload(body []byte, encoding string) ([]byte, error) {
	switch encoding {
	case "", payloadEncodingIdentity:
		return body, nil
	case payloadEncodingGzip:
		reader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("open compressed payload: %w", err)
		}
		decoded, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read compressed payload: %w", readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close compressed payload: %w", closeErr)
		}
		return decoded, nil
	default:
		return nil, fmt.Errorf("unsupported payload encoding %q", encoding)
	}
}
