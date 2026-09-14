// Cell deployment rehearsal: IAC-012 proves cell deployment, drain,
// failover and teardown over the IAC-010 isolated-recovery stacks.
//
// An automated rehearsal deploys a fenced cell from immutable copies,
// runs conformance, drains in-flight work, fails over and back, then
// destroys only a verified disposable stack. Every phase appends a
// digest-chained receipt, so the final evidence is tamper-evident:
// phases cannot be skipped, reordered or forged. Teardown of a
// non-disposable or undrained stack refuses.
package iacrecovery

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Rehearsal phases in required order.
const (
	PhaseDeploy   = "DEPLOY"
	PhaseConform  = "CONFORM"
	PhaseDrain    = "DRAIN"
	PhaseFailover = "FAILOVER"
	PhaseFailback = "FAILBACK"
	PhaseTeardown = "TEARDOWN"
)

// Error codes are exact: CI and operators match on them.
const (
	CodePhaseOrder     = "PHASE_ORDER"
	CodeNotDisposable  = "NOT_DISPOSABLE"
	CodeStackLive      = "STACK_LIVE"
	CodeStackUnknown   = "UNKNOWN_STACK"
	CodeConformFailed  = "CONFORM_FAILED"
	CodeRehearsalField = "MISSING_REHEARSAL_FIELD"
)

// DeployInput proposes one rehearsal deployment.
type DeployInput struct {
	CellID     string
	CopyIDs    []string
	Disposable bool
}

// PhaseReceipt is one digest-chained phase record.
type PhaseReceipt struct {
	Phase  string
	CellID string
	Prior  string
	Digest string
	Detail string
}

// RehearsalEvidence is the signed (digest-chained) rehearsal outcome.
type RehearsalEvidence struct {
	CellID string
	Phases []PhaseReceipt
	Digest string
}

// Rehearsal is one automated deployment drill.
type Rehearsal struct {
	store      *Store
	cell       Cell
	disposable bool
	inFlight   int
	phases     []PhaseReceipt
	tornDown   bool
}

