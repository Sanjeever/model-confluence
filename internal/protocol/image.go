package protocol

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

func decodeChatImage(raw json.RawMessage, path string) (Image, error) {
	var source struct {
		URL    string  `json:"url"`
		Detail *string `json:"detail"`
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		return Image{}, fmt.Errorf("%s: %w", path, err)
	}
	if source.URL == "" {
		return Image{}, fmt.Errorf("%s.url is required", path)
	}
	imageSource, err := parseImageURL(source.URL, path+".url")
	if err != nil {
		return Image{}, err
	}
	if err := validateImageDetail(source.Detail, path+".detail"); err != nil {
		return Image{}, err
	}
	return Image{Source: imageSource, Detail: source.Detail}, nil
}

func decodeResponsesImage(raw map[string]json.RawMessage, path string) (Image, error) {
	var source struct {
		URL    *string `json:"image_url"`
		FileID *string `json:"file_id"`
		Detail *string `json:"detail"`
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return Image{}, err
	}
	if err := json.Unmarshal(encoded, &source); err != nil {
		return Image{}, fmt.Errorf("%s: %w", path, err)
	}
	if source.FileID != nil {
		return Image{}, fmt.Errorf("%s.file_id is not supported", path)
	}
	if source.URL == nil || *source.URL == "" {
		return Image{}, fmt.Errorf("%s.image_url is required", path)
	}
	imageSource, err := parseImageURL(*source.URL, path+".image_url")
	if err != nil {
		return Image{}, err
	}
	if err := validateImageDetail(source.Detail, path+".detail"); err != nil {
		return Image{}, err
	}
	return Image{Source: imageSource, Detail: source.Detail}, nil
}

func decodeMessagesImage(raw map[string]json.RawMessage, path string) (Image, error) {
	var source struct {
		Type      string `json:"type"`
		URL       string `json:"url"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
		FileID    string `json:"file_id"`
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return Image{}, err
	}
	if err := json.Unmarshal(encoded, &source); err != nil {
		return Image{}, fmt.Errorf("%s: %w", path, err)
	}
	switch source.Type {
	case "url":
		if source.URL == "" {
			return Image{}, fmt.Errorf("%s.url is required", path)
		}
		imageSource, err := parseImageURL(source.URL, path+".url")
		if err != nil {
			return Image{}, err
		}
		return Image{Source: imageSource}, nil
	case "base64":
		if source.MediaType == "" {
			return Image{}, fmt.Errorf("%s.media_type is required", path)
		}
		if source.Data == "" {
			return Image{}, fmt.Errorf("%s.data is required", path)
		}
		if err := validateBase64Image(source.MediaType, source.Data, path); err != nil {
			return Image{}, err
		}
		return Image{Source: ImageSource{Kind: ImageBase64, MediaType: source.MediaType, Data: source.Data}}, nil
	case "file":
		if source.FileID == "" {
			return Image{}, fmt.Errorf("%s.file_id is required", path)
		}
		return Image{}, fmt.Errorf("%s.file_id is not supported", path)
	default:
		return Image{}, fmt.Errorf("unsupported image source type %q at %s.type", source.Type, path)
	}
}

func parseImageURL(value, path string) (ImageSource, error) {
	if !strings.HasPrefix(strings.ToLower(value), "data:") {
		return ImageSource{Kind: ImageURL, URL: value}, nil
	}
	header, data, ok := strings.Cut(value[5:], ",")
	if !ok {
		return ImageSource{}, fmt.Errorf("%s must be a valid base64 data URL", path)
	}
	parts := strings.Split(header, ";")
	mediaType := parts[0]
	if !strings.HasPrefix(strings.ToLower(mediaType), "image/") {
		return ImageSource{}, fmt.Errorf("%s must contain an image media type", path)
	}
	base64Encoded := false
	for _, part := range parts[1:] {
		if strings.EqualFold(part, "base64") {
			base64Encoded = true
		}
	}
	if !base64Encoded {
		return ImageSource{}, fmt.Errorf("%s must be a base64 data URL", path)
	}
	if err := validateBase64(data, path); err != nil {
		return ImageSource{}, err
	}
	return ImageSource{Kind: ImageBase64, MediaType: mediaType, Data: data}, nil
}

func validateBase64Image(mediaType, data, path string) error {
	if !strings.HasPrefix(strings.ToLower(mediaType), "image/") {
		return fmt.Errorf("%s.media_type must be an image media type", path)
	}
	return validateBase64(data, path+".data")
}

func validateBase64(data, path string) error {
	if _, err := base64.StdEncoding.DecodeString(data); err != nil {
		if _, rawErr := base64.RawStdEncoding.DecodeString(data); rawErr != nil {
			return fmt.Errorf("%s must contain valid base64 data", path)
		}
	}
	return nil
}

func validateImageDetail(detail *string, path string) error {
	if detail == nil {
		return nil
	}
	switch *detail {
	case "auto", "low", "high":
		return nil
	default:
		return fmt.Errorf("unsupported image detail %q at %s", *detail, path)
	}
}

func encodeImage(image *Image, target, path string) (any, error) {
	if image == nil {
		return nil, fmt.Errorf("%s is missing", path)
	}
	if err := validateImageDetail(image.Detail, path+".detail"); err != nil {
		return nil, err
	}
	if image.Source.Kind == ImageFileID {
		return nil, fmt.Errorf("%s.file_id is not supported", path)
	}
	switch target {
	case Chat:
		value := map[string]any{"type": "image_url", "image_url": map[string]any{"url": imageURL(image.Source)}}
		if image.Detail != nil {
			value["image_url"].(map[string]any)["detail"] = *image.Detail
		}
		return value, nil
	case Responses:
		value := map[string]any{"type": "input_image", "image_url": imageURL(image.Source)}
		if image.Detail != nil {
			value["detail"] = *image.Detail
		}
		return value, nil
	case Messages:
		if image.Detail != nil && *image.Detail != "auto" {
			return nil, fmt.Errorf("image detail %q cannot be represented by target protocol %q at %s.detail", *image.Detail, target, path)
		}
		switch image.Source.Kind {
		case ImageURL:
			return map[string]any{"type": "image", "source": map[string]any{"type": "url", "url": image.Source.URL}}, nil
		case ImageBase64:
			return map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": image.Source.MediaType, "data": image.Source.Data}}, nil
		default:
			return nil, fmt.Errorf("unsupported image source %q at %s", image.Source.Kind, path)
		}
	default:
		return nil, fmt.Errorf("unsupported image target protocol %q", target)
	}
}

func imageURL(source ImageSource) string {
	if source.Kind == ImageBase64 {
		return "data:" + source.MediaType + ";base64," + source.Data
	}
	return source.URL
}
