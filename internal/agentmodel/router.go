package agentmodel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

var (
	// ErrInvalidRoute marks malformed pins, policies or retry evidence.
	ErrInvalidRoute = errors.New("agentmodel: invalid route request")
	// ErrNoEligibleModel marks a pinned route with no qualified next model.
	ErrNoEligibleModel = errors.New("agentmodel: no eligible pinned model")
	// ErrFallbackDenied marks a fallback blocked by retry or effect policy.
	ErrFallbackDenied = errors.New("agentmodel: fallback denied")
	// ErrRouteRecord marks a decision that could not be durably recorded.
	ErrRouteRecord = errors.New("agentmodel: route record failed")
)

// ModelSelection binds an internal eligibility profile to an exact adapter model version.
type ModelSelection struct {
	ProfileID     string
	ProfileDigest string
	Identity      ModelIdentity
}

// ModelEvaluation captures the qualification evidence bound to an agent version.
type ModelEvaluation struct {
	AgentVersionDigest string
	SuiteDigest        string
	Passed             bool
}

// ModelProfile declares a qualified model version and its processing envelope.
type ModelProfile struct {
	ID                 string
	ProfileDigest      string
	Identity           ModelIdentity
	Regions            []string
	DataClasses        []string
	TaskProfileIDs     []string
	MaxLatency         time.Duration
	MaxCostMicros      int64
	ExpectedCostMicros int64
	SemanticsDigest    string
	OutputSchemaDigest string
	ToolSchemaDigest   string
	Evaluation         ModelEvaluation
}

// ModelPin fixes the ordered primary and fallback versions for an agent version.
type ModelPin struct {
	AgentVersionDigest string
	TaskProfileID      string
	Primary            ModelSelection
	Fallbacks          []ModelSelection
	SemanticsDigest    string
	OutputSchemaDigest string
	ToolSchemaDigest   string
}

// TaskProfile contains the processing and resource limits for one model task.
type TaskProfile struct {
	ID                 string
	AgentVersionDigest string
	Region             string
	DataClasses        []string
	MaxLatency         time.Duration
	MaxCostMicros      int64
	SemanticsDigest    string
	OutputSchemaDigest string
	ToolSchemaDigest   string
}

// EffectState records whether an earlier tool effect may have committed.
type EffectState uint8

const (
	// EffectNone means this model step has not produced a tool effect.
	EffectNone EffectState = iota
	// EffectCommitted means a tool effect has committed in this run.
	EffectCommitted
	// EffectUncertain means an earlier tool effect cannot be reconciled yet.
	EffectUncertain
)

// RouteRequest carries the immutable pin, constraints and retry evidence.
type RouteRequest struct {
	TraceID                      string
	Pin                          ModelPin
	Task                         TaskProfile
	BudgetRemainingMicros        int64
	Attempted                    []ModelSelection
	PreviousFailure              ModelFailure
	SameProviderRetriesExhausted bool
	Effect                       EffectState
}

// Eligibility records why one pinned model version did or did not qualify.
type Eligibility struct {
	Selection             ModelSelection `json:"selection"`
	ProfileDigest         string         `json:"profile_digest,omitempty"`
	EvaluationSuiteDigest string         `json:"evaluation_suite_digest,omitempty"`
	Eligible              bool           `json:"eligible"`
	Reason                string         `json:"reason,omitempty"`
}

// RouteRecord is the auditable decision and qualification snapshot for a request.
type RouteRecord struct {
	TraceID                      string           `json:"trace_id"`
	AgentVersionDigest           string           `json:"agent_version_digest"`
	TaskProfileID                string           `json:"task_profile_id"`
	Region                       string           `json:"region"`
	BudgetMicros                 int64            `json:"budget_micros"`
	Eligibility                  []Eligibility    `json:"eligibility"`
	Selected                     ModelSelection   `json:"selected"`
	Fallback                     bool             `json:"fallback"`
	Attempted                    []ModelSelection `json:"attempted"`
	PreviousFailure              FailureCode      `json:"previous_failure,omitempty"`
	RetryableFailure             bool             `json:"retryable_failure"`
	SameProviderRetriesExhausted bool             `json:"same_provider_retries_exhausted"`
	Effect                       EffectState      `json:"effect"`
	Denial                       string           `json:"denial,omitempty"`
	Digest                       string           `json:"digest"`
}

// RouteRecorder persists the route decision before the caller dispatches inference.
type RouteRecorder interface {
	RecordRoute(context.Context, RouteRecord) error
}

// Router applies pinned model eligibility without depending on provider SDKs.
type Router struct {
	profiles map[ModelSelection]ModelProfile
	recorder RouteRecorder
}

