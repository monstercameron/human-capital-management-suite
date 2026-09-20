package recruit

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
//
// It is deliberately not a domain implementation: REV-020-01 proves the
// workflow's own composition (duplicate-person refusal, offer binding and
// expiry, position/budget reservation, document work-authorization evidence,
// degraded downstream repair), not a real ATS, HRIS, payroll or IAM
// integration.
type Environment struct {
	CandidateID string
	PersonID    string
	OfferID     string
	PositionID  string

	DuplicatePerson   bool
	PositionAvailable bool
	BudgetAvailable   bool

	OfferApproved bool
	OfferAccepted bool

	// OfferClockOutcome and OfferWatermark control the answer the injected
	// read port gives for the offer-clock observation.
	OfferClockOutcome workflow.Outcome
	OfferWatermark    string

	WorkAuthValid        bool
	WorkAuthEvidenceRefs []string

	IAMReady       bool
	PayrollReady   bool
	EquipmentReady bool
	LearningReady  bool
}

// GoldenEnvironment is the reference scenario: a new person, a funded open
// position, a bound offer inside its window, valid work authorization and
// ready downstream systems. It is the scenario REV-020-01's primary golden
// walk runs.
func GoldenEnvironment() *Environment {
	return &Environment{
		CandidateID:       "cand-2026-0001",
		PersonID:          "person-2026-0001",
		OfferID:           "offer-2026-0001",
		PositionID:        "POS-ENG-114",
		DuplicatePerson:   false,
		PositionAvailable: true,
		BudgetAvailable:   true,
		OfferApproved:     true,
		OfferAccepted:     true,
		OfferClockOutcome: workflow.OutcomePass,
		OfferWatermark:    "recruiting.offer.stream_head@42",
		WorkAuthValid:     true,
		WorkAuthEvidenceRefs: []string{
			"doc:I-9/2026/881",
			"doc:passport/2026/114",
		},
		IAMReady:       true,
		PayrollReady:   true,
		EquipmentReady: true,
		LearningReady:  true,
	}
}

// DuplicatePersonEnvironment reports a candidate who already holds an active
// employment: the routing decision must refuse the hire rather than mint a
// second worker record.
func DuplicatePersonEnvironment() *Environment {
	e := GoldenEnvironment()
	e.DuplicatePerson = true
	return e
}

// OfferExpiredEnvironment answers the offer-clock observation with FAIL: the
// offer window lapsed before the start date, so the walk must end at the
// expired terminal without ever building a proposal.
func OfferExpiredEnvironment() *Environment {
	e := GoldenEnvironment()
	e.OfferClockOutcome = workflow.OutcomeFail
	e.OfferWatermark = ""
	return e
}

// OfferUnboundEnvironment leaves the offer approved but unaccepted: the
// routing decision must refuse the hire even though the clock is valid.
func OfferUnboundEnvironment() *Environment {
	e := GoldenEnvironment()
	e.OfferAccepted = false
	return e
}

// ExhaustedPositionEnvironment reports no open headcount on the target
// position: the reservation leg must refuse rather than over-hire.
func ExhaustedPositionEnvironment() *Environment {
	e := GoldenEnvironment()
	e.PositionAvailable = false
	return e
}

// ExhaustedBudgetEnvironment reports an exhausted budget line behind an open
// position: same refusal, different gate.
func ExhaustedBudgetEnvironment() *Environment {
	e := GoldenEnvironment()
	e.BudgetAvailable = false
	return e
}

// MissingWorkAuthEnvironment reports no valid work authorization: the
// document leg must block the hire even though every other gate is clear.
func MissingWorkAuthEnvironment() *Environment {
	e := GoldenEnvironment()
	e.WorkAuthValid = false
	e.WorkAuthEvidenceRefs = nil
	return e
}

// DegradedReadinessEnvironment reports payroll not ready for the start date:
// the walk must end at the bounded repair terminal, not a clean hire and
// not a silent partial onboarding.
func DegradedReadinessEnvironment() *Environment {
	e := GoldenEnvironment()
	e.PayrollReady = false
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
		{CapReadPerson, "people", e.handleReadPerson},
		{CapReadCapacity, "workforce", e.handleReadCapacity},
		{CapVerifyOffer, "offers", e.handleVerifyOffer},
		{CapObserveOffer, "offers", e.handleObserveOfferUnused},
		{CapVerifyWorkAuth, "compliance", e.handleVerifyWorkAuth},
		{CapCheckReadiness, "operations", e.handleCheckReadiness},
	}
	for _, entry := range entries {
		if err := r.Register(readOnlyDefinition(entry.id, entry.domain), entry.handler); err != nil {
			return nil, fmt.Errorf("recruit: publish %s: %w", entry.id, err)
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
		return simulate.CapabilityRequest{}, fmt.Errorf("recruit: handler received %T, want simulate.CapabilityRequest", payload)
	}
	return req, nil
}

func (e *Environment) handleReadPerson(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"person_id":        simulate.NewBranded("PersonID", e.PersonID),
			"duplicate_person": simulate.NewBool(e.DuplicatePerson),
		},
		Detail: "read person registry for " + e.CandidateID,
	}, nil
}

func (e *Environment) handleReadCapacity(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"position_available": simulate.NewBool(e.PositionAvailable),
			"budget_available":   simulate.NewBool(e.BudgetAvailable),
		},
		Detail: "read position and budget capacity for " + e.PositionID,
	}, nil
}

func (e *Environment) handleVerifyOffer(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"offer_approved": simulate.NewBool(e.OfferApproved),
			"offer_accepted": simulate.NewBool(e.OfferAccepted),
		},
		Detail: "verified offer binding for " + e.OfferID,
	}, nil
}

// handleObserveOfferUnused exists only to publish CapObserveOffer, so the
// StepObserve node has a capability to bind. The interpreter never invokes
// it: OBSERVE dispatches through the injected simulate.ReadPort.
func (e *Environment) handleObserveOfferUnused(_ context.Context, payload any) (any, error) {
	req, err := request(payload)
	if err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeUnknown, Outputs: simulate.Bag{},
		Detail: "node " + req.NodeID + " is served by the injected read port",
	}, nil
}

func (e *Environment) handleVerifyWorkAuth(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	refs := ""
	for i, ref := range e.WorkAuthEvidenceRefs {
		if i > 0 {
			refs += ","
		}
		refs += ref
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"work_auth_valid":         simulate.NewBool(e.WorkAuthValid),
			"work_auth_evidence_refs": simulate.NewString(refs),
		},
		Detail: "verified work authorization documents for " + e.CandidateID,
	}, nil
}

func (e *Environment) handleCheckReadiness(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	all := e.IAMReady && e.PayrollReady && e.EquipmentReady && e.LearningReady
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"iam_ready":       simulate.NewBool(e.IAMReady),
			"payroll_ready":   simulate.NewBool(e.PayrollReady),
			"equipment_ready": simulate.NewBool(e.EquipmentReady),
			"learning_ready":  simulate.NewBool(e.LearningReady),
			"all_ready":       simulate.NewBool(all),
		},
		Detail: "checked downstream readiness for " + e.CandidateID,
	}, nil
}
