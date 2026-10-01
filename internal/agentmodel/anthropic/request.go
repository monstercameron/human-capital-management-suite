package anthropic

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

var validToolName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type messageRequest struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type toolRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type toolChoice struct {
	Type                   string `json:"type"`
	DisableParallelToolUse bool   `json:"disable_parallel_tool_use,omitempty"`
}

type outputConfig struct {
	Format outputFormat `json:"format"`
}

type outputFormat struct {
	Type   string          `json:"type"`
	Schema json.RawMessage `json:"schema"`
}

type messagesRequest struct {
	Model        string           `json:"model"`
	MaxTokens    int64            `json:"max_tokens"`
	System       string           `json:"system,omitempty"`
	Messages     []messageRequest `json:"messages"`
	Tools        []toolRequest    `json:"tools,omitempty"`
	ToolChoice   *toolChoice      `json:"tool_choice,omitempty"`
	OutputConfig *outputConfig    `json:"output_config,omitempty"`
}

func makeRequest(model string, req agentmodel.ModelRequest) (messagesRequest, error) {
	out := messagesRequest{Model: model, MaxTokens: req.Limits.MaxOutputTokens}
	if out.MaxTokens == 0 {
		out.MaxTokens = 1024
	}
	conversationStarted := false
	var systemParts []string
	for i, message := range req.Messages {
		switch message.Role {
		case agentmodel.RoleSystem, agentmodel.RoleDeveloper:
			if conversationStarted {
				return messagesRequest{}, &agentmodel.ModelRefusal{Code: agentmodel.RefusalFeature, Detail: fmt.Sprintf("Anthropic cannot preserve instruction message %d after conversation messages", i)}
			}
			systemParts = append(systemParts, message.Content)
		case agentmodel.RoleUser, agentmodel.RoleAssistant, agentmodel.RoleTool:
			conversationStarted = true
			role := string(message.Role)
			if message.Role == agentmodel.RoleTool {
				content, _ := json.Marshal([]contentBlock{{Type: "tool_result", ToolUseID: message.ToolCallID, Content: message.Content}})
				out.Messages = append(out.Messages, messageRequest{Role: "user", Content: content})
			} else if message.Role == agentmodel.RoleAssistant && message.ToolCallID != "" {
				if !validToolName.MatchString(message.ToolName) {
					return messagesRequest{}, &agentmodel.ModelRefusal{Code: agentmodel.RefusalFeature, Feature: agentmodel.FeatureTools, Detail: "tool name is outside the provider's supported syntax"}
				}
				content, _ := json.Marshal([]contentBlock{{Type: "tool_use", ID: message.ToolCallID, Name: message.ToolName, Input: message.ToolArguments}})
				out.Messages = append(out.Messages, messageRequest{Role: "assistant", Content: content})
			} else {
				content, _ := json.Marshal(message.Content)
				if n := len(out.Messages); n > 0 && out.Messages[n-1].Role == role {
					var previous string
					_ = json.Unmarshal(out.Messages[n-1].Content, &previous)
					merged, _ := json.Marshal(previous + "\n" + message.Content)
					out.Messages[n-1].Content = merged
				} else {
					out.Messages = append(out.Messages, messageRequest{Role: role, Content: content})
				}
			}
		default:
			return messagesRequest{}, &agentmodel.ModelRefusal{Code: agentmodel.RefusalFeature, Detail: "unsupported message role"}
		}
	}
	out.System = strings.Join(systemParts, "\n\n")
	for _, tool := range req.Tools {
		if !validToolName.MatchString(tool.Name) {
			return messagesRequest{}, &agentmodel.ModelRefusal{Code: agentmodel.RefusalFeature, Feature: agentmodel.FeatureTools, Detail: "tool name is outside the provider's supported syntax"}
		}
		out.Tools = append(out.Tools, toolRequest{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema})
	}
	if len(out.Tools) > 0 {
		out.ToolChoice = &toolChoice{Type: "auto", DisableParallelToolUse: true}
	}
	switch req.Output.Mode {
	case agentmodel.OutputJSON:
		out.System = appendInstruction(out.System, "Return only valid JSON as the complete response.")
	case agentmodel.OutputSchema:
		out.OutputConfig = &outputConfig{Format: outputFormat{Type: "json_schema", Schema: req.Output.Schema}}
	}
	return out, nil
}

func appendInstruction(system, instruction string) string {
	if system == "" {
		return instruction
	}
	return system + "\n\n" + instruction
}
