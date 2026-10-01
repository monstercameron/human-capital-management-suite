package agentmodel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// ContractVersion identifies the stable provider-neutral model wire contract.
const ContractVersion = 1

var (
	// ErrInvalidModelRequest marks a malformed provider-neutral model request.
	ErrInvalidModelRequest = errors.New("agentmodel: invalid model request")
	// ErrUnsupportedModelFeature marks a request the adapter cannot honor.
	ErrUnsupportedModelFeature = errors.New("agentmodel: unsupported model feature")
)

// ModelRequest is one bounded inference request. Run identity and transcript
// ownership remain with HCM Next; this request deliberately has no provider
// session or conversation identifier.
type ModelRequest struct {
	ContractVersion  int                    `json:"contract_version"`
	TaskProfile      string                 `json:"task_profile"`
	ModelProfile     string                 `json:"model_profile"`
	Messages         []ModelMessage         `json:"messages"`
	ContextRefs      []ContextReference     `json:"context_refs,omitempty"`
	Tools            []ToolSchema           `json:"tools,omitempty"`
	Output           OutputConstraint       `json:"output"`
	Deadline         time.Time              `json:"deadline"`
	Limits           ModelLimits            `json:"limits"`
	TraceID          string                 `json:"trace_id"`
	Processing       ProcessingPolicy       `json:"processing"`
	RequiredFeatures []ModelFeature         `json:"required_features,omitempty"`
	ActionPolicy     *RequestedActionPolicy `json:"action_policy,omitempty"`
}

// ModelMessage is an approved conversation item supplied by HCM Next.
type ModelMessage struct {
	Role          MessageRole     `json:"role"`
	Content       string          `json:"content"`
	ToolCallID    string          `json:"tool_call_id,omitempty"`
	ToolName      string          `json:"tool_name,omitempty"`
	ToolArguments json.RawMessage `json:"tool_arguments,omitempty"`
}

// MessageRole is a provider-neutral message role.
type MessageRole string

const (
	// RoleSystem identifies an approved system instruction.
	RoleSystem MessageRole = "system"
	// RoleDeveloper identifies an approved developer instruction.
	RoleDeveloper MessageRole = "developer"
	// RoleUser identifies content supplied by a user.
	RoleUser MessageRole = "user"
	// RoleAssistant identifies prior assistant output owned by HCM Next.
	RoleAssistant MessageRole = "assistant"
	// RoleTool identifies a tool result returned to the model after HCM Next
	// executes and authorizes the matching assistant tool call.
	RoleTool MessageRole = "tool"
)

// ContextReference identifies an HCM-owned context item without embedding
// provider-side state or granting the model new authority.
type ContextReference struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

// ToolSchema declares a tool that the model may propose for HCM Next to review.
type ToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// OutputConstraint declares the required output representation and schema.
type OutputConstraint struct {
	Mode   OutputMode      `json:"mode"`
	Schema json.RawMessage `json:"schema,omitempty"`
}

// OutputMode selects the provider-neutral output shape.
type OutputMode string

const (
	// OutputText requests unstructured text.
	OutputText OutputMode = "text"
	// OutputJSON requests syntactically valid JSON output.
	OutputJSON OutputMode = "json"
	// OutputSchema requests JSON output constrained by the supplied schema.
	OutputSchema OutputMode = "schema"
)

// ModelLimits bounds provider work requested for this call.
type ModelLimits struct {
	MaxInputTokens  int64 `json:"max_input_tokens"`
	MaxOutputTokens int64 `json:"max_output_tokens"`
	MaxCostMicros   int64 `json:"max_cost_micros"`
}

// ProcessingPolicy states the required handling conditions for request data.
type ProcessingPolicy struct {
	Residency   string        `json:"residency"`
	Retention   string        `json:"retention"`
	TrainingUse ProcessingUse `json:"training_use"`
	Logging     ProcessingUse `json:"logging"`
}

// ProcessingUse declares whether a processing behavior is required or denied.
type ProcessingUse string

const (
	// UseUnspecified indicates that a policy was not selected; request validation refuses it.
	UseUnspecified ProcessingUse = "unspecified"
	// UseAllowed explicitly permits the named processing behavior.
	UseAllowed ProcessingUse = "allowed"
	// UseDenied explicitly prohibits the named processing behavior.
	UseDenied ProcessingUse = "denied"
)

