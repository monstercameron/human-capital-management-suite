package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

// inputTokenCountRequest projects precisely the inference input, including
// SchemaFlux's output schema. Provider state and generation controls are absent.
type inputTokenCountRequest struct {
	Model             string           `json:"model"`
	Input             []requestMessage `json:"input"`
	Tools             []requestTool    `json:"tools,omitempty"`
	ToolChoice        string           `json:"tool_choice,omitempty"`
	ParallelToolCalls bool             `json:"parallel_tool_calls"`
	Text              *requestText     `json:"text,omitempty"`
}

func (a *Adapter) checkInputTokenLimit(ctx context.Context, req agentmodel.ModelRequest) (agentmodel.ModelResult, error) {
	if req.Limits.MaxInputTokens <= 0 {
		return failureResult(a.identity, agentmodel.FailureInvalid, false, "input budget is required"), ErrInvalidResponse
	}
	inference := makeRequest(providerModelID(a.identity), req)
	body, err := json.Marshal(inputTokenCountRequest{Model: inference.Model, Input: inference.Input, Tools: inference.Tools, ToolChoice: inference.ToolChoice, ParallelToolCalls: inference.ParallelToolCalls, Text: inference.Text})
	if err != nil {
		return failureResult(a.identity, agentmodel.FailureInvalid, false, "input count request is invalid"), ErrInvalidResponse
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint+"/input_tokens", bytes.NewReader(body))
	if err != nil {
		return failureResult(a.identity, agentmodel.FailureInvalid, false, "input count request is invalid"), ErrInvalidResponse
	}
	request.Header.Set("Authorization", "Bearer "+a.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Client-Request-Id", requestIdentity(body, req.TraceID+":input-count"))
	response, err := a.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return agentmodel.ModelResult{ContractVersion: agentmodel.ContractVersion, Provider: a.identity, Finish: agentmodel.FinishCancelled}, ctx.Err()
		}
		return failureResult(a.identity, agentmodel.FailureUnavailable, true, "input count unavailable"), providerError{code: agentmodel.FailureUnavailable, retryable: true, message: "input count unavailable"}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return httpFailure(a.identity, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(data) > 4096 {
		return failureResult(a.identity, agentmodel.FailureInvalid, false, "input count response is invalid"), ErrInvalidResponse
	}
	var count struct {
		Object      string `json:"object"`
		InputTokens *int64 `json:"input_tokens"`
	}
	if json.Unmarshal(data, &count) != nil || count.Object != "response.input_tokens" || count.InputTokens == nil || *count.InputTokens < 0 {
		return failureResult(a.identity, agentmodel.FailureInvalid, false, "input count response is invalid"), ErrInvalidResponse
	}
	if *count.InputTokens > req.Limits.MaxInputTokens {
		return agentmodel.ModelResult{ContractVersion: agentmodel.ContractVersion, Provider: a.identity, Finish: agentmodel.FinishBlocked, Refusal: &agentmodel.ModelRefusal{Code: agentmodel.RefusalFeature, Detail: "input token count exceeds approved limit"}}, nil
	}
	return agentmodel.ModelResult{}, nil
}
