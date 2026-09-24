package channelparity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type IntentCreationChannel string

const (
	CreateDesktop               IntentCreationChannel = "desktop"
	CreateMobile                IntentCreationChannel = "mobile"
	CreateKiosk                 IntentCreationChannel = "kiosk"
	CreateAccessibilityAssisted IntentCreationChannel = "accessibility-assisted"
)

func IntentCreationChannels() []IntentCreationChannel {
	return []IntentCreationChannel{CreateDesktop, CreateMobile, CreateKiosk, CreateAccessibilityAssisted}
}

type CapabilityRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}
type TrustedContext struct {
	TenantID    string        `json:"tenantId"`
	PrincipalID string        `json:"principalId"`
	Purpose     string        `json:"purpose"`
	Capability  CapabilityRef `json:"capability"`
}
type IntentCreationInput struct {
	IntentType string         `json:"intentType"`
	Fields     map[string]any `json:"fields"`
}
type CreationAssistance struct {
	Kind    string `json:"kind"`
	ActorID string `json:"actorId"`
}
type ChannelAdaptation struct {
	InputMode    string              `json:"inputMode"`
	Confirmation string              `json:"confirmation"`
	Simulation   string              `json:"simulation"`
	Assistance   *CreationAssistance `json:"assistance,omitempty"`
	Handoff      *SafeHandoff        `json:"handoff,omitempty"`
}
type SafeHandoff struct {
	Kind           string `json:"kind"`
	Reference      string `json:"reference"`
	AuthorityToken bool   `json:"authorityToken"`
}
type NormalizedIntent struct {
	IntentType     string         `json:"intentType"`
	Fields         map[string]any `json:"fields"`
	Capability     CapabilityRef  `json:"capability"`
	TrustedContext TrustedContext `json:"trustedContext"`
}
type IntentCreatedResult struct {
	Status     string        `json:"status"`
	IntentType string        `json:"intentType"`
	Capability CapabilityRef `json:"capability"`
}
type CreationEvidence struct {
	ConfirmationRequired bool `json:"confirmationRequired"`
	SimulationAvailable  bool `json:"simulationAvailable"`
}
type IntentCreationError struct {
	Code    string   `json:"code"`
	Fields  []string `json:"fields"`
	Message string   `json:"message"`
}
type IntentCreationOutcome struct {
	OK               bool                  `json:"ok"`
	Channel          IntentCreationChannel `json:"channel"`
	NormalizedIntent *NormalizedIntent     `json:"normalizedIntent,omitempty"`
	RequestDigest    string                `json:"requestDigest"`
	Result           *IntentCreatedResult  `json:"result,omitempty"`
	Adaptation       ChannelAdaptation     `json:"adaptation"`
	Evidence         *CreationEvidence     `json:"evidence,omitempty"`
	Error            *IntentCreationError  `json:"error,omitempty"`
}

var requiredCreationFields = map[string][]string{"leave-request": {"startDate", "endDate", "reason"}}
var ErrInvalidCreationChannel = errors.New("invalid intent creation channel")

// CreateIntent keeps the business payload and digest identical across all
// presentation channels. Channel-specific behavior is output metadata only.
func CreateIntent(channel IntentCreationChannel, input IntentCreationInput, context TrustedContext, assistance *CreationAssistance) (IntentCreationOutcome, error) {
	adapted, err := creationAdaptation(channel, assistance)
	if err != nil {
		return IntentCreationOutcome{}, err
	}
	digest, err := creationDigest(input, context)
	if err != nil {
		return IntentCreationOutcome{}, fmt.Errorf("channel parity: canonicalize intent: %w", err)
	}
	out := IntentCreationOutcome{Channel: channel, RequestDigest: digest, Adaptation: adapted}
	if strings.TrimSpace(context.TenantID) == "" || strings.TrimSpace(context.PrincipalID) == "" || strings.TrimSpace(context.Purpose) == "" || strings.TrimSpace(context.Capability.ID) == "" || strings.TrimSpace(context.Capability.Version) == "" {
		out.Error = &IntentCreationError{Code: "INVALID_CONTEXT", Fields: []string{"trustedContext"}, Message: "trusted context is required"}
		return out, nil
	}
	required := requiredCreationFields[input.IntentType]
	missing := make([]string, 0, len(required))
	for _, field := range required {
		value, present := input.Fields[field]
		if !present || value == nil || blankString(value) {
			missing = append(missing, field)
		}
	}
	if strings.TrimSpace(input.IntentType) == "" || len(missing) > 0 {
		if len(missing) == 0 {
			missing = append(missing, "intentType")
		}
		out.Error = &IntentCreationError{Code: "INVALID_REQUIRED_INPUT", Fields: missing, Message: "required input is missing"}
		return out, nil
	}
	fields, err := cloneJSONMap(input.Fields)
	if err != nil {
		return IntentCreationOutcome{}, fmt.Errorf("channel parity: normalize fields: %w", err)
	}
	capability := context.Capability
	contextCopy := context
	contextCopy.Capability = capability
	out.OK = true
	out.NormalizedIntent = &NormalizedIntent{IntentType: input.IntentType, Fields: fields, Capability: capability, TrustedContext: contextCopy}
	out.Result = &IntentCreatedResult{Status: "created", IntentType: input.IntentType, Capability: capability}
	out.Evidence = &CreationEvidence{ConfirmationRequired: true, SimulationAvailable: true}
	return out, nil
}

func creationAdaptation(channel IntentCreationChannel, assistance *CreationAssistance) (ChannelAdaptation, error) {
	var out ChannelAdaptation
	switch channel {
	case CreateDesktop:
		out = ChannelAdaptation{InputMode: "pointer-keyboard", Confirmation: "standard", Simulation: "available"}
	case CreateMobile:
		out = ChannelAdaptation{InputMode: "touch", Confirmation: "large-targets", Simulation: "available"}
	case CreateKiosk:
		out = ChannelAdaptation{InputMode: "guided-touch", Confirmation: "guided", Simulation: "available", Handoff: &SafeHandoff{Kind: "safe-resume", Reference: "resume:opaque", AuthorityToken: false}}
	case CreateAccessibilityAssisted:
		out = ChannelAdaptation{InputMode: "screen-reader", Confirmation: "announced", Simulation: "available"}
		if assistance != nil {
			copyValue := *assistance
			out.Assistance = &copyValue
		}
	default:
		return ChannelAdaptation{}, fmt.Errorf("%w: %q", ErrInvalidCreationChannel, channel)
	}
	return out, nil
}
func creationDigest(input IntentCreationInput, context TrustedContext) (string, error) {
	fields := input.Fields
	if fields == nil {
		fields = map[string]any{}
	}
	value := map[string]any{"intentType": input.IntentType, "fields": fields, "capability": context.Capability, "tenantId": context.TenantID, "purpose": context.Purpose}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return "", err
	}
	canonical := bytes.TrimSuffix(b.Bytes(), []byte("\n"))
	h := sha256.Sum256(canonical)
	return hex.EncodeToString(h[:]), nil
}
func cloneJSONMap(value map[string]any) (map[string]any, error) {
	if value == nil {
		return map[string]any{}, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var copyValue map[string]any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&copyValue); err != nil {
		return nil, err
	}
	if copyValue == nil {
		return map[string]any{}, nil
	}
	return copyValue, nil
}
func blankString(value any) bool {
	text, ok := value.(string)
	return ok && strings.TrimSpace(text) == ""
}