// ModelFeature is a stable feature identifier independent of provider syntax.
type ModelFeature string

const (
	// FeatureTools indicates the adapter accepts HCM-owned tool schemas.
	FeatureTools ModelFeature = "tools"
	// FeatureParallelTools indicates the adapter can request multiple tools together.
	FeatureParallelTools ModelFeature = "parallel_tools"
	// FeatureStructuredJSON indicates the adapter can honor JSON output constraints.
	FeatureStructuredJSON ModelFeature = "structured_json"
	// FeatureStreaming indicates the adapter can emit provisional output chunks.
	FeatureStreaming ModelFeature = "streaming"
)

// AdapterCapabilities advertises the features and output modes an adapter can
// honor. Capability data is descriptive; authorization remains outside it.
type AdapterCapabilities struct {
	ContractVersions []int          `json:"contract_versions"`
	Features         []ModelFeature `json:"features"`
	OutputModes      []OutputMode   `json:"output_modes"`
	MaxTools         int            `json:"max_tools"`
}

// ModelAdapter performs one inference request using the provider-neutral
// contract. Implementations must return ModelRefusal when they cannot preserve
// the requested semantics and must never execute ToolProposal values.
type ModelAdapter interface {
	Capabilities() AdapterCapabilities
	Invoke(context.Context, ModelRequest) (ModelResult, error)
}

// Supports reports whether the adapter advertises the requested feature.
func (c AdapterCapabilities) Supports(feature ModelFeature) bool {
	return slices.Contains(c.Features, feature)
}

// ValidateModelRequest checks structural invariants before adapter dispatch.
func ValidateModelRequest(req ModelRequest) error {
	if req.ContractVersion != ContractVersion {
		return fmt.Errorf("%w: contract version %d is unsupported", ErrInvalidModelRequest, req.ContractVersion)
	}
	if strings.TrimSpace(req.TaskProfile) == "" || strings.TrimSpace(req.ModelProfile) == "" || strings.TrimSpace(req.TraceID) == "" {
		return fmt.Errorf("%w: task profile, model profile, and trace ID are required", ErrInvalidModelRequest)
	}
	if req.ActionPolicy != nil && ValidateRequestedActionPolicy(*req.ActionPolicy) != nil {
		return ErrInvalidModelRequest
	}
	if len(req.Messages) == 0 || req.Deadline.IsZero() {
		return fmt.Errorf("%w: at least one message and a deadline are required", ErrInvalidModelRequest)
	}
	for i, message := range req.Messages {
		if !validModelMessage(message) {
			return fmt.Errorf("%w: message %d has an invalid role, content, or tool binding", ErrInvalidModelRequest, i)
		}
		if message.Role == RoleTool {
			if i == 0 || req.Messages[i-1].Role != RoleAssistant || req.Messages[i-1].ToolCallID != message.ToolCallID {
				return fmt.Errorf("%w: tool result %d does not follow its matching assistant call", ErrInvalidModelRequest, i)
			}
		}
		if message.Role == RoleAssistant && message.ToolCallID != "" {
			if i+1 >= len(req.Messages) || req.Messages[i+1].Role != RoleTool || req.Messages[i+1].ToolCallID != message.ToolCallID {
				return fmt.Errorf("%w: assistant tool call %d has no matching result", ErrInvalidModelRequest, i)
			}
		}
	}
	if req.Limits.MaxInputTokens < 0 || req.Limits.MaxOutputTokens < 0 || req.Limits.MaxCostMicros < 0 {
		return fmt.Errorf("%w: token and cost limits cannot be negative", ErrInvalidModelRequest)
	}
	if req.Output.Mode != OutputText && req.Output.Mode != OutputJSON && req.Output.Mode != OutputSchema {
		return fmt.Errorf("%w: unknown output mode %q", ErrInvalidModelRequest, req.Output.Mode)
	}
	if req.Output.Mode == OutputSchema && !json.Valid(req.Output.Schema) {
		return fmt.Errorf("%w: schema output requires valid JSON schema bytes", ErrInvalidModelRequest)
	}
	if req.Output.Mode != OutputSchema && len(req.Output.Schema) != 0 {
		return fmt.Errorf("%w: schema bytes are only valid for schema output", ErrInvalidModelRequest)
	}
	seenTools := make(map[string]struct{}, len(req.Tools))
	for i, tool := range req.Tools {
		if strings.TrimSpace(tool.Name) == "" || !json.Valid(tool.InputSchema) {
			return fmt.Errorf("%w: tool %d requires a name and valid JSON schema", ErrInvalidModelRequest, i)
		}
		if _, exists := seenTools[tool.Name]; exists {
			return fmt.Errorf("%w: duplicate tool name %q", ErrInvalidModelRequest, tool.Name)
		}
		seenTools[tool.Name] = struct{}{}
	}
	if strings.TrimSpace(req.Processing.Residency) == "" || strings.TrimSpace(req.Processing.Retention) == "" {
		return fmt.Errorf("%w: processing residency and retention are required", ErrInvalidModelRequest)
	}
	if !validProcessingUse(req.Processing.TrainingUse) || req.Processing.TrainingUse == UseUnspecified ||
		!validProcessingUse(req.Processing.Logging) || req.Processing.Logging == UseUnspecified {
		return fmt.Errorf("%w: training-use and logging policy must be explicit", ErrInvalidModelRequest)
	}
	return nil
}

