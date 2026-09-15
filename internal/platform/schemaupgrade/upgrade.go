// Package schemaupgrade owns the side-effect-free protocol for an expand,
// backfill, shadow, cutover and contract upgrade. A database adapter can
// persist the returned checkpoints and events; this package never opens a
// database or changes authoritative business state.
package schemaupgrade

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const schemaVersion = 1

// Version returns the version of the upgrade protocol.
func Version() int { return schemaVersion }

// Explain returns a stable, redaction-safe description of this contract.
func Explain() string { return "expand-backfill-shadow-cutover-contract upgrade protocol" }

// Phase is the ordered lifecycle of a schema upgrade.
type Phase string

const (
	PhasePlanned    Phase = "PLANNED"
	PhaseExpanded   Phase = "EXPANDED"
	PhaseBackfilled Phase = "BACKFILLED"
	PhaseShadowed   Phase = "SHADOWED"
	PhaseCutover    Phase = "CUTOVER"
	PhaseContracted Phase = "CONTRACTED"
	PhaseRolledBack Phase = "ROLLED_BACK"
)

// Compatibility identifies the coexistence promise made by a plan.
type Compatibility string

const (
	CompatibilityBackward Compatibility = "BACKWARD_COMPATIBLE"
	CompatibilityFull     Compatibility = "FULL"
)

// RollbackBoundary is the last lifecycle boundary at which rollback is safe.
// This protocol currently supports only rollback before contraction removes
// the old representation; new boundary semantics must be added explicitly.
type RollbackBoundary string

const RollbackBeforeContract RollbackBoundary = "BEFORE_CONTRACT"

// Row is the stable identity and content digest of one migrated record. Raw
// payloads are deliberately absent from the contract.
type Row struct {
	Key    string
	Digest string
}

// Binary describes the schema versions a binary can read and write.
type Binary struct {
	Name   string
	Reads  []int
	Writes []int
}

// Plan is the immutable upgrade declaration.
type Plan struct {
	ID                        string
	FromVersion               int
	ToVersion                 int
	Compatibility             Compatibility
	SourceDigest              string
	TargetDigest              string
	RollbackBoundary          RollbackBoundary
	RequiredAdoptionWatermark uint64
	BackfillBatchSize         int
	Binaries                  []Binary
}

// BackfillCheckpoint is the resumable, content-addressed progress record.
type BackfillCheckpoint struct {
	PlanID       string
	Cursor       string
	CopiedRows   int
	SourceRows   int
	SourceDigest string
	CopiedDigest string
	Completed    bool
	RecordedAt   time.Time
}

// ShadowComparison is an exact comparison between old and new projections.
type ShadowComparison struct {
	SourceRows   int
	TargetRows   int
	SourceDigest string
	TargetDigest string
	Exact        bool
	ComparedAt   time.Time
}

// Event is append-only evidence for a lifecycle transition.
type Event struct {
	Phase     Phase
	At        time.Time
	Watermark uint64
	Detail    string
}

// State is a value object that an adapter may persist between calls.
type State struct {
	Plan              Plan
	Phase             Phase
	Checkpoint        BackfillCheckpoint
	Shadow            ShadowComparison
	ConsumerWatermark uint64
	History           []Event
}

var (
	ErrInvalidPlan        = errors.New("schema upgrade: invalid plan")
	ErrInvalidTransition  = errors.New("schema upgrade: invalid transition")
	ErrCheckpointMismatch = errors.New("schema upgrade: checkpoint mismatch")
	ErrShadowMismatch     = errors.New("schema upgrade: shadow comparison mismatch")
	ErrAdoptionLag        = errors.New("schema upgrade: consumer adoption watermark is behind")
	ErrNotReversible      = errors.New("schema upgrade: rollback boundary is not declared")
)

// ValidatePlan rejects a plan that cannot support mixed-version operation or a
// resumable, auditable backfill.
func ValidatePlan(plan Plan) error {
	if strings.TrimSpace(plan.ID) == "" || plan.FromVersion <= 0 || plan.ToVersion <= 0 || plan.FromVersion == plan.ToVersion {
		return fmt.Errorf("%w: id and distinct positive versions are required", ErrInvalidPlan)
	}
	if plan.Compatibility != CompatibilityBackward && plan.Compatibility != CompatibilityFull {
		return fmt.Errorf("%w: unsupported compatibility %q", ErrInvalidPlan, plan.Compatibility)
	}
	if len(plan.SourceDigest) != sha256.Size*2 || len(plan.TargetDigest) != sha256.Size*2 {
		return fmt.Errorf("%w: source and target digests must be sha256", ErrInvalidPlan)
	}
	if _, err := hex.DecodeString(plan.SourceDigest); err != nil {
		return fmt.Errorf("%w: source digest is not hexadecimal", ErrInvalidPlan)
	}
	if _, err := hex.DecodeString(plan.TargetDigest); err != nil {
		return fmt.Errorf("%w: target digest is not hexadecimal", ErrInvalidPlan)
	}
	if plan.RollbackBoundary != RollbackBeforeContract || plan.BackfillBatchSize <= 0 {
		return fmt.Errorf("%w: supported rollback boundary and positive batch size are required", ErrInvalidPlan)
	}
	if err := CheckVersionMatrix(plan); err != nil {
		return err
	}
	return nil
}

