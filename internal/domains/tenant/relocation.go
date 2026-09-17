// Tenant relocation with signed epoch fencing and rollback (TENANT-005).
//
// A relocation moves one tenant between cells through ordered, evidenced
// phases — freeze, snapshot, copy, catch-up, cutover, reconciliation —
// while the signed placement epoch fences stale writers. Cutover issues a
// new signed placement at epoch+1 for the target cell, so the old cell
// rejects every post-cutover write through CheckContext. Rollback restores
// the source cell at a strictly greater epoch: logical identity (tenant
// and stream IDs) is preserved and no fenced epoch is ever reused.
//
// The plan is a pure value: every phase receipt binds the target digest,
// so a tampered target or skipped phase fails closed before cutover.
package tenant

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	// ErrInvalidRelocation reports a malformed relocation plan, target or
	// stream manifest.
	ErrInvalidRelocation = errors.New("tenant: invalid relocation")
	// ErrRelocationPhase reports an out-of-order, repeated, missing or
	// tampered phase step, or an operation the plan's state forbids.
	ErrRelocationPhase = errors.New("tenant: invalid relocation phase")
)

// RelocationPhase is one ordered relocation step.
type RelocationPhase string

const (
	RelocationFreeze    RelocationPhase = "FREEZE"
	RelocationSnapshot  RelocationPhase = "SNAPSHOT"
	RelocationCopy      RelocationPhase = "COPY"
	RelocationCatchUp   RelocationPhase = "CATCH_UP"
	RelocationCutover   RelocationPhase = "CUTOVER"
	RelocationReconcile RelocationPhase = "RECONCILE"
)

// preCutoverPhases is the evidence order cutover requires.
var preCutoverPhases = []RelocationPhase{
	RelocationFreeze, RelocationSnapshot, RelocationCopy, RelocationCatchUp,
}

// RequiredStreamKinds names every state plane a relocation must carry:
// workflow, timer, signal, outbox, journal and key state. A manifest that
// omits one cannot cut over.
var RequiredStreamKinds = []string{"workflow", "timer", "signal", "outbox", "journal", "key"}

// StreamRef is one preserved state stream: its kind plus its stable ID.
// IDs are preserved verbatim across the move; the tenant and stream IDs
// after cutover must equal the manifest exactly.
type StreamRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// RelocationTarget is the destination cell placement dimensions. Epoch is
// derived (source epoch + 1), never caller-supplied.
type RelocationTarget struct {
	Cell             string `json:"cell"`
	Region           string `json:"region"`
	ResidencyProfile string `json:"residency_profile"`
	IsolationTier    string `json:"isolation_tier"`
}

// PhaseReceipt is the evidence for one completed phase. TargetDigest binds
// the receipt to the exact target the plan began with.
type PhaseReceipt struct {
	Phase          RelocationPhase `json:"phase"`
	TargetDigest   string          `json:"target_digest"`
	EvidenceDigest string          `json:"evidence_digest"`
	EvidenceRef    string          `json:"evidence_ref"`
}

// RelocationPlan is the relocation state. It is a value: callers advance
// it through RecordPhase, CutoverPlan, ReconcilePlan or RollbackPlan.
type RelocationPlan struct {
	PlanID       string           `json:"plan_id"`
	Tenant       string           `json:"tenant"`
	From         Placement        `json:"from"`
	Target       RelocationTarget `json:"target"`
	TargetEpoch  uint64           `json:"target_epoch"`
	TargetDigest string           `json:"target_digest"`
	Streams      []StreamRef      `json:"streams"`
	Receipts     []PhaseReceipt   `json:"receipts"`
	CutoverDone  bool             `json:"cutover_done"`
	Complete     bool             `json:"complete"`
	RolledBack   bool             `json:"rolled_back"`
}

// CutoverReceipt records one cutover with the placement digests on both
// sides so either side can be re-verified independently.
type CutoverReceipt struct {
	PlanID     string      `json:"plan_id"`
	Tenant     string      `json:"tenant"`
	FromCell   string      `json:"from_cell"`
	ToCell     string      `json:"to_cell"`
	FromEpoch  uint64      `json:"from_epoch"`
	ToEpoch    uint64      `json:"to_epoch"`
	FromDigest string      `json:"from_digest"`
	ToDigest   string      `json:"to_digest"`
	Streams    []StreamRef `json:"streams"`
	Digest     string      `json:"digest"`
}

// RollbackReceipt records one rollback to the source cell.
type RollbackReceipt struct {
	PlanID       string `json:"plan_id"`
	Tenant       string `json:"tenant"`
	RestoredCell string `json:"restored_cell"`
	Epoch        uint64 `json:"epoch"`
	Digest       string `json:"digest"`
}