// ModelRefusal is a typed, fail-closed refusal to handle a request.
type ModelRefusal struct {
	Code    RefusalCode  `json:"code"`
	Feature ModelFeature `json:"feature,omitempty"`
	Detail  string       `json:"detail,omitempty"`
}

// RefusalCode classifies a refusal without exposing provider wire errors.
type RefusalCode string

const (
	// RefusalContractVersion indicates the adapter does not implement the request's version.
	RefusalContractVersion RefusalCode = "contract_version"
	// RefusalFeature indicates a required behavior is not advertised.
	RefusalFeature RefusalCode = "unsupported_feature"
	// RefusalOutputMode indicates the requested output representation is unsupported.
	RefusalOutputMode RefusalCode = "unsupported_output_mode"
	// RefusalToolLimit indicates the request exceeds the adapter's declared tool ceiling.
	RefusalToolLimit RefusalCode = "tool_limit_exceeded"
	// RefusalContentPolicy indicates the model declined to answer under its content policy.
	RefusalContentPolicy RefusalCode = "content_policy"
)

// Error implements error while preserving the refusal's typed fields.
func (r ModelRefusal) Error() string {
	if r.Detail == "" {
		return fmt.Sprintf("%s: %s", ErrUnsupportedModelFeature, r.Code)
	}
	return fmt.Sprintf("%s: %s: %s", ErrUnsupportedModelFeature, r.Code, r.Detail)
}

// Unwrap lets callers classify refusals with errors.Is.
func (r ModelRefusal) Unwrap() error { return ErrUnsupportedModelFeature }

// CheckCapabilities returns a typed refusal when the adapter cannot preserve
// the request's declared semantics. It does not attempt a provider call.
func CheckCapabilities(req ModelRequest, caps AdapterCapabilities) *ModelRefusal {
	if !slices.Contains(caps.ContractVersions, req.ContractVersion) {
		return &ModelRefusal{Code: RefusalContractVersion, Detail: fmt.Sprintf("version %d is not advertised", req.ContractVersion)}
	}
	if !slices.Contains(caps.OutputModes, req.Output.Mode) {
		return &ModelRefusal{Code: RefusalOutputMode, Detail: string(req.Output.Mode)}
	}
	if len(req.Tools) > caps.MaxTools {
		return &ModelRefusal{Code: RefusalToolLimit, Feature: FeatureTools, Detail: fmt.Sprintf("requested %d tools, maximum is %d", len(req.Tools), caps.MaxTools)}
	}
	if len(req.Tools) > 0 && !caps.Supports(FeatureTools) {
		return &ModelRefusal{Code: RefusalFeature, Feature: FeatureTools}
	}
	for _, feature := range req.RequiredFeatures {
		if !caps.Supports(feature) {
			return &ModelRefusal{Code: RefusalFeature, Feature: feature}
		}
	}
	if (req.Output.Mode == OutputSchema || req.Output.Mode == OutputJSON) && !caps.Supports(FeatureStructuredJSON) {
		return &ModelRefusal{Code: RefusalFeature, Feature: FeatureStructuredJSON}
	}
	return nil
}

