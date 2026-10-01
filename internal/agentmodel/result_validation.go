package agentmodel

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrInvalidModelResult marks an inconsistent or untrusted adapter result.
	ErrInvalidModelResult = errors.New("agentmodel: invalid model result")
)

// ValidateModelResult checks an adapter result against the request and the
// adapter's declared capabilities. Structured JSON is checked for syntax and
// consistency only; the owning HCM output validator must still check its schema.
func ValidateModelResult(req ModelRequest, result ModelResult, caps AdapterCapabilities) error {
	if err := ValidateModelRequest(req); err != nil {
		return err
	}
	if refusal := CheckCapabilities(req, caps); refusal != nil {
		return refusal
	}
	if result.ContractVersion != ContractVersion {
		return invalidResult("contract version %d is unsupported", result.ContractVersion)
	}
	if !validResultIdentity(result.Provider) {
		return invalidResult("provider identity is incomplete")
	}
	if err := validateUsage(req.Limits, result.Usage); err != nil {
		return err
	}
	if result.Failure != nil {
		return validateFailure(result)
	}
	if result.Refusal != nil {
		return validateRefusal(result)
	}
	if result.Finish == "" || !validFinishReason(result.Finish) {
		return invalidResult("unknown or empty finish reason %q", result.Finish)
	}
	if result.Finish == FinishCancelled {
		if hasOutput(result) {
			return invalidResult("cancelled result carries output")
		}
		return nil
	}
	if result.Finish == FinishBlocked {
		return invalidResult("blocked result has no typed refusal")
	}
	actionDenied := false
	if req.ActionPolicy != nil {
		allowed, err := CheckRequestedActions(*req.ActionPolicy, result.RequestedActions)
		if err != nil {
			return err
		}
		actionDenied = !allowed
		if actionDenied && (result.Finish != FinishComplete || hasOutput(result)) {
			return invalidResult("disallowed typed action carries executable output")
		}
	} else if len(result.RequestedActions) != 0 {
		return invalidResult("requested actions were not requested")
	}
	if err := validateStructuredOutput(req, result, caps); err != nil {
		return err
	}
	if err := validateProposals(req, result, caps); err != nil {
		return err
	}
	if result.Finish == FinishToolCalls && len(result.ToolProposals) == 0 {
		return invalidResult("tool-call finish has no tool proposals")
	}
	if result.Finish != FinishToolCalls && len(result.ToolProposals) > 0 {
		return invalidResult("tool proposals require a tool-call finish")
	}
	if result.Finish == FinishComplete && strings.TrimSpace(result.Text) == "" && len(result.Structured) == 0 && len(result.ToolProposals) == 0 && !actionDenied {
		return invalidResult("complete result has no output")
	}
	return nil
}

func validateUsage(limits ModelLimits, usage ModelUsage) error {
	if usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.TotalTokens < 0 || usage.CostMicros < 0 || usage.CachedInputTokens < 0 || usage.CachedInputTokens > usage.InputTokens {
		return invalidResult("usage cannot be negative")
	}
	if usage.InputTokens > usage.TotalTokens || usage.OutputTokens > usage.TotalTokens-usage.InputTokens {
		return invalidResult("total token usage is below input plus output usage")
	}
	if limits.MaxInputTokens > 0 && usage.InputTokens > limits.MaxInputTokens {
		return invalidResult("input token usage exceeds the request limit")
	}
	if limits.MaxOutputTokens > 0 && usage.OutputTokens > limits.MaxOutputTokens {
		return invalidResult("output token usage exceeds the request limit")
	}
	if limits.MaxCostMicros > 0 && usage.CostMicros > limits.MaxCostMicros {
		return invalidResult("cost usage exceeds the request limit")
	}
	return nil
}

func validateFailure(result ModelResult) error {
	if !validFailureCode(result.Failure.Code) {
		return invalidResult("unknown failure code %q", result.Failure.Code)
	}
	if result.Finish != "" || result.Refusal != nil || hasOutput(result) {
		return invalidResult("failure result also carries a finish, refusal, or output")
	}
	return nil
}

