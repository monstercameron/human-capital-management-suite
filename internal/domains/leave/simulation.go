package leave

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Simulation is the immutable side-effect-free leave simulation. It binds
// digests, never rows: simulation creates no leave, availability, schedule
// or balance rows, no WorkItem, no MessageIntent, no outbox and no external
// call, because it owns no port to any of them.
type Simulation struct {
	SnapshotDigest   string
	ResolutionDigest string
	PlanDigest       string
	Obligations      []string
	EvidenceRefs     []string
	NotExecuted      bool
	Digest           string
}

func simulationDigest(snapshot, resolution, plan string, obligations, refs []string) string {
	sortedObligations := append([]string(nil), obligations...)
	sort.Strings(sortedObligations)
	sortedRefs := append([]string(nil), refs...)
	sort.Strings(sortedRefs)
	parts := append([]string{"leave-simulation", snapshot, resolution, plan}, sortedObligations...)
	parts = append(parts, sortedRefs...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Simulate binds the snapshot, eligibility, entitlement and schedule plans
// into one immutable simulation. Every input seal is verified first; the
// result is explicitly NOT_EXECUTED.
func Simulate(snapshot LeaveInputSnapshot, resolution EligibilityResolution, plan LeaveEntitlementPlan) (Simulation, error) {
	if err := snapshot.Verify(); err != nil {
		return Simulation{}, fmt.Errorf("leave: simulation refuses an unsealed snapshot: %v", err)
	}
	if err := resolution.Verify(); err != nil {
		return Simulation{}, fmt.Errorf("leave: simulation refuses an unsealed resolution: %v", err)
	}
	if err := plan.Verify(); err != nil {
		return Simulation{}, fmt.Errorf("leave: simulation refuses an unsealed plan: %v", err)
	}
	var obligations []string
	for _, result := range resolution.Programs {
		obligations = append(obligations, result.Obligations...)
	}
	obligations = append(obligations, plan.Obligations...)
	sort.Strings(obligations)
	var refs []string
	for _, entry := range snapshot.Entries {
		refs = append(refs, entry.EvidenceRef.ID)
	}
	sort.Strings(refs)
	return Simulation{
		SnapshotDigest: snapshot.Digest, ResolutionDigest: resolution.Digest, PlanDigest: plan.Digest,
		Obligations: obligations, EvidenceRefs: refs, NotExecuted: true,
		Digest: simulationDigest(snapshot.Digest, resolution.Digest, plan.Digest, obligations, refs),
	}, nil
}

// Verify recomputes the simulation seal.
func (simulation Simulation) Verify() error {
	if !simulation.NotExecuted {
		return fmt.Errorf("leave: simulation must stay NOT_EXECUTED")
	}
	if simulation.Digest == "" || simulationDigest(simulation.SnapshotDigest, simulation.ResolutionDigest, simulation.PlanDigest, simulation.Obligations, simulation.EvidenceRefs) != simulation.Digest {
		return fmt.Errorf("leave: simulation seal is broken")
	}
	return nil
}

// MaskRef reduces a typed evidence reference to its compartment: safe
// presentation is a field-masked view of typed artifacts, never copied
// evidence content.
func MaskRef(ref string) string {
	compartment, _, found := strings.Cut(ref, ":")
	if !found || strings.TrimSpace(compartment) == "" {
		return "untyped:***"
	}
	return compartment + ":***"
}

// SafeView is the ordinary artifact: masked references and counts only,
// with zero sensitive detail.
type SafeView struct {
	SegmentKinds    []string
	ObligationCount int
	MaskedRefs      []string
	ScheduledHours  int
	PlannedDebits   int
}

// ProposalRevision freezes every material component under one digest.
type ProposalRevision struct {
	Revision   uint64
	Simulation Simulation
	Plan       LeaveEntitlementPlan
	View       SafeView
	Digest     string
}

func proposalDigest(simulation Simulation, plan LeaveEntitlementPlan, revision uint64) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{"leave-proposal", simulation.Digest, plan.Digest, fmt.Sprint(revision)}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// FreezeProposal binds the simulation and plan into a successor revision
// with its masked safe view.
func FreezeProposal(simulation Simulation, plan LeaveEntitlementPlan, revision uint64) (ProposalRevision, error) {
	if err := simulation.Verify(); err != nil {
		return ProposalRevision{}, fmt.Errorf("leave: proposal refuses an unsealed simulation: %v", err)
	}
	if err := plan.Verify(); err != nil {
		return ProposalRevision{}, fmt.Errorf("leave: proposal refuses an unsealed plan: %v", err)
	}
	if revision == 0 {
		return ProposalRevision{}, fmt.Errorf("leave: proposal revision must be positive")
	}
	view := SafeView{ScheduledHours: plan.ScheduledHours, PlannedDebits: plan.PlannedDebits, ObligationCount: len(simulation.Obligations)}
	for _, segment := range plan.Segments {
		view.SegmentKinds = append(view.SegmentKinds, segment.Kind)
	}
	for _, ref := range simulation.EvidenceRefs {
		view.MaskedRefs = append(view.MaskedRefs, MaskRef(ref))
	}
	sort.Strings(view.MaskedRefs)
	return ProposalRevision{
		Revision: revision, Simulation: simulation, Plan: plan, View: view,
		Digest: proposalDigest(simulation, plan, revision),
	}, nil
}

// Verify recomputes the proposal seal.
func (proposal ProposalRevision) Verify() error {
	if proposal.Digest == "" || proposalDigest(proposal.Simulation, proposal.Plan, proposal.Revision) != proposal.Digest {
		return fmt.Errorf("leave: proposal seal is broken")
	}
	if err := proposal.Simulation.Verify(); err != nil {
		return err
	}
	return proposal.Plan.Verify()
}