// CheckVersionMatrix proves that old and new binaries have an overlap for the
// declared rolling window. Every writer must be readable by every binary, and
// every binary must be able to read both representations during the window.
func CheckVersionMatrix(plan Plan) error {
	if len(plan.Binaries) < 2 {
		return fmt.Errorf("%w: at least old and new binaries are required", ErrInvalidPlan)
	}
	for _, binary := range plan.Binaries {
		if strings.TrimSpace(binary.Name) == "" || !containsVersion(binary.Reads, plan.FromVersion) || !containsVersion(binary.Reads, plan.ToVersion) {
			return fmt.Errorf("%w: binary %q cannot read both upgrade versions", ErrInvalidPlan, binary.Name)
		}
		if len(binary.Writes) == 0 {
			return fmt.Errorf("%w: binary %q has no write version", ErrInvalidPlan, binary.Name)
		}
		for _, version := range binary.Writes {
			if version != plan.FromVersion && version != plan.ToVersion {
				return fmt.Errorf("%w: binary %q writes undeclared version %d", ErrInvalidPlan, binary.Name, version)
			}
		}
	}
	return nil
}

func containsVersion(versions []int, want int) bool {
	for _, version := range versions {
		if version == want {
			return true
		}
	}
	return false
}

// New validates a plan and creates its initial state.
func New(plan Plan, now time.Time) (State, error) {
	if err := ValidatePlan(plan); err != nil {
		return State{}, err
	}
	if now.IsZero() {
		return State{}, fmt.Errorf("%w: initial timestamp is required", ErrInvalidPlan)
	}
	state := State{Plan: clonePlan(plan), Phase: PhasePlanned}
	state.appendEvent(PhasePlanned, 0, "plan accepted", now)
	return state, nil
}

// Expand opens the coexistence window.
func (s *State) Expand(now time.Time) error {
	if s.Phase != PhasePlanned {
		return fmt.Errorf("%w: expand from %s", ErrInvalidTransition, s.Phase)
	}
	s.Phase = PhaseExpanded
	s.appendEvent(PhaseExpanded, s.ConsumerWatermark, "old and new representations admitted", now)
	return nil
}

// ResumeBackfill copies at most batch rows into the new representation and
// returns an exact checkpoint. Repeating a checkpoint is idempotent because
// progress is keyed by the last sorted row key and the full source digest.
func (s *State) ResumeBackfill(source []Row, batch int, now time.Time) (BackfillCheckpoint, error) {
	if s.Phase != PhaseExpanded && s.Phase != PhaseBackfilled {
		return BackfillCheckpoint{}, fmt.Errorf("%w: backfill from %s", ErrInvalidTransition, s.Phase)
	}
	if batch <= 0 {
		batch = s.Plan.BackfillBatchSize
	}
	rows, digest, err := normalizeRows(source)
	if err != nil {
		return BackfillCheckpoint{}, err
	}
	if s.Checkpoint.SourceDigest != "" && s.Checkpoint.SourceDigest != digest {
		return BackfillCheckpoint{}, ErrCheckpointMismatch
	}
	if s.Phase == PhaseBackfilled && s.Checkpoint.Completed {
		return s.Checkpoint, nil
	}
	start := s.Checkpoint.CopiedRows
	if start < 0 || start > len(rows) {
		return BackfillCheckpoint{}, ErrCheckpointMismatch
	}
	end := start + batch
	if end > len(rows) {
		end = len(rows)
	}
	copied := rows[:end]
	s.Checkpoint = BackfillCheckpoint{
		PlanID:       s.Plan.ID,
		SourceRows:   len(rows),
		CopiedRows:   end,
		SourceDigest: digest,
		CopiedDigest: digestRows(copied),
		RecordedAt:   now,
	}
	if end > 0 {
		s.Checkpoint.Cursor = rows[end-1].Key
	}
	s.Checkpoint.Completed = end == len(rows)
	if s.Checkpoint.Completed {
		s.Phase = PhaseBackfilled
		s.appendEvent(PhaseBackfilled, s.ConsumerWatermark, "backfill complete", now)
	} else {
		s.Phase = PhaseExpanded
	}
	return s.Checkpoint, nil
}

