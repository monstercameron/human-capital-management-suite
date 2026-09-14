package workflow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
)

// Workload admission (WF-RUN-021) gates workflow work before it schedules:
// branch, child, payload, concurrent-activity and cost demand against the
// limits resolved for the request's tenant, capability, criticality and
// control snapshot.
//
// Denial is a verdict, not an error. Structural excess the retry policy
// cannot fix (too many branches or children, oversize payload) returns
// OVERLOADED; transient pressure (concurrency or cost against current
// usage) returns ADMISSION_DEFERRED with the durable intent reference
// intact, so the intent stays visible to retry and queue policy and is
// never partially scheduled. Malformed policies, requests or scope
// mismatches are errors: misconfiguration fails closed.
//
// Verdicts map onto the owned admission vocabulary (ADMISSION-001):
// ADMIT to [admission.Admit], ADMISSION_DEFERRED to [admission.Defer],
// OVERLOADED to [admission.Reject].

// WorkloadVerdict is one workload admission outcome.
type WorkloadVerdict string

// Workload admission verdicts.
const (
	WorkloadAdmit      WorkloadVerdict = "ADMIT"
	WorkloadOverloaded WorkloadVerdict = "OVERLOADED"
	WorkloadDeferred   WorkloadVerdict = "ADMISSION_DEFERRED"
)

// Valid reports whether v is a declared workload verdict.
func (v WorkloadVerdict) Valid() bool {
	return v == WorkloadAdmit || v == WorkloadOverloaded || v == WorkloadDeferred
}

// AdmissionOutcome maps the verdict onto the owned admission vocabulary.
func (v WorkloadVerdict) AdmissionOutcome() admission.Outcome {
	switch v {
	case WorkloadDeferred:
		return admission.Defer
	case WorkloadOverloaded:
		return admission.Reject
	default:
		return admission.Admit
	}
}

// ErrWorkload is the sentinel every workload misconfiguration refusal
// unwraps to. Classify with [errors.Is]; over-limit work is a
// [WorkloadDecision], never this error.
var ErrWorkload = errors.New("workflow: workload refused")

// Workload refusal codes.
const (
	// CodeWorkloadInvalidPolicy reports a policy with no tenant,
	// capability, criticality, snapshot or usable limit.
	CodeWorkloadInvalidPolicy = "WORKLOAD_INVALID_POLICY"
	// CodeWorkloadInvalidRequest reports a request with no intent, scope
	// or coherent demand.
	CodeWorkloadInvalidRequest = "WORKLOAD_INVALID_REQUEST"
	// CodeWorkloadScopeMismatch reports a request evaluated against a
	// policy bound to another tenant, capability, criticality or control
	// snapshot.
	CodeWorkloadScopeMismatch = "WORKLOAD_SCOPE_MISMATCH"
)

// WorkloadLimits bounds one scope's workflow work. Every bound is
// inclusive: demand exactly at a limit admits.
type WorkloadLimits struct {
	MaxBranches             int
	MaxChildren             int
	MaxPayloadBytes         int64
	MaxConcurrentActivities int
	MaxCostUnits            int64
}

// WorkloadRequest is one admission candidate with its demand.
type WorkloadRequest struct {
	Tenant          values.TenantId
	Capability      string
	Criticality     admission.Criticality
	ControlSnapshot string
	IntentRef       string

	Branches             int
	Children             int
	PayloadBytes         int64
	ConcurrentActivities int
	CostUnits            int64
}

// WorkloadUsage is the current load the request lands on.
type WorkloadUsage struct {
	ConcurrentActivities int
	CostUnits            int64
}

// WorkloadPolicy binds limits to exactly one scope.
type WorkloadPolicy struct {
	Tenant          values.TenantId
	Capability      string
	Criticality     admission.Criticality
	ControlSnapshot string
	Limits          WorkloadLimits
}

// WorkloadDecision is the complete deterministic receipt. LimitName,
// Observed and Allowed name the binding constraint on a denial; IntentRef
// keeps the durable intent visible to retry and queue policy.
type WorkloadDecision struct {
	Verdict   WorkloadVerdict
	IntentRef string
	LimitName string
	Observed  int64
	Allowed   int64
	Retryable bool
}

