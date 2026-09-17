package leavereturn

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// Environment is the wired, zero-effect, in-memory projection this
// conformance fixture is simulated against. Every field is a declared,
// pinned fact; nothing here reads a clock, a database or a network.
type Environment struct {
	EmploymentActive  bool
	CurrentManagerID  string
	ProgramsEligible  bool
	ReadinessState    string
	ReviewerRole      string
	MedicalSealed     bool
	ReviewerIsManager bool
	Jurisdiction      string

	// ObserveOutcome and ObserveWatermark control the answer the injected
	// read port gives for the benefits-continuation observation.
	ObserveOutcome   workflow.Outcome
	ObserveWatermark string
}

// GoldenEnvironment is the reference scenario: an active employment, an
// eligible program set, a READY readiness, a sealed leave-administrator
// review and a clean benefits observation.
func GoldenEnvironment() *Environment {
	return &Environment{
		EmploymentActive:  true,
		CurrentManagerID:  "mgr-001",
		ProgramsEligible:  true,
		ReadinessState:    "READY",
		ReviewerRole:      "leave-administrator",
		MedicalSealed:     true,
		ReviewerIsManager: false,
		Jurisdiction:      "hypothetical-fmla",
		ObserveOutcome:    workflow.OutcomePass,
		ObserveWatermark:  "benefits.continuation.stream_head@12",
	}
}

// UnsealedEvidenceEnvironment routes medical evidence outside its
// compartment, which the evidence-review DECISION must block.
func UnsealedEvidenceEnvironment() *Environment {
	e := GoldenEnvironment()
	e.MedicalSealed = false
	return e
}

// ManagerEligibilityEnvironment lets the manager determine legal
// eligibility, which the evidence-review DECISION must block.
func ManagerEligibilityEnvironment() *Environment {
	e := GoldenEnvironment()
	e.ReviewerIsManager = true
	return e
}

// NotReadyEnvironment reports a not-ready return, which the
// readiness-return DECISION must block rather than return into.
func NotReadyEnvironment() *Environment {
	e := GoldenEnvironment()
	e.ReadinessState = "NOT_READY"
	return e
}

// DegradedObservationEnvironment answers the benefits observation with
// FAIL, which must route to the bounded repair terminal rather than
// being folded into a consistent completion or rolling back valid leave.
func DegradedObservationEnvironment() *Environment {
	e := GoldenEnvironment()
	e.ObserveOutcome = workflow.OutcomeFail
	e.ObserveWatermark = ""
	return e
}

// Registry publishes the fixture capability versions this reference workflow
// binds. Every definition is READ_ONLY: the governed gateway refuses every
// write effect class in P1A, so nothing registered here could mutate even if
// a handler tried.
func (e *Environment) Registry() (*capability.Registry, error) {
	r := capability.NewRegistry()
	entries := []struct {
		id      string
		domain  string
		handler capability.Handler
	}{
		{CapReadEmploymentAuthority, "people", e.handleReadEmploymentAuthority},
		{CapResolveLeavePrograms, "leave", e.handleResolveLeavePrograms},
		// The benefits observation runs through the injected ReadPort, not
		// this capability handler; the capability exists only because a
		// StepObserve node must bind one.
		{CapObserveBenefits, "benefits", e.handleObserveBenefitsUnused},
		{CapResolveReadiness, "leave", e.handleResolveReadiness},
	}
	for _, entry := range entries {
		if err := r.Register(readOnlyDefinition(entry.id, entry.domain), entry.handler); err != nil {
			return nil, fmt.Errorf("leavereturn: publish %s: %w", entry.id, err)
		}
	}
	return r, nil
}

func readOnlyDefinition(id, domain string) capability.Definition {
	schema := func(slot string) capability.SchemaRef {
		return capability.SchemaRef{
			SchemaID: id + "." + slot + "/v1", Version: 1,
			ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition",
		}
	}
	return capability.Definition{
		ID: id, Version: 1, OwnerDomain: domain,
		RequestSchema: schema("request"), ResponseSchema: schema("response"), ErrorSchema: schema("error"),
		EffectClass: capability.EffectReadOnly, ReadData: capability.DataDomainFieldSet{DataDomains: []string{domain}},
		RiskClass: "LOW", IdempotencyPolicyRef: "idempotency.read-safe.v1", AuthZScopeRef: "scope:" + domain + ".read",
		LegalBasisRef: "legal.p1a.observation-only.v1", EntitlementRef: "entitlement.pilot.p1a.v1",
		SLOClassRef: "slo.interactive.p95-2s.v1", TestRef: "conformance:" + id + "/v1",
	}
}

func request(payload any) (simulate.CapabilityRequest, error) {
	req, ok := payload.(simulate.CapabilityRequest)
	if !ok {
		return simulate.CapabilityRequest{}, fmt.Errorf("leavereturn: handler received %T, want simulate.CapabilityRequest", payload)
	}
	return req, nil
}

func (e *Environment) handleReadEmploymentAuthority(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"employment_active":      simulate.NewBool(e.EmploymentActive),
			"source_authority_scope": simulate.NewString("scope-authority:acme-workforce"),
			"current_manager_id":     simulate.NewBranded("WorkerID", e.CurrentManagerID),
		},
		Detail: "read employment and authority for manager " + e.CurrentManagerID,
	}, nil
}

func (e *Environment) handleResolveLeavePrograms(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"program_ids":  simulate.NewString("hypothetical-fmla/v2026.1"),
			"all_eligible": simulate.NewBool(e.ProgramsEligible),
			"jurisdiction": simulate.NewString(e.Jurisdiction),
		},
		Detail: "resolved hypothetical leave programs under " + e.Jurisdiction,
	}, nil
}

// handleObserveBenefitsUnused exists only to publish CapObserveBenefits, so
// the OBSERVE node has a capability to bind. The interpreter never invokes
// it: OBSERVE dispatches through the injected simulate.ReadPort (see Reads).
func (e *Environment) handleObserveBenefitsUnused(_ context.Context, payload any) (any, error) {
	req, err := request(payload)
	if err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeUnknown, Outputs: simulate.Bag{},
		Detail: "node " + req.NodeID + " is served by the injected read port",
	}, nil
}

func (e *Environment) handleResolveReadiness(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"readiness_state":   simulate.NewString(e.ReadinessState),
			"employment_active": simulate.NewBool(e.EmploymentActive),
		},
		Detail: "resolved return readiness " + e.ReadinessState,
	}, nil
}
