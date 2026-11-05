package agentmodel

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// DecisionContractVersion identifies the stable provider-neutral decision
// contract. It sits beside the generation contract: generation writes text,
// the decision port answers typed questions.
const DecisionContractVersion = 1

var (
	// ErrInvalidDecisionRequest marks a malformed provider-neutral decision request.
	ErrInvalidDecisionRequest = errors.New("agentmodel: invalid decision request")
	// ErrInvalidDecisionResult marks a decision result that disagrees with its request.
	ErrInvalidDecisionResult = errors.New("agentmodel: invalid decision result")
)

// DecisionQuestion is the shape of a typed question.
type DecisionQuestion string

const (
	// DecisionBoolean asks yes or no.
	DecisionBoolean DecisionQuestion = "boolean"
	// DecisionChoice asks for one of N named options.
	DecisionChoice DecisionQuestion = "choice"
	// DecisionScore asks for a number between 0 and 1.
	DecisionScore DecisionQuestion = "score"
)

// MaxDecisionOptions caps the options of one question so a "bounded set"
// stays bounded.
const MaxDecisionOptions = 32

// DecisionRequest is one typed question to a decision model. It carries no
// provider session or conversation identifier; run identity stays with the run
// record, which seals the question, the options, the probabilities and the
// threshold after the call.
type DecisionRequest struct {
	ContractVersion int              `json:"contract_version"`
	Point           string           `json:"point"`
	Question        DecisionQuestion `json:"question"`
	Options         []string         `json:"options,omitempty"`
	// State is the content the question is asked about. The port leaves it
	// optional so sealed replays can carry the digest alone; adapters that
	// must send content refuse an empty state.
	State         string            `json:"state,omitempty"`
	Criteria      map[string]string `json:"criteria,omitempty"`
	ContextDigest string            `json:"context_digest,omitempty"`
	Threshold     float64           `json:"threshold"`
	TraceID       string            `json:"trace_id"`
	Limits        ModelLimits       `json:"limits"`
	Processing    ProcessingPolicy  `json:"processing"`
}

// ValidateDecisionRequest checks structural invariants before dispatch. A
// question without a threshold is refused: there is no decision the product
// takes at any confidence.
func ValidateDecisionRequest(req DecisionRequest) error {
	if req.ContractVersion != DecisionContractVersion {
		return fmt.Errorf("%w: contract version %d is unsupported", ErrInvalidDecisionRequest, req.ContractVersion)
	}
	if strings.TrimSpace(req.Point) == "" || strings.TrimSpace(req.TraceID) == "" {
		return fmt.Errorf("%w: decision point and trace ID are required", ErrInvalidDecisionRequest)
	}
	if req.Threshold <= 0 || req.Threshold > 1 {
		return fmt.Errorf("%w: threshold must be within (0,1]", ErrInvalidDecisionRequest)
	}
	switch req.Question {
	case DecisionBoolean:
		if len(req.Options) != 0 && (len(req.Options) != 2 || req.Options[0] != "no" || req.Options[1] != "yes") {
			return fmt.Errorf("%w: a yes-or-no question takes no options or [no yes]", ErrInvalidDecisionRequest)
		}
	case DecisionChoice:
		if len(req.Options) < 2 || len(req.Options) > MaxDecisionOptions {
			return fmt.Errorf("%w: a choice needs 2 to %d options", ErrInvalidDecisionRequest, MaxDecisionOptions)
		}
		for i, option := range req.Options {
			if strings.TrimSpace(option) == "" {
				return fmt.Errorf("%w: option %d is blank", ErrInvalidDecisionRequest, i)
			}
		}
	case DecisionScore:
		if len(req.Options) != 0 {
			return fmt.Errorf("%w: a score takes no options", ErrInvalidDecisionRequest)
		}
	default:
		return fmt.Errorf("%w: unknown question %q", ErrInvalidDecisionRequest, req.Question)
	}
	if req.Limits.MaxInputTokens < 0 || req.Limits.MaxOutputTokens < 0 || req.Limits.MaxCostMicros < 0 {
		return fmt.Errorf("%w: token and cost limits cannot be negative", ErrInvalidDecisionRequest)
	}
	if strings.TrimSpace(req.Processing.Residency) == "" || strings.TrimSpace(req.Processing.Retention) == "" {
		return fmt.Errorf("%w: processing residency and retention are required", ErrInvalidDecisionRequest)
	}
	if !validProcessingUse(req.Processing.TrainingUse) || req.Processing.TrainingUse == UseUnspecified ||
		!validProcessingUse(req.Processing.Logging) || req.Processing.Logging == UseUnspecified {
		return fmt.Errorf("%w: training-use and logging policy must be explicit", ErrInvalidDecisionRequest)
	}
	return nil
}