// ReconciliationReceipt records the stream-identity comparison.
type ReconciliationReceipt struct {
	PlanID  string      `json:"plan_id"`
	Tenant  string      `json:"tenant"`
	Streams []StreamRef `json:"streams"`
	Digest  string      `json:"digest"`
}

// RelocationGate is a stateful epoch fence: it holds the current
// authoritative placement and rejects any candidate that differs.
type RelocationGate struct {
	Current Placement
	Key     [32]byte
}

// CheckWrite verifies the candidate and rejects stale or foreign
// placements before the caller may use them.
func (g RelocationGate) CheckWrite(candidate Placement) error {
	return CheckContext(g.Current, candidate, g.Key)
}

// BeginRelocation opens a relocation from the signed source placement to
// the target cell. The source signature is verified, the target must be a
// different cell with complete dimensions, and the stream manifest must
// cover every required state plane exactly once.
func BeginRelocation(planID string, current Placement, target RelocationTarget, streams []StreamRef, key [32]byte) (RelocationPlan, error) {
	if strings.TrimSpace(planID) == "" {
		return RelocationPlan{}, fmt.Errorf("%w: plan id is required", ErrInvalidRelocation)
	}
	if err := Verify(current, key); err != nil {
		return RelocationPlan{}, err
	}
	if strings.TrimSpace(target.Cell) == "" || strings.TrimSpace(target.Region) == "" ||
		strings.TrimSpace(target.ResidencyProfile) == "" || strings.TrimSpace(target.IsolationTier) == "" {
		return RelocationPlan{}, fmt.Errorf("%w: target cell, region, residency profile and isolation tier are required", ErrInvalidRelocation)
	}
	if target.Cell == current.Cell {
		return RelocationPlan{}, fmt.Errorf("%w: target cell must differ from the source cell", ErrInvalidRelocation)
	}
	if err := checkStreamManifest(streams); err != nil {
		return RelocationPlan{}, err
	}
	plan := RelocationPlan{
		PlanID: planID, Tenant: current.Tenant, From: current, Target: target,
		TargetEpoch: current.Epoch + 1, Streams: append([]StreamRef(nil), streams...),
	}
	digest, err := targetDigest(plan)
	if err != nil {
		return RelocationPlan{}, err
	}
	plan.TargetDigest = digest
	return plan, nil
}

func checkStreamManifest(streams []StreamRef) error {
	if len(streams) == 0 {
		return fmt.Errorf("%w: stream manifest is required", ErrInvalidRelocation)
	}
	seen := make(map[string]bool, len(streams))
	for _, ref := range streams {
		if strings.TrimSpace(ref.Kind) == "" || strings.TrimSpace(ref.ID) == "" {
			return fmt.Errorf("%w: stream kind and id are required", ErrInvalidRelocation)
		}
		if seen[ref.Kind] {
			return fmt.Errorf("%w: duplicate stream kind %q", ErrInvalidRelocation, ref.Kind)
		}
		seen[ref.Kind] = true
	}
	for _, kind := range RequiredStreamKinds {
		if !seen[kind] {
			return fmt.Errorf("%w: required stream plane %q is missing", ErrInvalidRelocation, kind)
		}
	}
	return nil
}