// NewRouter validates the immutable profile catalog and requires a durable recorder.
func NewRouter(profiles []ModelProfile, recorder RouteRecorder) (*Router, error) {
	if recorder == nil || len(profiles) == 0 {
		return nil, ErrInvalidRoute
	}
	indexed := make(map[ModelSelection]ModelProfile, len(profiles))
	for _, profile := range profiles {
		if !nonblank(profile.ID) || !validIdentity(profile.Identity) || profile.MaxLatency <= 0 || profile.MaxCostMicros < 0 || profile.ExpectedCostMicros < 0 || profile.ExpectedCostMicros > profile.MaxCostMicros ||
			!validStrings(profile.Regions) || !validStrings(profile.DataClasses) || !validStrings(profile.TaskProfileIDs) ||
			!nonblank(profile.SemanticsDigest) || !nonblank(profile.OutputSchemaDigest) || !nonblank(profile.ToolSchemaDigest) {
			return nil, fmt.Errorf("%w: malformed model profile", ErrInvalidRoute)
		}
		profile.Regions = slices.Clone(profile.Regions)
		profile.DataClasses = slices.Clone(profile.DataClasses)
		profile.TaskProfileIDs = slices.Clone(profile.TaskProfileIDs)
		slices.Sort(profile.Regions)
		slices.Sort(profile.DataClasses)
		slices.Sort(profile.TaskProfileIDs)
		if !validDigest(profile.ProfileDigest) || ModelProfileDigest(profile) != profile.ProfileDigest {
			return nil, fmt.Errorf("%w: model profile digest is missing or incorrect", ErrInvalidRoute)
		}
		selection := ModelSelection{ProfileID: profile.ID, ProfileDigest: profile.ProfileDigest, Identity: profile.Identity}
		if _, exists := indexed[selection]; exists {
			return nil, fmt.Errorf("%w: duplicate model identity", ErrInvalidRoute)
		}
		indexed[selection] = profile
	}
	return &Router{profiles: indexed, recorder: recorder}, nil
}

// Route selects the next eligible pinned version and records the decision before returning.
func (r *Router) Route(ctx context.Context, req RouteRequest) (RouteRecord, error) {
	if r == nil || r.recorder == nil || ctx == nil || !validRequest(req) {
		return RouteRecord{}, ErrInvalidRoute
	}
	if err := ctx.Err(); err != nil {
		return RouteRecord{}, err
	}
	pinned := append([]ModelSelection{req.Pin.Primary}, req.Pin.Fallbacks...)
	if !validAttemptPrefix(req.Attempted, pinned) {
		return RouteRecord{}, ErrInvalidRoute
	}
	index := len(req.Attempted)
	record := RouteRecord{
		TraceID:                      req.TraceID,
		AgentVersionDigest:           req.Pin.AgentVersionDigest,
		TaskProfileID:                req.Task.ID,
		Region:                       req.Task.Region,
		BudgetMicros:                 req.BudgetRemainingMicros,
		Eligibility:                  make([]Eligibility, 0, len(pinned)),
		Attempted:                    slices.Clone(req.Attempted),
		PreviousFailure:              req.PreviousFailure.Code,
		RetryableFailure:             req.PreviousFailure.Retryable,
		SameProviderRetriesExhausted: req.SameProviderRetriesExhausted,
		Effect:                       req.Effect,
	}
	for _, identity := range pinned {
		profile, exists := r.profiles[identity]
		check := Eligibility{Selection: identity}
		if !exists {
			check.Reason = "model version is not in the approved catalog"
		} else if ModelProfileDigest(profile) != identity.ProfileDigest {
			check.Reason = "model profile does not match the pinned digest"
		} else {
			check.ProfileDigest = ModelProfileDigest(profile)
			check.EvaluationSuiteDigest = profile.Evaluation.SuiteDigest
			if reason := eligibilityReason(profile, req); reason != "" {
				check.Reason = reason
			} else {
				check.Eligible = true
			}
		}
		record.Eligibility = append(record.Eligibility, check)
	}
	var routeErr error
	switch {
	case index >= len(pinned):
		routeErr = ErrNoEligibleModel
	case index > 0 && !req.SameProviderRetriesExhausted:
		routeErr = fmt.Errorf("%w: same-provider retries are not exhausted", ErrFallbackDenied)
	case index > 0 && !retryableProviderFailure(req.PreviousFailure):
		routeErr = fmt.Errorf("%w: previous attempt did not fail retryably", ErrFallbackDenied)
	case index > 0 && req.Effect != EffectNone:
		routeErr = fmt.Errorf("%w: tool effect is committed or uncertain", ErrFallbackDenied)
	case !record.Eligibility[index].Eligible:
		routeErr = fmt.Errorf("%w: %s", ErrNoEligibleModel, record.Eligibility[index].Reason)
	default:
		record.Selected = pinned[index]
		record.Fallback = index > 0
	}
	if routeErr != nil {
		record.Denial = routeErr.Error()
	}
	record.Digest = routeDigest(record)
	if err := r.recorder.RecordRoute(ctx, record); err != nil {
		return RouteRecord{}, fmt.Errorf("%w: %v", ErrRouteRecord, err)
	}
	return record, routeErr
}

