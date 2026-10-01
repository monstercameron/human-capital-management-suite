package openai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

type responseRequest struct {
	Reasoning         map[string]string `json:"reasoning,omitempty"`
	ServiceTier       string            `json:"service_tier"`
	Model             string            `json:"model"`
	Input             []requestMessage  `json:"input"`
	Store             bool              `json:"store"`
	MaxOutputTokens   int64             `json:"max_output_tokens,omitempty"`
	Tools             []requestTool     `json:"tools,omitempty"`
	ToolChoice        string            `json:"tool_choice,omitempty"`
	ParallelToolCalls bool              `json:"parallel_tool_calls,omitempty"`
	Text              *requestText      `json:"text,omitempty"`
}

type requestMessage struct {
	Type      string `json:"type,omitempty"`
	Role      string `json:"role,omitempty"`
	Content   string `json:"content,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
}

type requestTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      bool            `json:"strict"`
}

type requestText struct {
	Format responseFormat `json:"format"`
}
type responseFormat struct {
	Type   string          `json:"type"`
	Name   string          `json:"name,omitempty"`
	Schema json.RawMessage `json:"schema,omitempty"`
	Strict bool            `json:"strict,omitempty"`
}

func makeRequest(model string, req agentmodel.ModelRequest) responseRequest {
	out := responseRequest{Model: model, Store: false, ServiceTier: "default", ParallelToolCalls: false}
	// This pinned mini model supports minimal reasoning, keeping bounded chat replies within their output allowance.
	if model == "gpt-5-mini-2025-08-07" {
		out.Reasoning = map[string]string{"effort": "minimal"}
	}
	if req.Limits.MaxOutputTokens > 0 {
		out.MaxOutputTokens = req.Limits.MaxOutputTokens
	}
	for _, message := range req.Messages {
		switch message.Role {
		case agentmodel.RoleTool:
			out.Input = append(out.Input, requestMessage{Type: "function_call_output", CallID: message.ToolCallID, Output: message.Content})
		case agentmodel.RoleAssistant:
			if message.ToolCallID != "" {
				out.Input = append(out.Input, requestMessage{Type: "function_call", CallID: message.ToolCallID, Name: message.ToolName, Arguments: string(message.ToolArguments)})
			} else {
				out.Input = append(out.Input, requestMessage{Role: string(message.Role), Content: message.Content})
			}
		default:
			out.Input = append(out.Input, requestMessage{Role: string(message.Role), Content: message.Content})
		}
	}
	for _, tool := range req.Tools {
		out.Tools = append(out.Tools, requestTool{Type: "function", Name: tool.Name, Description: tool.Description, Parameters: tool.InputSchema})
	}
	if len(out.Tools) > 0 {
		out.ToolChoice = "auto"
	}
	switch req.Output.Mode {
	case agentmodel.OutputJSON:
		out.Text = &requestText{Format: responseFormat{Type: "json_object"}}
	case agentmodel.OutputSchema:
		out.Text = &requestText{Format: responseFormat{Type: "json_schema", Name: "hcm_output", Schema: req.Output.Schema, Strict: true}}
	}
	return out
}

// requestIdentity gives retries of one trace and exact payload a stable provider correlation ID.
func requestIdentity(body []byte, traceID string) string {
	digest := sha256.Sum256([]byte(traceID + "\x00" + string(body)))
	return "hcm-" + hex.EncodeToString(digest[:16])
}

type responseEnvelope struct {
	ID                string `json:"id"`
	Model             string `json:"model"`
	Status            string `json:"status"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
	Usage  *responseUsage `json:"usage"`
	Output []responseItem `json:"output"`
}

type responseUsage struct {
	InputTokenDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

type responseItem struct {
	Type      string            `json:"type"`
	ID        string            `json:"id"`
	CallID    string            `json:"call_id"`
	Name      string            `json:"name"`
	Arguments string            `json:"arguments"`
	Content   []responseContent `json:"content"`
}

type responseContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func decodeResponse(data []byte) (responseEnvelope, error) {
	var decoded responseEnvelope
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&decoded); err != nil {
		return responseEnvelope{}, err
	}
	if decoded.ID == "" || decoded.Model == "" || decoded.Status == "" || decoded.Usage == nil {
		return responseEnvelope{}, ErrInvalidResponse
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return responseEnvelope{}, ErrInvalidResponse
	}
	return decoded, nil
}