// AdmitWorkload evaluates demand against the bound limits. Structural
// dimensions (branches, children, payload) deny OVERLOADED; pressure
// dimensions (concurrency, cost) deny ADMISSION_DEFERRED. The first
// binding constraint wins, evaluated in structural-then-pressure order,
// so a decision names exactly one limit.
func AdmitWorkload(req WorkloadRequest, usage WorkloadUsage, policy WorkloadPolicy) (WorkloadDecision, error) {
	if err := checkWorkloadPolicy(policy); err != nil {
		return WorkloadDecision{}, err
	}
	if err := checkWorkloadRequest(req); err != nil {
		return WorkloadDecision{}, err
	}
	if req.Tenant != policy.Tenant || req.Capability != policy.Capability ||
		req.Criticality != policy.Criticality || req.ControlSnapshot != policy.ControlSnapshot {
		return WorkloadDecision{}, fmt.Errorf("%w: %s", ErrWorkload, CodeWorkloadScopeMismatch)
	}
	if usage.ConcurrentActivities < 0 || usage.CostUnits < 0 {
		return WorkloadDecision{}, fmt.Errorf("%w: %s: current usage cannot be negative", ErrWorkload, CodeWorkloadInvalidRequest)
	}
	limits := policy.Limits
	if over, name, observed, allowed := structuralBreach(req, limits); over {
		return WorkloadDecision{Verdict: WorkloadOverloaded, IntentRef: req.IntentRef,
			LimitName: name, Observed: observed, Allowed: allowed}, nil
	}
	if over, name, observed, allowed := pressureBreach(req, usage, limits); over {
		return WorkloadDecision{Verdict: WorkloadDeferred, IntentRef: req.IntentRef,
			LimitName: name, Observed: observed, Allowed: allowed, Retryable: true}, nil
	}
	return WorkloadDecision{Verdict: WorkloadAdmit, IntentRef: req.IntentRef}, nil
}

func structuralBreach(req WorkloadRequest, limits WorkloadLimits) (bool, string, int64, int64) {
	if int64(req.Branches) > int64(limits.MaxBranches) {
		return true, "branches", int64(req.Branches), int64(limits.MaxBranches)
	}
	if int64(req.Children) > int64(limits.MaxChildren) {
		return true, "children", int64(req.Children), int64(limits.MaxChildren)
	}
	if req.PayloadBytes > limits.MaxPayloadBytes {
		return true, "payload_bytes", req.PayloadBytes, limits.MaxPayloadBytes
	}
	return false, "", 0, 0
}

func pressureBreach(req WorkloadRequest, usage WorkloadUsage, limits WorkloadLimits) (bool, string, int64, int64) {
	if int64(usage.ConcurrentActivities+req.ConcurrentActivities) > int64(limits.MaxConcurrentActivities) {
		return true, "concurrent_activities",
			int64(usage.ConcurrentActivities + req.ConcurrentActivities),
			int64(limits.MaxConcurrentActivities)
	}
	if usage.CostUnits+req.CostUnits > limits.MaxCostUnits {
		return true, "cost_units",
			usage.CostUnits + req.CostUnits, limits.MaxCostUnits
	}
	return false, "", 0, 0
}

func checkWorkloadPolicy(policy WorkloadPolicy) error {
	switch {
	case policy.Tenant.Validate() != nil:
		return fmt.Errorf("%w: %s: %v", ErrWorkload, CodeWorkloadInvalidPolicy, policy.Tenant.Validate())
	case strings.TrimSpace(policy.Capability) == "":
		return fmt.Errorf("%w: %s: capability is required", ErrWorkload, CodeWorkloadInvalidPolicy)
	case !validWorkloadCriticality(policy.Criticality):
		return fmt.Errorf("%w: %s: unknown criticality %q", ErrWorkload, CodeWorkloadInvalidPolicy, policy.Criticality)
	case strings.TrimSpace(policy.ControlSnapshot) == "":
		return fmt.Errorf("%w: %s: control snapshot is required", ErrWorkload, CodeWorkloadInvalidPolicy)
	case policy.Limits.MaxBranches <= 0 || policy.Limits.MaxChildren <= 0 ||
		policy.Limits.MaxPayloadBytes <= 0 || policy.Limits.MaxConcurrentActivities <= 0 ||
		policy.Limits.MaxCostUnits <= 0:
		return fmt.Errorf("%w: %s: every limit must be positive", ErrWorkload, CodeWorkloadInvalidPolicy)
	}
	return nil
}

func checkWorkloadRequest(req WorkloadRequest) error {
	switch {
	case req.Tenant.Validate() != nil:
		return fmt.Errorf("%w: %s: %v", ErrWorkload, CodeWorkloadInvalidRequest, req.Tenant.Validate())
	case strings.TrimSpace(req.Capability) == "":
		return fmt.Errorf("%w: %s: capability is required", ErrWorkload, CodeWorkloadInvalidRequest)
	case !validWorkloadCriticality(req.Criticality):
		return fmt.Errorf("%w: %s: unknown criticality %q", ErrWorkload, CodeWorkloadInvalidRequest, req.Criticality)
	case strings.TrimSpace(req.ControlSnapshot) == "":
		return fmt.Errorf("%w: %s: control snapshot is required", ErrWorkload, CodeWorkloadInvalidRequest)
	case strings.TrimSpace(req.IntentRef) == "":
		return fmt.Errorf("%w: %s: durable intent reference is required", ErrWorkload, CodeWorkloadInvalidRequest)
	case req.Branches < 0 || req.Children < 0 || req.PayloadBytes < 0 ||
		req.ConcurrentActivities < 0 || req.CostUnits < 0:
		return fmt.Errorf("%w: %s: demand cannot be negative", ErrWorkload, CodeWorkloadInvalidRequest)
	}
	return nil
}

func validWorkloadCriticality(c admission.Criticality) bool {
	switch c {
	case admission.P0, admission.P1, admission.P2, admission.P3, admission.P4:
		return true
	}
	return false
}
