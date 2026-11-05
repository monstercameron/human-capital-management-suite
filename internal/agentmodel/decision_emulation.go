package agentmodel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// EmulatedDecisionsAdapter is the decision port's first adapter: it answers
// typed questions through the existing structured-output route, so the port
// ships and decision accuracy can be measured before any preview access
// exists. It implements DecisionAdapter.
type EmulatedDecisionsAdapter struct {
	models ModelAdapter
	now    func() time.Time
}

// NewEmulatedDecisionsAdapter answers decisions through models.
func NewEmulatedDecisionsAdapter(models ModelAdapter) *EmulatedDecisionsAdapter {
	return &EmulatedDecisionsAdapter{models: models, now: time.Now}
}

var _ DecisionAdapter = (*EmulatedDecisionsAdapter)(nil)

// ErrDecisionEmulation marks a refusal to answer through the emulation.
var ErrDecisionEmulation = errors.New("agentmodel: decision emulation refused")

// Ask validates the question, asks the model for schema-shaped JSON, parses
// it strictly, and abstains below the threshold. Anything unexpected fails
// closed: an unparseable answer is an error, never a guess.
func (a *EmulatedDecisionsAdapter) Ask(ctx context.Context, req DecisionRequest) (DecisionResult, error) {
	if a == nil || a.models == nil {
		return DecisionResult{}, fmt.Errorf("%w: no model adapter", ErrDecisionEmulation)
	}
	if err := ValidateDecisionRequest(req); err != nil {
		return DecisionResult{}, err
	}
	if strings.TrimSpace(req.State) == "" {
		return DecisionResult{}, fmt.Errorf("%w: the emulation needs the content, not only its digest", ErrDecisionEmulation)
	}
	schema, err := emulationSchema(req)
	if err != nil {
		return DecisionResult{}, err
	}
	var message strings.Builder
	fmt.Fprintf(&message, "State: %s\nQuestion %s (%s).\n", req.State, req.Point, req.Question)
	if len(req.Options) > 0 {
		fmt.Fprintf(&message, "Options (%d): %s.\n", len(req.Options), strings.Join(req.Options, " | "))
	}
	message.WriteString("Answer with JSON matching the schema and nothing else.")
	result, err := a.models.Invoke(ctx, ModelRequest{
		ContractVersion: ContractVersion,
		TaskProfile:     "decision.emulation",
		ModelProfile:    "general",
		Messages: []ModelMessage{
			{Role: RoleSystem, Content: "Answer the typed question about the state. Reply with JSON only."},
			{Role: RoleUser, Content: message.String()},
		},
		Output:     OutputConstraint{Mode: OutputSchema, Schema: json.RawMessage(schema)},
		Deadline:   a.now().Add(30 * time.Second),
		Limits:     req.Limits,
		TraceID:    req.TraceID,
		Processing: req.Processing,
	})
	if err != nil {
		return DecisionResult{}, err
	}
	parsed, err := parseEmulatedAnswer(req, result.Structured)
	if err != nil {
		return DecisionResult{}, err
	}
	parsed.ContractVersion, parsed.Provider, parsed.Usage = DecisionContractVersion, result.Provider, result.Usage
	if err := ValidateDecisionResult(req, parsed); err != nil {
		return DecisionResult{}, err
	}
	return ApplyDecisionThreshold(req, parsed), nil
}

func emulationSchema(req DecisionRequest) (string, error) {
	number := `{"type":"number","minimum":0,"maximum":1}`
	switch req.Question {
	case DecisionBoolean:
		return fmt.Sprintf(`{"type":"object","properties":{"yes_probability":%s,"confidence":%s},"required":["yes_probability","confidence"],"additionalProperties":false}`, number, number), nil
	case DecisionChoice:
		return fmt.Sprintf(`{"type":"object","properties":{"choice":{"type":"integer","minimum":0,"maximum":%d},"probabilities":{"type":"array","items":%s,"minItems":%d,"maxItems":%d},"confidence":%s},"required":["choice","probabilities","confidence"],"additionalProperties":false}`, len(req.Options)-1, number, len(req.Options), len(req.Options), number), nil
	case DecisionScore:
		return fmt.Sprintf(`{"type":"object","properties":{"score":%s,"confidence":%s},"required":["score","confidence"],"additionalProperties":false}`, number, number), nil
	default:
		return "", fmt.Errorf("%w: unknown question %q", ErrDecisionEmulation, req.Question)
	}
}

func parseEmulatedAnswer(req DecisionRequest, raw json.RawMessage) (DecisionResult, error) {
	if !json.Valid(raw) {
		return DecisionResult{}, fmt.Errorf("%w: the model did not return JSON", ErrDecisionEmulation)
	}
	var answer struct {
		YesProbability *float64  `json:"yes_probability"`
		Choice         *int      `json:"choice"`
		Score          *float64  `json:"score"`
		Probabilities  []float64 `json:"probabilities"`
		Confidence     *float64  `json:"confidence"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return DecisionResult{}, fmt.Errorf("%w: %v", ErrDecisionEmulation, err)
	}
	if answer.Confidence == nil {
		return DecisionResult{}, fmt.Errorf("%w: confidence is missing", ErrDecisionEmulation)
	}
	result := DecisionResult{Confidence: *answer.Confidence}
	switch req.Question {
	case DecisionBoolean:
		if answer.YesProbability == nil {
			return DecisionResult{}, fmt.Errorf("%w: yes_probability is missing", ErrDecisionEmulation)
		}
		probability := *answer.YesProbability
		result.Chosen = 0
		if probability >= 0.5 {
			result.Chosen = 1
		}
		result.Probabilities = []float64{1 - probability, probability}
	case DecisionChoice:
		if answer.Choice == nil {
			return DecisionResult{}, fmt.Errorf("%w: choice is missing", ErrDecisionEmulation)
		}
		result.Chosen, result.Probabilities = *answer.Choice, answer.Probabilities
	case DecisionScore:
		if answer.Score == nil {
			return DecisionResult{}, fmt.Errorf("%w: score is missing", ErrDecisionEmulation)
		}
		result.Score = *answer.Score
	default:
		return DecisionResult{}, fmt.Errorf("%w: unknown question %q", ErrDecisionEmulation, req.Question)
	}
	return result, nil
}

// FlaggedDecisionAdapter puts a preview adapter behind a feature flag with a
// fallback. With the flag off the fallback answers; with it on the primary
// answers, and a primary failure falls back rather than failing the run.
// Nothing in the product may require the primary: the fallback is the
// shippable path.
type FlaggedDecisionAdapter struct {
	Primary  DecisionAdapter
	Fallback DecisionAdapter
	Enabled  func() bool
}

var _ DecisionAdapter = (*FlaggedDecisionAdapter)(nil)

// ErrDecisionUnavailable marks a decision that neither adapter could answer.
var ErrDecisionUnavailable = errors.New("agentmodel: no decision adapter answered")

// Ask routes to the primary or the fallback as above.
func (a *FlaggedDecisionAdapter) Ask(ctx context.Context, req DecisionRequest) (DecisionResult, error) {
	if a == nil || a.Fallback == nil {
		return DecisionResult{}, ErrDecisionUnavailable
	}
	if a.Primary == nil || a.Enabled == nil || !a.Enabled() {
		return a.Fallback.Ask(ctx, req)
	}
	result, err := a.Primary.Ask(ctx, req)
	if err != nil {
		return a.Fallback.Ask(ctx, req)
	}
	return result, nil
}