func targetDigest(plan RelocationPlan) (string, error) {
	view := struct {
		Tenant string           `json:"tenant"`
		From   Placement        `json:"from"`
		Target RelocationTarget `json:"target"`
		Epoch  uint64           `json:"epoch"`
	}{plan.Tenant, unsignedPlacement(plan.From), plan.Target, plan.TargetEpoch}
	// The source signature authenticates the old cell; the target digest
	// binds the relocation to placement dimensions, not to that signature.
	b, err := json.Marshal(view)
	if err != nil {
		return "", fmt.Errorf("%w: encode target: %v", ErrInvalidRelocation, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func unsignedPlacement(p Placement) Placement {
	p.Signature = ""
	return p
}

// RecordPhase files the evidence for the next expected pre-cutover phase.
// Phases must advance in order without skips or repeats; every receipt
// must carry the plan's target digest so a retargeted plan cannot reuse
// evidence recorded for another destination.
func (p *RelocationPlan) RecordPhase(phase RelocationPhase, evidenceDigest, evidenceRef string) error {
	if p == nil {
		return ErrInvalidRelocation
	}
	if p.Complete || p.RolledBack {
		return fmt.Errorf("%w: plan %s is terminal", ErrRelocationPhase, p.PlanID)
	}
	if p.CutoverDone {
		return fmt.Errorf("%w: plan %s already cut over", ErrRelocationPhase, p.PlanID)
	}
	if len(p.Receipts) >= len(preCutoverPhases) {
		return fmt.Errorf("%w: all pre-cutover phases are recorded", ErrRelocationPhase)
	}
	want := preCutoverPhases[len(p.Receipts)]
	if phase != want {
		return fmt.Errorf("%w: want phase %s, got %s", ErrRelocationPhase, want, phase)
	}
	if strings.TrimSpace(evidenceDigest) == "" || strings.TrimSpace(evidenceRef) == "" {
		return fmt.Errorf("%w: phase %s requires an evidence digest and reference", ErrRelocationPhase, phase)
	}
	p.Receipts = append(p.Receipts, PhaseReceipt{
		Phase: phase, TargetDigest: p.TargetDigest,
		EvidenceDigest: evidenceDigest, EvidenceRef: evidenceRef,
	})
	return nil
}

// CutoverPlan issues the target-cell placement at the fenced epoch. Every
// pre-cutover phase must be evidenced and every receipt must still bind
// the plan's target digest; cutover then records its own receipt.
func CutoverPlan(p *RelocationPlan, key [32]byte) (Placement, CutoverReceipt, error) {
	if p == nil {
		return Placement{}, CutoverReceipt{}, ErrInvalidRelocation
	}
	if p.Complete || p.RolledBack {
		return Placement{}, CutoverReceipt{}, fmt.Errorf("%w: plan %s is terminal", ErrRelocationPhase, p.PlanID)
	}
	if p.CutoverDone {
		return Placement{}, CutoverReceipt{}, fmt.Errorf("%w: plan %s already cut over", ErrRelocationPhase, p.PlanID)
	}
	if len(p.Receipts) != len(preCutoverPhases) {
		missing := preCutoverPhases[len(p.Receipts)]
		return Placement{}, CutoverReceipt{}, fmt.Errorf("%w: phase %s has no evidence", ErrRelocationPhase, missing)
	}
	for _, receipt := range p.Receipts {
		if receipt.TargetDigest != p.TargetDigest {
			return Placement{}, CutoverReceipt{}, fmt.Errorf("%w: phase %s evidence binds another target", ErrRelocationPhase, receipt.Phase)
		}
	}
	if err := Verify(p.From, key); err != nil {
		return Placement{}, CutoverReceipt{}, err
	}
	current, err := targetDigest(*p)
	if err != nil {
		return Placement{}, CutoverReceipt{}, err
	}
	if current != p.TargetDigest {
		return Placement{}, CutoverReceipt{}, fmt.Errorf("%w: relocation target changed after evidence was recorded", ErrRelocationPhase)
	}
	moved, err := Sign(Placement{
		Tenant: p.Tenant, Cell: p.Target.Cell, Region: p.Target.Region,
		ResidencyProfile: p.Target.ResidencyProfile, IsolationTier: p.Target.IsolationTier,
		Epoch: p.TargetEpoch,
	}, key)
	if err != nil {
		return Placement{}, CutoverReceipt{}, err
	}
	fromDigest, err := p.From.Digest()
	if err != nil {
		return Placement{}, CutoverReceipt{}, err
	}
	toDigest, err := moved.Digest()
	if err != nil {
		return Placement{}, CutoverReceipt{}, err
	}
	receipt := CutoverReceipt{
		PlanID: p.PlanID, Tenant: p.Tenant,
		FromCell: p.From.Cell, ToCell: p.Target.Cell,
		FromEpoch: p.From.Epoch, ToEpoch: p.TargetEpoch,
		FromDigest: fromDigest, ToDigest: toDigest,
		Streams: append([]StreamRef(nil), p.Streams...),
	}
	receipt.Digest = hashJSON(struct {
		PlanID     string      `json:"plan_id"`
		Tenant     string      `json:"tenant"`
		FromCell   string      `json:"from_cell"`
		ToCell     string      `json:"to_cell"`
		FromEpoch  uint64      `json:"from_epoch"`
		ToEpoch    uint64      `json:"to_epoch"`
		FromDigest string      `json:"from_digest"`
		ToDigest   string      `json:"to_digest"`
		Streams    []StreamRef `json:"streams"`
	}{receipt.PlanID, receipt.Tenant, receipt.FromCell, receipt.ToCell, receipt.FromEpoch, receipt.ToEpoch, receipt.FromDigest, receipt.ToDigest, receipt.Streams})
	p.Receipts = append(p.Receipts, PhaseReceipt{
		Phase: RelocationCutover, TargetDigest: p.TargetDigest,
		EvidenceDigest: receipt.Digest, EvidenceRef: "cutover-" + p.PlanID,
	})
	p.CutoverDone = true
	return moved, receipt, nil
}

// ReconcilePlan compares the observed post-cutover stream identities with
// the manifest. Missing, swapped or unexpected streams fail closed; an
// exact match completes the plan.
func ReconcilePlan(p *RelocationPlan, observed []StreamRef) (ReconciliationReceipt, error) {
	if p == nil {
		return ReconciliationReceipt{}, ErrInvalidRelocation
	}
	if p.Complete || p.RolledBack {
		return ReconciliationReceipt{}, fmt.Errorf("%w: plan %s is terminal", ErrRelocationPhase, p.PlanID)
	}
	if !p.CutoverDone {
		return ReconciliationReceipt{}, fmt.Errorf("%w: plan %s has not cut over", ErrRelocationPhase, p.PlanID)
	}
	want := streamIndex(p.Streams)
	got := streamIndex(observed)
	for kind, id := range want {
		observedID, ok := got[kind]
		if !ok {
			return ReconciliationReceipt{}, fmt.Errorf("%w: stream plane %q is missing after cutover", ErrRelocationPhase, kind)
		}
		if observedID != id {
			return ReconciliationReceipt{}, fmt.Errorf("%w: stream plane %q changed identity %q to %q", ErrRelocationPhase, kind, id, observedID)
		}
	}
	for kind := range got {
		if _, ok := want[kind]; !ok {
			return ReconciliationReceipt{}, fmt.Errorf("%w: unexpected stream plane %q after cutover", ErrRelocationPhase, kind)
		}
	}
	receipt := ReconciliationReceipt{
		PlanID: p.PlanID, Tenant: p.Tenant, Streams: append([]StreamRef(nil), p.Streams...),
	}
	receipt.Digest = hashJSON(struct {
		PlanID  string      `json:"plan_id"`
		Tenant  string      `json:"tenant"`
		Streams []StreamRef `json:"streams"`
	}{receipt.PlanID, receipt.Tenant, receipt.Streams})
	p.Receipts = append(p.Receipts, PhaseReceipt{
		Phase: RelocationReconcile, TargetDigest: p.TargetDigest,
		EvidenceDigest: receipt.Digest, EvidenceRef: "reconcile-" + p.PlanID,
	})
	p.Complete = true
	return receipt, nil
}

// RollbackPlan restores the source cell at a strictly greater epoch so the
// failed target is fenced and no epoch is ever reused. Tenant and stream
// identity are preserved: only the cell and epoch change. Rollback is
// refused once the plan is complete or already rolled back.
func RollbackPlan(p *RelocationPlan, key [32]byte) (Placement, RollbackReceipt, error) {
	if p == nil {
		return Placement{}, RollbackReceipt{}, ErrInvalidRelocation
	}
	if p.Complete {
		return Placement{}, RollbackReceipt{}, fmt.Errorf("%w: plan %s is complete and cannot roll back", ErrRelocationPhase, p.PlanID)
	}
	if p.RolledBack {
		return Placement{}, RollbackReceipt{}, fmt.Errorf("%w: plan %s already rolled back", ErrRelocationPhase, p.PlanID)
	}
	if err := Verify(p.From, key); err != nil {
		return Placement{}, RollbackReceipt{}, err
	}
	epoch := p.From.Epoch + 1
	if p.CutoverDone && p.TargetEpoch >= epoch {
		epoch = p.TargetEpoch + 1
	}
	restored, err := Sign(Placement{
		Tenant: p.From.Tenant, Cell: p.From.Cell, Region: p.From.Region,
		ResidencyProfile: p.From.ResidencyProfile, IsolationTier: p.From.IsolationTier,
		Epoch: epoch,
	}, key)
	if err != nil {
		return Placement{}, RollbackReceipt{}, err
	}
	receipt := RollbackReceipt{
		PlanID: p.PlanID, Tenant: p.From.Tenant, RestoredCell: p.From.Cell, Epoch: epoch,
	}
	receipt.Digest = hashJSON(struct {
		PlanID       string `json:"plan_id"`
		Tenant       string `json:"tenant"`
		RestoredCell string `json:"restored_cell"`
		Epoch        uint64 `json:"epoch"`
	}{receipt.PlanID, receipt.Tenant, receipt.RestoredCell, receipt.Epoch})
	p.RolledBack = true
	return restored, receipt, nil
}

func streamIndex(refs []StreamRef) map[string]string {
	out := make(map[string]string, len(refs))
	for _, ref := range refs {
		out[ref.Kind] = ref.ID
	}
	return out
}

func hashJSON(view any) string {
	b, err := json.Marshal(view)
	if err != nil {
		// Views are plain strings, integers and slices; Marshal cannot fail.
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// SortedStreamKinds reports the manifest kinds in order for evidence.
func SortedStreamKinds(refs []StreamRef) []string {
	kinds := make([]string, 0, len(refs))
	for _, ref := range refs {
		kinds = append(kinds, ref.Kind)
	}
	sort.Strings(kinds)
	return kinds
}