// A dated OpenAI snapshot is both the requested and accepted wire model.
// Logical test/deployment revision labels retain their configured model ID.
func providerModelID(identity agentmodel.ModelIdentity) string {
	prefix := identity.ModelID + "-"
	if strings.HasPrefix(identity.Version, prefix) {
		if _, err := time.Parse("2006-01-02", strings.TrimPrefix(identity.Version, prefix)); err == nil {
			return identity.Version
		}
	}
	return identity.ModelID
}

func mapResponse(identity agentmodel.ModelIdentity, response responseEnvelope, req agentmodel.ModelRequest) (agentmodel.ModelResult, error) {
	if response.Model != providerModelID(identity) {
		return invalidResult(identity, "provider returned a model different from the pinned model")
	}
	result := agentmodel.ModelResult{
		ContractVersion:   agentmodel.ContractVersion,
		Provider:          identity,
		ProviderRequestID: response.ID,
		Usage:             agentmodel.ModelUsage{InputTokens: response.Usage.InputTokens, CachedInputTokens: response.Usage.InputTokenDetails.CachedTokens, OutputTokens: response.Usage.OutputTokens, TotalTokens: response.Usage.TotalTokens},
	}
	if result.Usage.TotalTokens == 0 {
		result.Usage.TotalTokens = result.Usage.InputTokens + result.Usage.OutputTokens
	}
	switch response.Status {
	case "completed":
		result.Finish = agentmodel.FinishComplete
	case "incomplete":
		result.Finish = agentmodel.FinishLength
		if response.IncompleteDetails != nil && response.IncompleteDetails.Reason != "max_output_tokens" {
			return invalidResult(identity, "provider returned an unsupported incomplete response")
		}
		// Partial content has not completed contract validation and must not escape.
		return result, nil
	case "cancelled":
		result.Finish = agentmodel.FinishCancelled
		return result, context.Canceled
	case "failed":
		return failureResult(identity, agentmodel.FailureUnavailable, true, "provider reported a failed response"), providerError{code: agentmodel.FailureUnavailable, retryable: true, message: "provider reported a failed response"}
	default:
		return invalidResult(identity, "provider returned an unknown response status")
	}
	toolNames := make(map[string]struct{}, len(req.Tools))
	for _, tool := range req.Tools {
		toolNames[tool.Name] = struct{}{}
	}
	for _, item := range response.Output {
		switch item.Type {
		case "message":
			for _, content := range item.Content {
				switch content.Type {
				case "output_text":
					result.Text += content.Text
				case "refusal":
					result.Finish = agentmodel.FinishBlocked
					result.Text = ""
					result.Structured = nil
					result.ToolProposals = nil
					result.Refusal = &agentmodel.ModelRefusal{Code: agentmodel.RefusalContentPolicy, Detail: "provider refused the request"}
				default:
					return invalidResult(identity, "provider returned unsupported message content")
				}
			}
		case "function_call":
			if item.CallID == "" || item.Name == "" || !json.Valid([]byte(item.Arguments)) {
				return invalidResult(identity, "provider returned an invalid function proposal")
			}
			if _, allowed := toolNames[item.Name]; !allowed {
				return invalidResult(identity, "provider proposed an unrequested function")
			}
			result.ToolProposals = append(result.ToolProposals, agentmodel.ToolProposal{ID: item.CallID, Name: item.Name, Arguments: json.RawMessage(item.Arguments)})
		case "reasoning":
			// Reasoning items are provider-internal and are never exposed to callers.
		default:
			return invalidResult(identity, "provider returned an unsupported output item")
		}
	}
	if response.Status == "completed" && result.Refusal == nil && len(result.ToolProposals) > 0 {
		result.Finish = agentmodel.FinishToolCalls
	}
	if result.Refusal == nil && strings.TrimSpace(result.Text) == "" && len(result.ToolProposals) == 0 && result.Finish == agentmodel.FinishComplete {
		return invalidResult(identity, "provider returned no content")
	}
	if req.Output.Mode != agentmodel.OutputText && result.Text != "" {
		if !json.Valid([]byte(result.Text)) {
			return invalidResult(identity, "provider returned invalid structured output")
		}
		result.Structured = json.RawMessage(result.Text)
	}
	return result, nil
}
