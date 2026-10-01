package anthropic

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

type responseEnvelope struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Role       string          `json:"role"`
	Model      string          `json:"model"`
	StopReason *string         `json:"stop_reason"`
	Content    []responseBlock `json:"content"`
	Usage      *responseUsage  `json:"usage"`
}

type responseUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type responseBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

func decodeResponse(data []byte) (responseEnvelope, error) {
	var decoded responseEnvelope
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&decoded); err != nil {
		return responseEnvelope{}, err
	}
	if decoded.ID == "" || decoded.Type != "message" || decoded.Role != "assistant" || decoded.Model == "" || decoded.Usage == nil || decoded.StopReason == nil {
		return responseEnvelope{}, ErrInvalidResponse
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return responseEnvelope{}, ErrInvalidResponse
	}
	return decoded, nil
}

func mapResponse(identity agentmodel.ModelIdentity, response responseEnvelope, req agentmodel.ModelRequest) (agentmodel.ModelResult, error) {
	if response.Model != identity.ModelID {
		return invalidResult(identity, "provider returned a model different from the pinned model")
	}
	if response.Usage.InputTokens < 0 || response.Usage.OutputTokens < 0 {
		return invalidResult(identity, "provider returned negative token usage")
	}
	result := agentmodel.ModelResult{
		ContractVersion:   agentmodel.ContractVersion,
		Provider:          identity,
		ProviderRequestID: response.ID,
		Usage: agentmodel.ModelUsage{
			InputTokens:  response.Usage.InputTokens,
			OutputTokens: response.Usage.OutputTokens,
			TotalTokens:  response.Usage.InputTokens + response.Usage.OutputTokens,
		},
	}
	switch *response.StopReason {
	case "end_turn", "stop_sequence":
		result.Finish = agentmodel.FinishComplete
	case "max_tokens":
		result.Finish = agentmodel.FinishLength
	case "tool_use":
		result.Finish = agentmodel.FinishToolCalls
	case "refusal":
		result.Finish = agentmodel.FinishBlocked
		result.Refusal = &agentmodel.ModelRefusal{Code: agentmodel.RefusalContentPolicy, Detail: "provider refused the request"}
	default:
		return invalidResult(identity, "provider returned an unsupported stop reason")
	}
	toolNames := make(map[string]struct{}, len(req.Tools))
	for _, tool := range req.Tools {
		toolNames[tool.Name] = struct{}{}
	}
	for _, block := range response.Content {
		switch block.Type {
		case "text":
			if result.Refusal == nil {
				result.Text += block.Text
			}
		case "tool_use":
			if *response.StopReason != "tool_use" || result.Refusal != nil || strings.TrimSpace(block.ID) == "" || block.Name == "" || !jsonObject(block.Input) {
				return invalidResult(identity, "provider returned an invalid tool proposal")
			}
			if _, allowed := toolNames[block.Name]; !allowed {
				return invalidResult(identity, "provider proposed an unrequested tool")
			}
			result.ToolProposals = append(result.ToolProposals, agentmodel.ToolProposal{ID: block.ID, Name: block.Name, Arguments: block.Input})
		case "thinking", "redacted_thinking":
			// Provider-internal reasoning is never exposed to HCM callers.
		default:
			return invalidResult(identity, "provider returned unsupported content")
		}
	}
	if result.Refusal != nil {
		result.Text = ""
		result.Structured = nil
		result.ToolProposals = nil
	} else if len(result.ToolProposals) > 0 {
		if len(result.ToolProposals) > 1 {
			return invalidResult(identity, "provider returned parallel tools despite the single-tool request")
		}
		result.Finish = agentmodel.FinishToolCalls
	} else if result.Finish == agentmodel.FinishToolCalls {
		return invalidResult(identity, "provider reported tool use without a tool proposal")
	}
	if result.Refusal == nil && req.Output.Mode != agentmodel.OutputText && result.Text != "" {
		if !json.Valid([]byte(result.Text)) {
			return invalidResult(identity, "provider returned invalid structured output")
		}
		result.Structured = json.RawMessage(result.Text)
	}
	if result.Refusal == nil && result.Finish == agentmodel.FinishComplete && strings.TrimSpace(result.Text) == "" && len(result.ToolProposals) == 0 {
		return invalidResult(identity, "provider returned no content")
	}
	if err := agentmodel.ValidateModelResult(req, result, capabilities()); err != nil {
		return invalidResult(identity, "provider result did not satisfy the model contract")
	}
	return result, nil
}

func capabilities() agentmodel.AdapterCapabilities {
	return agentmodel.AdapterCapabilities{
		ContractVersions: []int{agentmodel.ContractVersion},
		Features:         []agentmodel.ModelFeature{agentmodel.FeatureTools, agentmodel.FeatureStructuredJSON},
		OutputModes:      []agentmodel.OutputMode{agentmodel.OutputText, agentmodel.OutputJSON, agentmodel.OutputSchema},
		MaxTools:         maxTools,
	}
}

func jsonObject(data []byte) bool {
	var object map[string]json.RawMessage
	return json.Unmarshal(data, &object) == nil && object != nil
}
