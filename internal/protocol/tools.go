package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func decodeChatTools(raw json.RawMessage, destination *[]Tool) error {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	var tools []struct {
		Type     string `json:"type"`
		Function struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
			Strict      *bool           `json:"strict"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &tools); err != nil {
		return err
	}
	for _, source := range tools {
		if source.Type != "function" {
			return fmt.Errorf("unsupported Chat tool type %q", source.Type)
		}
		*destination = append(*destination, Tool{Kind: "function", Name: source.Function.Name, Description: source.Function.Description, Parameters: source.Function.Parameters, Strict: source.Function.Strict})
	}
	return nil
}

func decodeResponsesTools(raw json.RawMessage, destination *[]Tool) error {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	var tools []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &tools); err != nil {
		return err
	}
	for _, source := range tools {
		var kind, name, description string
		var strict *bool
		_ = json.Unmarshal(source["type"], &kind)
		_ = json.Unmarshal(source["name"], &name)
		_ = json.Unmarshal(source["description"], &description)
		_ = json.Unmarshal(source["strict"], &strict)
		parameters := source["parameters"]
		switch kind {
		case "function":
		case "custom":
			if len(parameters) == 0 {
				parameters = json.RawMessage(`{"type":"object","properties":{"input":{"type":"string"}},"required":["input"]}`)
			}
		case "shell", "apply_patch":
			name = kind
			parameters = json.RawMessage(`{"type":"object","additionalProperties":true}`)
		default:
			return fmt.Errorf("unsupported hosted Responses tool type %q", kind)
		}
		*destination = append(*destination, Tool{Kind: kind, Name: name, Description: description, Parameters: parameters, Strict: strict})
	}
	return nil
}

func decodeMessagesTools(raw json.RawMessage, destination *[]Tool) error {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	var tools []struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
		Type        string          `json:"type"`
	}
	if err := json.Unmarshal(raw, &tools); err != nil {
		return err
	}
	for _, source := range tools {
		if source.Type != "" && source.Type != "custom" {
			return fmt.Errorf("unsupported hosted Messages tool type %q", source.Type)
		}
		*destination = append(*destination, Tool{Kind: "function", Name: source.Name, Description: source.Description, Parameters: source.InputSchema})
	}
	return nil
}

func decodeChatToolChoice(raw json.RawMessage) ToolChoice {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ToolChoice{}
	}
	var mode string
	if json.Unmarshal(raw, &mode) == nil {
		return ToolChoice{Mode: mode}
	}
	var choice struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(raw, &choice) == nil && choice.Function.Name != "" {
		return ToolChoice{Mode: "tool", Name: choice.Function.Name}
	}
	return ToolChoice{}
}

func decodeResponsesToolChoice(raw json.RawMessage) ToolChoice {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ToolChoice{}
	}
	var mode string
	if json.Unmarshal(raw, &mode) == nil {
		return ToolChoice{Mode: mode}
	}
	var choice struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &choice) == nil && choice.Name != "" {
		return ToolChoice{Mode: "tool", Name: choice.Name}
	}
	return ToolChoice{}
}

func decodeMessagesToolChoice(raw json.RawMessage) ToolChoice {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ToolChoice{}
	}
	var choice struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &choice) != nil {
		return ToolChoice{}
	}
	mode := choice.Type
	if mode == "any" {
		mode = "required"
	}
	if mode == "tool" {
		return ToolChoice{Mode: "tool", Name: choice.Name}
	}
	return ToolChoice{Mode: mode}
}

func encodeChatToolChoice(choice ToolChoice) any {
	switch choice.Mode {
	case "auto", "none", "required":
		return choice.Mode
	case "tool":
		return map[string]any{"type": "function", "function": map[string]string{"name": choice.Name}}
	default:
		return nil
	}
}

func encodeResponsesToolChoice(choice ToolChoice) any {
	switch choice.Mode {
	case "auto", "none", "required":
		return choice.Mode
	case "tool":
		return map[string]string{"type": "function", "name": choice.Name}
	default:
		return nil
	}
}

func encodeMessagesToolChoice(choice ToolChoice) any {
	switch choice.Mode {
	case "auto", "none":
		return map[string]string{"type": choice.Mode}
	case "required":
		return map[string]string{"type": "any"}
	case "tool":
		return map[string]string{"type": "tool", "name": choice.Name}
	default:
		return nil
	}
}
