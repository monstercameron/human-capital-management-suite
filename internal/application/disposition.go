package application

// Governed retention disposition, legal-hold enforcement and verified
// deletion (REV-004-02).
//
// MODEL-026 (retention), MODEL-027 (legal hold) and MODEL-028 (verified
// deletion) shipped as well-tested libraries with no caller in any running
// binary. This file is the thin adapter that puts them on a real request
// path: the serve composition root builds one DispositionGate per process
// (see ComposeServe), the App exposes it, and the `hcmnext
// records-disposition` operator command drives it. No evaluator or
// certificate logic changes here; the gate only orders the calls every
// governed deletion must pass through: retention simulation, legal-hold
// evaluation, then verified deletion, with the hold verdict failing closed.

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legalhold"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/records"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ComponentDispositionGate is the graph name of the gate ComposeServe builds.
const ComponentDispositionGate = "disposition-gate"

// DispositionGate is the serve cell's governed disposition entry point. It
// owns the process's legal-hold store and orders retention, hold and
// deletion evaluation. The zero value is unusable: NewDispositionGate is the
// only constructor, so a gate without hold state cannot be composed.
type DispositionGate struct {
	holds *legalhold.Store
}

// NewDispositionGate returns a gate with an empty hold store.
func NewDispositionGate() *DispositionGate {
	return &DispositionGate{holds: legalhold.NewStore()}
}

// CreateHold records a legal hold. It is the operator's way of freezing
// disposition for matching records before any deletion is attempted.
func (g *DispositionGate) CreateHold(hold legalhold.Hold) error {
	if g == nil || g.holds == nil {
		return fmt.Errorf("application: disposition gate is not composed")
	}
	return g.holds.Create(hold)
}

// ReleaseHold ends a hold at the supplied time so disposition can proceed.
func (g *DispositionGate) ReleaseHold(id string, at time.Time) error {
	if g == nil || g.holds == nil {
		return fmt.Errorf("application: disposition gate is not composed")
	}
	return g.holds.Release(id, values.NewInstant(at))
}

// Holds lists the holds visible to the caller's tenant and compartment.
func (g *DispositionGate) Holds(tenant, compartment string, authorized bool) ([]legalhold.Hold, error) {
	if g == nil || g.holds == nil {
		return nil, fmt.Errorf("application: disposition gate is not composed")
	}
	return g.holds.Holds(tenant, compartment, authorized)
}

// HoldEvidence returns the gate's persisted hold findings in recording
// order: every blocked disposition and every lifecycle transition.
func (g *DispositionGate) HoldEvidence() []legalhold.Evidence {
	if g == nil || g.holds == nil {
		return nil
	}
	return g.holds.Evidence()
}

// EvaluateRetention runs the non-destructive retention simulation for one
// disposition candidate set. It authorizes nothing: the report only states
// which copies are eligible and which blockers keep the rest.
func (g *DispositionGate) EvaluateRetention(req records.SimulationRequest) (records.Report, error) {
	if g == nil || g.holds == nil {
		return records.Report{}, fmt.Errorf("application: disposition gate is not composed")
	}
	return records.Simulate(req)
}

// DecideDisposition evaluates the gate's legal holds against one record and
// persists the finding in the hold-evidence log.
func (g *DispositionGate) DecideDisposition(record legalhold.Record, evidenceID string, at time.Time) legalhold.Decision {
	if g == nil || g.holds == nil {
		return legalhold.Decision{Code: "GATE_UNCOMPOSED"}
	}
	return g.holds.DecideDisposition(record, evidenceID, values.NewInstant(at))
}

// VerifiedDeletionRequest is everything a governed deletion must state: the
// deletion envelope the certificate is built from, the retention subject the
// hold check runs against, and the evidence identity the finding is
// persisted under.
type VerifiedDeletionRequest struct {
	Deletion   records.DeletionRequest
	Record     legalhold.Record
	EvidenceID string
	At         time.Time
}

// VerifiedDeletionResult carries the hold decision that gated the deletion
// and, when the gate allowed it, the verified-deletion certificate.
type VerifiedDeletionResult struct {
	Decision    legalhold.Decision
	Certificate records.DeletionCertificate
}

// ExecuteVerifiedDeletion evaluates legal holds before certifying deletion.
// A held record returns the HOLD_BLOCKED decision and no certificate; a
// deletion whose tenant differs from the hold-checked record's tenant is
// refused before either library runs, so one tenant's holds can neither
// open nor freeze another tenant's bytes.
func (g *DispositionGate) ExecuteVerifiedDeletion(req VerifiedDeletionRequest) (VerifiedDeletionResult, error) {
	if g == nil || g.holds == nil {
		return VerifiedDeletionResult{}, fmt.Errorf("application: disposition gate is not composed")
	}
	if req.EvidenceID == "" {
		return VerifiedDeletionResult{}, fmt.Errorf("application: disposition evidence identity is required")
	}
	if req.Deletion.Tenant != req.Record.Tenant {
		return VerifiedDeletionResult{}, fmt.Errorf("application: deletion tenant %q does not match hold-checked record tenant %q",
			req.Deletion.Tenant, req.Record.Tenant)
	}
	decision, err := g.holds.EvaluateDisposition(req.Record, req.EvidenceID, values.NewInstant(req.At))
	if err != nil {
		return VerifiedDeletionResult{Decision: decision}, err
	}
	certificate, err := records.ExecuteDeletion(req.Deletion)
	if err != nil {
		return VerifiedDeletionResult{Decision: decision}, err
	}
	return VerifiedDeletionResult{Decision: decision, Certificate: certificate}, nil
}