// DecisionResult is a provider-neutral answer: the chosen option, the
// per-option probabilities, and a confidence. Abstained is true when the
// answer fell below the threshold; then EscalateTo names where the question
// went instead ("model" or "person") and Chosen is -1.
type DecisionResult struct {
	ContractVersion int           `json:"contract_version"`
	Chosen          int           `json:"chosen"`
	Score           float64       `json:"score,omitempty"`
	Probabilities   []float64     `json:"probabilities,omitempty"`
	Confidence      float64       `json:"confidence"`
	Abstained       bool          `json:"abstained"`
	EscalateTo      string        `json:"escalate_to,omitempty"`
	Provider        ModelIdentity `json:"provider"`
	Usage           ModelUsage    `json:"usage"`
	LatencyMillis   int64         `json:"latency_ms"`
}

// ValidateDecisionResult refuses a result that disagrees with its request:
// a choice outside the options, probabilities that do not cover the options,
// or a score outside 0 to 1.
func ValidateDecisionResult(req DecisionRequest, result DecisionResult) error {
	if result.ContractVersion != DecisionContractVersion {
		return fmt.Errorf("%w: contract version %d is unsupported", ErrInvalidDecisionResult, result.ContractVersion)
	}
	if result.Abstained {
		if result.Chosen != -1 && result.Chosen != 0 {
			return fmt.Errorf("%w: an abstention names no choice", ErrInvalidDecisionResult)
		}
		if result.EscalateTo != "model" && result.EscalateTo != "person" {
			return fmt.Errorf("%w: an abstention must escalate to the model or a person", ErrInvalidDecisionResult)
		}
		return nil
	}
	if result.EscalateTo != "" {
		return fmt.Errorf("%w: an answer escalates nowhere", ErrInvalidDecisionResult)
	}
	switch req.Question {
	case DecisionBoolean:
		options := req.Options
		if len(options) == 0 {
			options = []string{"no", "yes"}
		}
		if result.Chosen < 0 || result.Chosen >= len(options) {
			return fmt.Errorf("%w: choice %d is outside the options", ErrInvalidDecisionResult, result.Chosen)
		}
		if len(result.Probabilities) != 0 && len(result.Probabilities) != len(options) {
			return fmt.Errorf("%w: probabilities do not cover the options", ErrInvalidDecisionResult)
		}
	case DecisionChoice:
		if result.Chosen < 0 || result.Chosen >= len(req.Options) {
			return fmt.Errorf("%w: choice %d is outside the options", ErrInvalidDecisionResult, result.Chosen)
		}
		if len(result.Probabilities) != 0 && len(result.Probabilities) != len(req.Options) {
			return fmt.Errorf("%w: probabilities do not cover the options", ErrInvalidDecisionResult)
		}
	case DecisionScore:
		if result.Score < 0 || result.Score > 1 {
			return fmt.Errorf("%w: score %v is outside 0 to 1", ErrInvalidDecisionResult, result.Score)
		}
	default:
		return fmt.Errorf("%w: unknown question %q", ErrInvalidDecisionResult, req.Question)
	}
	if result.Confidence < 0 || result.Confidence > 1 {
		return fmt.Errorf("%w: confidence %v is outside 0 to 1", ErrInvalidDecisionResult, result.Confidence)
	}
	return nil
}

// ApplyDecisionThreshold abstains when the confidence falls below the
// request's threshold, keeping the probabilities so the escalation can see
// how close the call was. The escalation target comes from the decision
// point's record, defaulting to the general model.
func ApplyDecisionThreshold(req DecisionRequest, result DecisionResult) DecisionResult {
	if result.Abstained || result.Confidence >= req.Threshold {
		return result
	}
	escalate := "model"
	for _, point := range Agentcost002DecisionPoints() {
		if point.ID == req.Point {
			escalate = point.Escalation
		}
	}
	result.Abstained, result.Chosen, result.EscalateTo = true, -1, escalate
	return result
}

// DecisionSeal is what the run record keeps for a decision: the question, the
// options, the probabilities and the threshold. It carries no content, only
// the shape of what was asked and answered.
type DecisionSeal struct {
	Point         string    `json:"point"`
	Question      string    `json:"question"`
	Options       []string  `json:"options"`
	Probabilities []float64 `json:"probabilities"`
	Threshold     float64   `json:"threshold"`
	Chosen        int       `json:"chosen"`
	Abstained     bool      `json:"abstained"`
}

// SealDecision builds the run record's evidence for a decision call.
func SealDecision(req DecisionRequest, result DecisionResult) DecisionSeal {
	options := req.Options
	if req.Question == DecisionBoolean && len(options) == 0 {
		options = []string{"no", "yes"}
	}
	return DecisionSeal{Point: req.Point, Question: string(req.Question), Options: options, Probabilities: result.Probabilities, Threshold: req.Threshold, Chosen: result.Chosen, Abstained: result.Abstained}
}

// DecisionAdapter answers one typed question. Implementations must abstain
// through ApplyDecisionThreshold rather than guessing below the threshold,
// and must never act on the answer themselves.
type DecisionAdapter interface {
	Ask(context.Context, DecisionRequest) (DecisionResult, error)
}