func validateRefusal(result ModelResult) error {
	if !validRefusalCode(result.Refusal.Code) {
		return invalidResult("unknown refusal code %q", result.Refusal.Code)
	}
	if result.Finish != FinishBlocked || result.Failure != nil || hasOutput(result) {
		return invalidResult("refusal must be blocked and cannot carry output or a failure")
	}
	return nil
}

func validateStructuredOutput(req ModelRequest, result ModelResult, caps AdapterCapabilities) error {
	structuredMode := req.Output.Mode == OutputJSON || req.Output.Mode == OutputSchema
	if len(result.Structured) > 0 {
		if !structuredMode || !caps.Supports(FeatureStructuredJSON) {
			return invalidResult("structured output was not requested or advertised")
		}
		if !json.Valid(result.Structured) {
			return invalidResult("structured output is not valid JSON")
		}
	}
	if result.Text != "" && structuredMode && !json.Valid([]byte(result.Text)) {
		return invalidResult("text for a structured request is not valid JSON")
	}
	if len(result.Structured) > 0 && result.Text != "" && !bytes.Equal(bytes.TrimSpace(result.Structured), bytes.TrimSpace([]byte(result.Text))) {
		return invalidResult("text and structured output disagree")
	}
	return nil
}

func validateProposals(req ModelRequest, result ModelResult, caps AdapterCapabilities) error {
	proposals := result.ToolProposals
	if len(proposals) == 0 {
		return nil
	}
	if len(req.Tools) == 0 || !caps.Supports(FeatureTools) {
		return invalidResult("adapter proposed a tool that was not enabled")
	}
	if len(proposals) > 1 && !caps.Supports(FeatureParallelTools) {
		return invalidResult("adapter proposed parallel tools without advertising support")
	}
	allowed := make(map[string]struct{}, len(req.Tools))
	for _, tool := range req.Tools {
		allowed[tool.Name] = struct{}{}
	}
	seenIDs := make(map[string]struct{}, len(proposals))
	for i, proposal := range proposals {
		if strings.TrimSpace(proposal.ID) == "" || strings.TrimSpace(proposal.Name) == "" || !jsonObject(proposal.Arguments) {
			return invalidResult("tool proposal %d has an invalid ID, name, or arguments object", i)
		}
		if _, ok := allowed[proposal.Name]; !ok {
			return invalidResult("tool proposal %d names an unrequested tool", i)
		}
		if _, duplicate := seenIDs[proposal.ID]; duplicate {
			return invalidResult("tool proposal ID %q is repeated", proposal.ID)
		}
		seenIDs[proposal.ID] = struct{}{}
	}
	return nil
}

func validFinishReason(reason FinishReason) bool {
	switch reason {
	case FinishComplete, FinishToolCalls, FinishLength, FinishBlocked, FinishCancelled:
		return true
	default:
		return false
	}
}

func validResultIdentity(identity ModelIdentity) bool {
	return strings.TrimSpace(identity.ProviderID) != "" && strings.TrimSpace(identity.ModelID) != "" && strings.TrimSpace(identity.Version) != ""
}

func validFailureCode(code FailureCode) bool {
	switch code {
	case FailureUnavailable, FailureTimeout, FailureInvalid, FailureLimit, FailureInternal:
		return true
	default:
		return false
	}
}

func validRefusalCode(code RefusalCode) bool {
	switch code {
	case RefusalContractVersion, RefusalFeature, RefusalOutputMode, RefusalToolLimit, RefusalContentPolicy:
		return true
	default:
		return false
	}
}

func jsonObject(data []byte) bool {
	var object map[string]json.RawMessage
	return json.Unmarshal(data, &object) == nil && object != nil
}

func hasOutput(result ModelResult) bool {
	return result.Text != "" || len(result.Structured) > 0 || len(result.ToolProposals) > 0
}

func invalidResult(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidModelResult, fmt.Sprintf(format, args...))
}
