package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var antigravityToolName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.-]{0,63}$`)

// PrepareAntigravityRequest 在进入宽松的公共解码器前检查本适配器支持的字段。
func PrepareAntigravityRequest(body []byte, source, model string, defaultMax, max int) (Request, []string, error) {
	var ignored []string
	if err := validateAntigravityInput(body, source, &ignored); err != nil {
		return Request{}, nil, err
	}
	request, err := DecodeRequest(body, source)
	if err != nil {
		return Request{}, nil, err
	}
	request.Model = model
	if request.MaxOutputTokens == nil {
		request.MaxOutputTokens = &defaultMax
	}
	if *request.MaxOutputTokens <= 0 || *request.MaxOutputTokens > max {
		return Request{}, nil, fmt.Errorf("最大输出 Token 须在 1 至 %d 之间", max)
	}
	for _, tool := range request.Tools {
		if !antigravityToolName.MatchString(tool.Name) {
			return Request{}, nil, fmt.Errorf("Antigravity 工具名无效: %q", tool.Name)
		}
		if tool.Kind != "function" {
			return Request{}, nil, fmt.Errorf("Antigravity 尚不支持 %s 工具", tool.Kind)
		}
		if tool.Strict != nil && *tool.Strict {
			return Request{}, nil, fmt.Errorf("tools.%s.strict 无等价的上游保证", tool.Name)
		}
	}
	if request.ToolChoice.Mode != "" && request.ToolChoice.Mode != "auto" && request.ToolChoice.Mode != "none" && request.ToolChoice.Mode != "required" && request.ToolChoice.Mode != "tool" {
		return Request{}, nil, fmt.Errorf("不支持的 tool_choice: %s", request.ToolChoice.Mode)
	}
	if request.ToolChoice.Mode == "tool" {
		found := false
		for _, tool := range request.Tools {
			found = found || tool.Name == request.ToolChoice.Name
		}
		if !found {
			return Request{}, nil, fmt.Errorf("tool_choice 引用了未声明的工具 %q", request.ToolChoice.Name)
		}
	}
	sort.Strings(ignored)
	return request, ignored, nil
}

func EncodeAntigravityRequest(request Request, project, requestID string) ([]byte, error) {
	contents := make([]any, 0, len(request.Messages))
	calls := make(map[string]ToolCall)
	for mi, message := range request.Messages {
		role := "user"
		if message.Role == "assistant" {
			role = "model"
		}
		parts := make([]any, 0, len(message.Blocks))
		for bi, block := range message.Blocks {
			path := fmt.Sprintf("messages[%d].content[%d]", mi, bi)
			switch block.Type {
			case "text":
				parts = append(parts, map[string]string{"text": block.Text})
			case "image":
				if block.Image == nil || block.Image.Source.Kind != ImageBase64 {
					return nil, fmt.Errorf("%s: Antigravity 图片输入目前要求 base64 或 data URL", path)
				}
				if block.Image.Detail != nil && *block.Image.Detail != "auto" {
					return nil, fmt.Errorf("%s.detail 无等价上游字段", path)
				}
				parts = append(parts, map[string]any{"inlineData": map[string]string{"mimeType": block.Image.Source.MediaType, "data": block.Image.Source.Data}})
			case "tool_call":
				call := block.ToolCall
				if call.ID == "" || call.Name == "" || !json.Valid(call.Arguments) {
					return nil, fmt.Errorf("%s: 工具调用缺少 ID、名称或有效 JSON 参数", path)
				}
				if _, exists := calls[call.ID]; exists {
					return nil, fmt.Errorf("%s: 重复工具调用 ID", path)
				}
				calls[call.ID] = *call
				parts = append(parts, map[string]any{"functionCall": map[string]any{"id": call.ID, "name": call.Name, "args": call.Arguments}})
			case "tool_result":
				result := block.ToolResult
				call, ok := calls[result.CallID]
				if !ok {
					return nil, fmt.Errorf("%s: 找不到工具结果对应的历史调用 %q", path, result.CallID)
				}
				field := "result"
				if result.IsError {
					field = "error"
				}
				parts = append(parts, map[string]any{"functionResponse": map[string]any{"id": result.CallID, "name": call.Name, "response": map[string]string{field: result.Content}}})
			default:
				return nil, fmt.Errorf("%s: Antigravity 不支持历史内容类型 %s", path, block.Type)
			}
		}
		contents = append(contents, map[string]any{"role": role, "parts": parts})
	}
	generation := map[string]any{"maxOutputTokens": request.MaxOutputTokens}
	if request.Temperature != nil {
		generation["temperature"] = request.Temperature
	}
	if request.TopP != nil {
		generation["topP"] = request.TopP
	}
	if len(request.Stop) > 0 {
		generation["stopSequences"] = request.Stop
	}
	inner := map[string]any{"contents": contents, "generationConfig": generation}
	if len(request.Instructions) > 0 {
		parts := make([]any, 0, len(request.Instructions))
		for _, instruction := range request.Instructions {
			parts = append(parts, map[string]string{"text": instruction})
		}
		inner["systemInstruction"] = map[string]any{"role": "user", "parts": parts}
	}
	if len(request.Tools) > 0 {
		declarations := make([]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			declaration := map[string]any{"name": tool.Name, "description": tool.Description}
			if len(tool.Parameters) > 0 {
				declaration["parameters"] = tool.Parameters
			}
			declarations = append(declarations, declaration)
		}
		inner["tools"] = []any{map[string]any{"functionDeclarations": declarations}}
		mode := "AUTO"
		switch request.ToolChoice.Mode {
		case "none":
			mode = "NONE"
		case "required", "tool":
			mode = "ANY"
		}
		choice := map[string]any{"mode": mode}
		if request.ToolChoice.Mode == "tool" {
			choice["allowedFunctionNames"] = []string{request.ToolChoice.Name}
		}
		inner["toolConfig"] = map[string]any{"functionCallingConfig": choice}
	}
	return json.Marshal(map[string]any{"project": project, "model": request.Model, "requestId": requestID, "userAgent": "antigravity", "request": inner})
}

func agObject(raw json.RawMessage, path, allowed string, ignored *[]string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, fmt.Errorf("%s 必须是 JSON 对象", path)
	}
	var unsupported []string
	for field, value := range object {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			continue
		}
		if field == "cache_control" && strings.Contains(" "+allowed+" ", " cache_control ") || field == "prompt_cache_key" && path == "$" {
			*ignored = append(*ignored, path+"."+field)
			continue
		}
		if !strings.Contains(" "+allowed+" ", " "+field+" ") {
			unsupported = append(unsupported, path+"."+field)
		}
	}
	if len(unsupported) > 0 {
		sort.Strings(unsupported)
		return nil, fmt.Errorf("Antigravity 不支持字段: %s", strings.Join(unsupported, ", "))
	}
	return object, nil
}

func agString(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}
func agPresent(raw json.RawMessage) bool {
	return len(raw) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func validateAntigravityInput(body []byte, source string, ignored *[]string) error {
	fields := "model tools tool_choice stream temperature top_p prompt_cache_key"
	switch source {
	case Chat:
		fields += " messages max_tokens max_completion_tokens stop parallel_tool_calls stream_options"
	case Responses:
		fields += " input instructions max_output_tokens parallel_tool_calls store"
	case Messages:
		fields += " messages system max_tokens stop_sequences"
	default:
		return fmt.Errorf("不支持的客户端协议 %s", source)
	}
	root, err := agObject(body, "$", fields, ignored)
	if err != nil {
		return err
	}
	if agPresent(root["store"]) && string(root["store"]) != "false" {
		return fmt.Errorf("$.store 必须为 false")
	}
	if agPresent(root["parallel_tool_calls"]) && string(root["parallel_tool_calls"]) != "true" {
		return fmt.Errorf("$.parallel_tool_calls: Antigravity 无法保证禁止并行工具调用")
	}
	if agPresent(root["stream_options"]) {
		options, err := agObject(root["stream_options"], "$.stream_options", "include_usage", ignored)
		if err != nil {
			return err
		}
		if agPresent(options["include_usage"]) && string(options["include_usage"]) != "true" && string(options["include_usage"]) != "false" {
			return fmt.Errorf("$.stream_options.include_usage 必须为布尔值")
		}
	}
	for _, field := range []string{"instructions", "system"} {
		if agPresent(root[field]) {
			if err := agContent(root[field], "$."+field, source, ignored); err != nil {
				return err
			}
		}
	}
	messageField := "messages"
	if source == Responses {
		messageField = "input"
	}
	raw := bytes.TrimSpace(root[messageField])
	if len(raw) == 0 {
		return fmt.Errorf("$.%s 不能为空", messageField)
	}
	if source != Responses || raw[0] != '"' {
		var messages []json.RawMessage
		if err := json.Unmarshal(raw, &messages); err != nil {
			return fmt.Errorf("$.%s 必须是数组", messageField)
		}
		seenContent := false
		for i, message := range messages {
			path := fmt.Sprintf("$.%s[%d]", messageField, i)
			allowed := "role content"
			if source == Chat {
				allowed += " tool_calls tool_call_id"
			}
			if source == Responses {
				allowed += " type id call_id name arguments output"
			}
			item, err := agObject(message, path, allowed, ignored)
			if err != nil {
				return err
			}
			role := agString(item["role"])
			if role == "system" || role == "developer" {
				if seenContent {
					return fmt.Errorf("%s.role: 不支持对话中途的 system/developer", path)
				}
			} else {
				seenContent = true
			}
			for _, field := range []string{"content", "output"} {
				if agPresent(item[field]) {
					if err := agContent(item[field], path+"."+field, source, ignored); err != nil {
						return err
					}
				}
			}
			if agPresent(item["tool_calls"]) {
				var calls []json.RawMessage
				if err := json.Unmarshal(item["tool_calls"], &calls); err != nil {
					return err
				}
				for j, call := range calls {
					cp := fmt.Sprintf("%s.tool_calls[%d]", path, j)
					obj, err := agObject(call, cp, "id type function", ignored)
					if err != nil {
						return err
					}
					if _, err := agObject(obj["function"], cp+".function", "name arguments", ignored); err != nil {
						return err
					}
				}
			}
		}
	}
	if agPresent(root["tools"]) {
		var tools []json.RawMessage
		if err := json.Unmarshal(root["tools"], &tools); err != nil {
			return err
		}
		for i, raw := range tools {
			path := fmt.Sprintf("$.tools[%d]", i)
			allowed := "type name description parameters strict"
			if source == Chat {
				allowed = "type function"
			}
			if source == Messages {
				allowed = "type name description input_schema cache_control"
			}
			tool, err := agObject(raw, path, allowed, ignored)
			if err != nil {
				return err
			}
			if source == Chat {
				if _, err := agObject(tool["function"], path+".function", "name description parameters strict", ignored); err != nil {
					return err
				}
			}
		}
	}
	if choice := bytes.TrimSpace(root["tool_choice"]); agPresent(choice) && choice[0] != '"' {
		allowed := "type name"
		if source == Chat {
			allowed = "type function"
		}
		object, err := agObject(choice, "$.tool_choice", allowed, ignored)
		if err != nil {
			return err
		}
		if source == Chat && agPresent(object["function"]) {
			if _, err := agObject(object["function"], "$.tool_choice.function", "name", ignored); err != nil {
				return err
			}
		}
	}
	return nil
}

func agContent(raw json.RawMessage, path, source string, ignored *[]string) error {
	raw = bytes.TrimSpace(raw)
	if !agPresent(raw) || raw[0] == '"' {
		return nil
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return fmt.Errorf("%s 必须是文本或内容数组", path)
	}
	for i, raw := range blocks {
		bp := fmt.Sprintf("%s[%d]", path, i)
		var header struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &header); err != nil {
			return err
		}
		fields := "type"
		switch header.Type {
		case "text", "input_text", "output_text":
			fields += " text cache_control"
		case "image_url":
			fields += " image_url"
		case "input_image":
			fields += " image_url detail"
		case "image":
			fields += " source cache_control"
		case "tool_use":
			fields += " id name input cache_control"
		case "tool_result":
			fields += " tool_use_id content is_error cache_control"
		default:
			return fmt.Errorf("%s.type: Antigravity 不支持 %q", bp, header.Type)
		}
		block, err := agObject(raw, bp, fields, ignored)
		if err != nil {
			return err
		}
		if header.Type == "image_url" {
			if _, err := agObject(block["image_url"], bp+".image_url", "url detail", ignored); err != nil {
				return err
			}
		}
		if header.Type == "image" {
			if _, err := agObject(block["source"], bp+".source", "type media_type data url", ignored); err != nil {
				return err
			}
		}
		if header.Type == "tool_result" {
			if err := agContent(block["content"], bp+".content", source, ignored); err != nil {
				return err
			}
		}
	}
	return nil
}
