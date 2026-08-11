package protocol

import "encoding/json"

const (
	Chat      = "chat_completions"
	Responses = "responses"
	Messages  = "messages"
)

type Request struct {
	Model             string
	Instructions      []string
	Messages          []Message
	Tools             []Tool
	ToolChoice        ToolChoice
	Stream            bool
	ParallelToolCalls bool
	MaxOutputTokens   *int
	Temperature       *float64
	TopP              *float64
	Stop              []string
	Effort            string
}

type Message struct {
	Role   string
	Blocks []Block
}

type Block struct {
	Type       string
	Text       string
	ToolCall   *ToolCall
	ToolResult *ToolResult
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

type ToolResult struct {
	CallID  string
	Content string
	IsError bool
}

type Tool struct {
	Kind        string
	Name        string
	Description string
	Parameters  json.RawMessage
	Strict      *bool
}

type ToolChoice struct {
	Mode string
	Name string
}

type Response struct {
	ID         string
	Model      string
	Blocks     []Block
	StopReason string
	Usage      Usage
}

type Usage struct {
	InputTokens      *int
	CacheReadTokens  *int
	CacheWriteTokens *int
	OutputTokens     *int
	ReasoningTokens  *int
	TotalTokens      *int
	Raw              json.RawMessage
}

func boolPointer(value bool) *bool {
	return &value
}