// CompareShadow records the exact old/new comparison after backfill.
func (s *State) CompareShadow(source, target []Row, now time.Time) (ShadowComparison, error) {
	if s.Phase != PhaseBackfilled {
		return ShadowComparison{}, fmt.Errorf("%w: shadow from %s", ErrInvalidTransition, s.Phase)
	}
	left, leftDigest, err := normalizeRows(source)
	if err != nil {
		return ShadowComparison{}, err
	}
	right, rightDigest, err := normalizeRows(target)
	if err != nil {
		return ShadowComparison{}, err
	}
	comparison := ShadowComparison{SourceRows: len(left), TargetRows: len(right), SourceDigest: leftDigest, TargetDigest: rightDigest, Exact: equalRows(left, right), ComparedAt: now}
	s.Shadow = comparison
	if !comparison.Exact {
		return comparison, ErrShadowMismatch
	}
	s.Phase = PhaseShadowed
	s.appendEvent(PhaseShadowed, s.ConsumerWatermark, "shadow comparison exact", now)
	return comparison, nil
}

// SetConsumerWatermark records the latest observed adoption watermark.
func (s *State) SetConsumerWatermark(watermark uint64) {
	if watermark > s.ConsumerWatermark {
		s.ConsumerWatermark = watermark
	}
}

// Cutover switches serving to the new representation only after exact shadow
// equality and the declared consumer-adoption watermark are both present.
func (s *State) Cutover(watermark uint64, now time.Time) error {
	s.SetConsumerWatermark(watermark)
	if s.Phase != PhaseShadowed {
		return fmt.Errorf("%w: cutover from %s", ErrInvalidTransition, s.Phase)
	}
	if !s.Shadow.Exact {
		return ErrShadowMismatch
	}
	if s.ConsumerWatermark < s.Plan.RequiredAdoptionWatermark {
		return ErrAdoptionLag
	}
	s.Phase = PhaseCutover
	s.appendEvent(PhaseCutover, s.ConsumerWatermark, "new representation serving", now)
	return nil
}

// Contract removes the old representation logically. The state machine keeps
// history intact and requires the consumer watermark again at this boundary.
func (s *State) Contract(now time.Time) error {
	if s.Phase != PhaseCutover {
		return fmt.Errorf("%w: contract from %s", ErrInvalidTransition, s.Phase)
	}
	if s.ConsumerWatermark < s.Plan.RequiredAdoptionWatermark {
		return ErrAdoptionLag
	}
	s.Phase = PhaseContracted
	s.appendEvent(PhaseContracted, s.ConsumerWatermark, "old representation contract permitted", now)
	return nil
}

// Rollback creates a new append-only event and never rewrites prior evidence.
// Replaying a completed rollback is idempotent: it reports success without
// appending a duplicate event so retried recoveries have no duplicate effect.
func (s *State) Rollback(now time.Time) error {
	if s.Phase == PhaseRolledBack {
		return nil
	}
	if s.Phase != PhaseCutover && s.Phase != PhaseContracted {
		return fmt.Errorf("%w: rollback from %s", ErrInvalidTransition, s.Phase)
	}
	if s.Plan.RollbackBoundary != RollbackBeforeContract {
		return ErrNotReversible
	}
	if s.Phase == PhaseContracted {
		return fmt.Errorf("%w: rollback boundary %q has passed", ErrNotReversible, s.Plan.RollbackBoundary)
	}
	s.Phase = PhaseRolledBack
	s.appendEvent(PhaseRolledBack, s.ConsumerWatermark, "serving restored to source representation", now)
	return nil
}

// Snapshot returns a defensive copy suitable for persistence or evidence.
func (s State) Snapshot() State {
	s.Plan = clonePlan(s.Plan)
	s.History = append([]Event(nil), s.History...)
	return s
}

func (s *State) appendEvent(phase Phase, watermark uint64, detail string, now time.Time) {
	if now.IsZero() {
		now = time.Unix(0, 0).UTC()
	}
	s.History = append(s.History, Event{Phase: phase, At: now.UTC(), Watermark: watermark, Detail: detail})
}

func clonePlan(plan Plan) Plan {
	plan.Binaries = append([]Binary(nil), plan.Binaries...)
	for i := range plan.Binaries {
		plan.Binaries[i].Reads = append([]int(nil), plan.Binaries[i].Reads...)
		plan.Binaries[i].Writes = append([]int(nil), plan.Binaries[i].Writes...)
	}
	return plan
}

func normalizeRows(rows []Row) ([]Row, string, error) {
	out := append([]Row(nil), rows...)
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	for i, row := range out {
		if strings.TrimSpace(row.Key) == "" || strings.TrimSpace(row.Digest) == "" {
			return nil, "", fmt.Errorf("%w: row identity and digest are required", ErrInvalidPlan)
		}
		if i > 0 && out[i-1].Key == row.Key {
			return nil, "", fmt.Errorf("%w: duplicate row key %q", ErrInvalidPlan, row.Key)
		}
	}
	return out, digestRows(out), nil
}

func digestRows(rows []Row) string {
	h := sha256.New()
	for _, row := range rows {
		fmt.Fprintf(h, "%s\x00%s\x00", row.Key, row.Digest)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func equalRows(left, right []Row) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