// ModelResult is a provider-neutral outcome. ProviderRequestID is diagnostic
// correlation only and is never an HCM run or conversation identity.
type ModelResult struct {
	ContractVersion   int               `json:"contract_version"`
	Text              string            `json:"text,omitempty"`
	Structured        json.RawMessage   `json:"structured,omitempty"`
	ToolProposals     []ToolProposal    `json:"tool_proposals,omitempty"`
	Usage             ModelUsage        `json:"usage"`
	Finish            FinishReason      `json:"finish"`
	Provider          ModelIdentity     `json:"provider"`
	ProviderRequestID string            `json:"provider_request_id,omitempty"`
	Refusal           *ModelRefusal     `json:"refusal,omitempty"`
	Failure           *ModelFailure     `json:"failure,omitempty"`
	RequestedActions  []RequestedAction `json:"requested_actions,omitempty"`
}

// ToolProposal is untrusted model output; HCM Next must validate and authorize
// it before execution.
type ToolProposal struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ModelUsage reports provider-neutral usage accounting.
type ModelUsage struct {
	CachedInputTokens int64 `json:"cached_input_tokens,omitempty"`
	InputTokens       int64 `json:"input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	TotalTokens       int64 `json:"total_tokens"`
	CostMicros        int64 `json:"cost_micros"`
}

// FinishReason describes why generation ended.
type FinishReason string

const (
	// FinishComplete indicates normal completion.
	FinishComplete FinishReason = "complete"
	// FinishToolCalls indicates one or more tool proposals ended generation.
	FinishToolCalls FinishReason = "tool_calls"
	// FinishLength indicates generation stopped at a token ceiling.
	FinishLength FinishReason = "length"
	// FinishBlocked indicates provider safety handling stopped generation.
	FinishBlocked FinishReason = "blocked"
	// FinishCancelled indicates cancellation stopped generation.
	FinishCancelled FinishReason = "cancelled"
)

// ModelIdentity is the approved provider and model version used for a call.
type ModelIdentity struct {
	ProviderID string `json:"provider_id"`
	ModelID    string `json:"model_id"`
	Version    string `json:"version"`
}

// ModelFailure is a normalized adapter failure, without provider wire details.
type ModelFailure struct {
	Code      FailureCode `json:"code"`
	Retryable bool        `json:"retryable"`
	Message   string      `json:"message,omitempty"`
}

// FailureCode is a stable provider-neutral failure category.
type FailureCode string

const (
	// FailureUnavailable indicates the adapter or endpoint was unavailable.
	FailureUnavailable FailureCode = "unavailable"
	// FailureTimeout indicates the model call exceeded its deadline.
	FailureTimeout FailureCode = "timeout"
	// FailureInvalid indicates the adapter received an invalid provider response.
	FailureInvalid FailureCode = "invalid_response"
	// FailureLimit indicates a provider or request limit prevented completion.
	FailureLimit FailureCode = "limit_exceeded"
	// FailureInternal indicates an adapter-side failure with no narrower category.
	FailureInternal FailureCode = "internal"
)

func validRole(role MessageRole) bool {
	return role == RoleSystem || role == RoleDeveloper || role == RoleUser || role == RoleAssistant || role == RoleTool
}

func validModelMessage(message ModelMessage) bool {
	if !validRole(message.Role) {
		return false
	}
	switch message.Role {
	case RoleTool:
		return strings.TrimSpace(message.ToolCallID) != "" && strings.TrimSpace(message.Content) != "" && message.ToolName == "" && len(message.ToolArguments) == 0
	case RoleAssistant:
		if message.ToolCallID != "" || message.ToolName != "" || len(message.ToolArguments) > 0 {
			return strings.TrimSpace(message.ToolCallID) != "" && strings.TrimSpace(message.ToolName) != "" && jsonObject(message.ToolArguments) && strings.TrimSpace(message.Content) == ""
		}
		return strings.TrimSpace(message.Content) != ""
	default:
		return strings.TrimSpace(message.Content) != "" && message.ToolCallID == "" && message.ToolName == "" && len(message.ToolArguments) == 0
	}
}

func validProcessingUse(value ProcessingUse) bool {
	return value == UseUnspecified || value == UseAllowed || value == UseDenied
}