func validRequest(req RouteRequest) bool {
	return nonblank(req.TraceID) && validPin(req.Pin) && nonblank(req.Pin.AgentVersionDigest) &&
		req.Pin.AgentVersionDigest == req.Task.AgentVersionDigest &&
		req.Pin.TaskProfileID == req.Task.ID && nonblank(req.Task.ID) && nonblank(req.Task.Region) &&
		req.Task.MaxLatency > 0 && req.Task.MaxCostMicros >= 0 && req.BudgetRemainingMicros >= 0 &&
		req.Effect <= EffectUncertain && len(req.Task.DataClasses) > 0 &&
		nonblank(req.Pin.SemanticsDigest) && req.Pin.SemanticsDigest == req.Task.SemanticsDigest &&
		nonblank(req.Pin.OutputSchemaDigest) && req.Pin.OutputSchemaDigest == req.Task.OutputSchemaDigest &&
		nonblank(req.Pin.ToolSchemaDigest) && req.Pin.ToolSchemaDigest == req.Task.ToolSchemaDigest
}

func validPin(pin ModelPin) bool {
	if !validSelection(pin.Primary) {
		return false
	}
	seen := map[ModelSelection]struct{}{pin.Primary: {}}
	for _, fallback := range pin.Fallbacks {
		if !validSelection(fallback) {
			return false
		}
		if _, exists := seen[fallback]; exists {
			return false
		}
		seen[fallback] = struct{}{}
	}
	return true
}

func eligibilityReason(profile ModelProfile, req RouteRequest) string {
	if profile.Evaluation.Passed == false || profile.Evaluation.AgentVersionDigest != req.Pin.AgentVersionDigest || !nonblank(profile.Evaluation.SuiteDigest) {
		return "model has no passing evaluation for this agent version"
	}
	if !slices.Contains(profile.TaskProfileIDs, req.Task.ID) {
		return "task profile is not qualified"
	}
	if !slices.Contains(profile.Regions, req.Task.Region) {
		return "processing region is not eligible"
	}
	for _, class := range req.Task.DataClasses {
		if !slices.Contains(profile.DataClasses, class) {
			return "data class is not eligible"
		}
	}
	if profile.MaxLatency > req.Task.MaxLatency {
		return "latency limit is exceeded"
	}
	if profile.MaxCostMicros > req.Task.MaxCostMicros || profile.ExpectedCostMicros > req.Task.MaxCostMicros {
		return "task cost limit is exceeded"
	}
	if profile.MaxCostMicros > req.BudgetRemainingMicros {
		return "reserved tenant budget is insufficient"
	}
	if profile.SemanticsDigest != req.Task.SemanticsDigest || profile.OutputSchemaDigest != req.Task.OutputSchemaDigest || profile.ToolSchemaDigest != req.Task.ToolSchemaDigest {
		return "output or tool semantics differ from the pinned task"
	}
	return ""
}

func validAttemptPrefix(attempted, pinned []ModelSelection) bool {
	if len(attempted) > len(pinned) {
		return false
	}
	for i, identity := range attempted {
		if identity != pinned[i] {
			return false
		}
	}
	return true
}

func routeDigest(record RouteRecord) string {
	record.Digest = ""
	encoded, _ := json.Marshal(record)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func retryableProviderFailure(failure ModelFailure) bool {
	return failure.Retryable && (failure.Code == FailureUnavailable || failure.Code == FailureTimeout)
}

func validIdentity(identity ModelIdentity) bool {
	return nonblank(identity.ProviderID) && nonblank(identity.ModelID) && nonblank(identity.Version)
}

func validSelection(selection ModelSelection) bool {
	return nonblank(selection.ProfileID) && validDigest(selection.ProfileDigest) && validIdentity(selection.Identity)
}

func validStrings(values []string) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if !nonblank(value) {
			return false
		}
	}
	return true
}

func nonblank(value string) bool { return strings.TrimSpace(value) != "" }

func validDigest(value string) bool {
	return len(value) == sha256.Size*2 && strings.Trim(value, "0123456789abcdef") == ""
}

// ModelProfileDigest returns the canonical digest callers pin to a model profile.
func ModelProfileDigest(profile ModelProfile) string {
	profile.ProfileDigest = ""
	profile.Regions = slices.Clone(profile.Regions)
	profile.DataClasses = slices.Clone(profile.DataClasses)
	profile.TaskProfileIDs = slices.Clone(profile.TaskProfileIDs)
	slices.Sort(profile.Regions)
	slices.Sort(profile.DataClasses)
	slices.Sort(profile.TaskProfileIDs)
	encoded, _ := json.Marshal(profile)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