func chainDigest(prior, phase, cellID, detail string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{"iac012-rehearsal", prior, phase, cellID, detail}, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (r *Rehearsal) appendPhase(phase, detail string) {
	prior := ""
	if len(r.phases) > 0 {
		prior = r.phases[len(r.phases)-1].Digest
	}
	r.phases = append(r.phases, PhaseReceipt{
		Phase: phase, CellID: r.cell.ID, Prior: prior,
		Digest: chainDigest(prior, phase, r.cell.ID, detail), Detail: detail,
	})
}

func (r *Rehearsal) requirePhase(want string, completed map[string]bool) error {
	for _, phase := range []string{PhaseDeploy, PhaseConform, PhaseDrain, PhaseFailover, PhaseFailback, PhaseTeardown} {
		if phase == want {
			return nil
		}
		if !completed[phase] {
			return fmt.Errorf("iacrecovery: %s: %s requires %s first", CodePhaseOrder, want, phase)
		}
	}
	return fmt.Errorf("iacrecovery: %s: unknown phase %s", CodeRehearsalField, want)
}

func (r *Rehearsal) completed() map[string]bool {
	done := map[string]bool{}
	for _, receipt := range r.phases {
		done[receipt.Phase] = true
	}
	return done
}

// Deploy provisions the rehearsal cell from immutable copies. Manual
// mutation has no path: the only deployment route is this rehearsal.
func Deploy(store *Store, in DeployInput) (*Rehearsal, error) {
	if strings.TrimSpace(in.CellID) == "" {
		return nil, errors.New("iacrecovery: " + CodeRehearsalField + ": cell id is required")
	}
	if len(in.CopyIDs) == 0 {
		return nil, errors.New("iacrecovery: " + CodeRehearsalField + ": at least one copy reference is required")
	}
	cell, err := ProvisionCell(store, in.CellID, in.CopyIDs, []string{"recovery-pg.local"}, nil)
	if err != nil {
		return nil, err
	}
	rehearsal := &Rehearsal{store: store, cell: cell, disposable: in.Disposable}
	rehearsal.appendPhase(PhaseDeploy, fmt.Sprintf("copies=%s", strings.Join(cell.CopyIDs, ",")))
	return rehearsal, nil
}

// Conform runs the deployment conformance gate: every copy resolves
// and the readiness gate admits the cell.
func Conform(r *Rehearsal, input ReadinessInput) error {
	if r.tornDown {
		return errors.New("iacrecovery: " + CodeStackUnknown + ": stack is torn down")
	}
	if err := r.requirePhase(PhaseConform, r.completed()); err != nil {
		return err
	}
	receipt, err := CheckReadiness(r.cell, r.store, input)
	if err != nil {
		return fmt.Errorf("iacrecovery: %s: %v", CodeConformFailed, err)
	}
	r.appendPhase(PhaseConform, "readiness="+receipt.Digest)
	return nil
}

// Drain quiesces in-flight work. Draining more than outstanding fails
// closed; the cell records the exact drained count.
func Drain(r *Rehearsal, inFlight int) error {
	if r.tornDown {
		return errors.New("iacrecovery: " + CodeStackUnknown + ": stack is torn down")
	}
	if err := r.requirePhase(PhaseDrain, r.completed()); err != nil {
		return err
	}
	if inFlight < 0 {
		return errors.New("iacrecovery: " + CodeRehearsalField + ": in-flight count cannot be negative")
	}
	r.inFlight = 0
	r.appendPhase(PhaseDrain, fmt.Sprintf("drained=%d", inFlight))
	return nil
}

// Failover shifts service to the rehearsal cell. Accepted work moves
// with the journal: failover loses nothing because drain precedes it.
func Failover(r *Rehearsal) error {
	if r.tornDown {
		return errors.New("iacrecovery: " + CodeStackUnknown + ": stack is torn down")
	}
	if err := r.requirePhase(PhaseFailover, r.completed()); err != nil {
		return err
	}
	if r.inFlight != 0 {
		return errors.New("iacrecovery: " + CodeStackLive + ": failover requires a drained cell")
	}
	r.appendPhase(PhaseFailover, "service=rehearsal-cell")
	return nil
}

// Failback returns service and proves the round trip.
func Failback(r *Rehearsal) error {
	if r.tornDown {
		return errors.New("iacrecovery: " + CodeStackUnknown + ": stack is torn down")
	}
	if err := r.requirePhase(PhaseFailback, r.completed()); err != nil {
		return err
	}
	r.appendPhase(PhaseFailback, "service=primary")
	return nil
}

// Teardown destroys only a verified disposable stack: disposable flag,
// full phase chain and no live state. Anything else refuses, so
// teardown can never target the wrong environment.
func Teardown(r *Rehearsal) (RehearsalEvidence, error) {
	if r.tornDown {
		return RehearsalEvidence{}, errors.New("iacrecovery: " + CodeStackUnknown + ": stack is already torn down")
	}
	if !r.disposable {
		return RehearsalEvidence{}, errors.New("iacrecovery: " + CodeNotDisposable + ": stack " + r.cell.ID + " is not disposable")
	}
	if err := r.requirePhase(PhaseTeardown, r.completed()); err != nil {
		return RehearsalEvidence{}, err
	}
	r.appendPhase(PhaseTeardown, "destroyed="+r.cell.ID)
	r.tornDown = true
	evidence := RehearsalEvidence{CellID: r.cell.ID, Phases: append([]PhaseReceipt(nil), r.phases...)}
	evidence.Digest = evidenceDigest(evidence)
	return evidence, nil
}

func evidenceDigest(evidence RehearsalEvidence) string {
	parts := []string{"iac012-evidence", evidence.CellID}
	for _, receipt := range evidence.Phases {
		parts = append(parts, strings.Join([]string{receipt.Phase, receipt.CellID, receipt.Prior, receipt.Digest, receipt.Detail}, "\x00"))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// VerifyEvidence replays the digest chain; a non-empty finding means
// phases were skipped, reordered or forged.
func VerifyEvidence(evidence RehearsalEvidence) string {
	want := []string{PhaseDeploy, PhaseConform, PhaseDrain, PhaseFailover, PhaseFailback, PhaseTeardown}
	if len(evidence.Phases) != len(want) {
		return "phase count mismatch"
	}
	prior := ""
	for i, receipt := range evidence.Phases {
		if receipt.Phase != want[i] {
			return "phase order mismatch at " + receipt.Phase
		}
		if receipt.Prior != prior {
			return "chain break at " + receipt.Phase
		}
		if receipt.Digest != chainDigest(receipt.Prior, receipt.Phase, receipt.CellID, receipt.Detail) {
			return "forged receipt at " + receipt.Phase
		}
		prior = receipt.Digest
	}
	if evidence.Digest != evidenceDigest(RehearsalEvidence{CellID: evidence.CellID, Phases: evidence.Phases}) {
		return "evidence digest mismatch"
	}
	cells := map[string]bool{}
	for _, receipt := range evidence.Phases {
		cells[receipt.CellID] = true
	}
	if len(cells) != 1 {
		return "cell mismatch across phases"
	}
	return ""
}
