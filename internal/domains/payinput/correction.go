// Payroll-input correction and snapshot: PAYINPUT-003 keeps finalized
// runs immutable while still correcting them, and freezes one complete
// payroll-input snapshot behind a cutoff.
//
// A correction is append-only: PlanCorrection emits the affected-run plan
// for an explicit superseded assignment revision and never rewrites a
// finalized run — the ledger it reads is left untouched. FreezeSnapshot
// records every input revision, presence flag and watermark for the four
// required sources (time, benefit, tax, garnishment); VerifySnapshot
// refuses anything that changed after the cutoff. Amounts use exact
// kernel-decimal math; the clock is injected by the caller. This file is
// kernel-pure: no database, no wall clock, no network.
package payinput

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// CorrectionRejectedCode refuses a malformed correction or snapshot;
// SnapshotFrozenCode refuses input that changed after the cutoff.
const (
	CorrectionRejectedCode = "PAYINPUT_003_REJECTED"
	SnapshotFrozenCode     = "PAYINPUT_003_FROZEN"
)

var (
	// ErrCorrectionRejected is the sentinel for refused corrections and
	// malformed snapshots. Match it with errors.Is.
	ErrCorrectionRejected = errors.New("payinput: correction rejected")
	// ErrSnapshotFrozen is the sentinel for post-cutoff changes. Match
	// it with errors.Is.
	ErrSnapshotFrozen = errors.New("payinput: snapshot frozen")
)

// CorrectionError is a typed correction or snapshot refusal naming the
// offending field and state.
type CorrectionError struct {
	Code   string
	Field  string
	State  string
	Detail string
}

// Error implements error.
func (e *CorrectionError) Error() string {
	if e.Detail == "" {
		return e.Code + ": field " + e.Field + " state " + e.State
	}
	return e.Code + ": field " + e.Field + " state " + e.State + ": " + e.Detail
}

// Is reports ErrCorrectionRejected or ErrSnapshotFrozen by code.
func (e *CorrectionError) Is(target error) bool {
	switch target {
	case ErrCorrectionRejected:
		return e.Code == CorrectionRejectedCode
	case ErrSnapshotFrozen:
		return e.Code == SnapshotFrozenCode
	default:
		return false
	}
}

// AsCorrectionError unwraps a PAYINPUT-003 refusal.
func AsCorrectionError(err error) (*CorrectionError, bool) {
	var refused *CorrectionError
	if errors.As(err, &refused) && (refused.Code == CorrectionRejectedCode || refused.Code == SnapshotFrozenCode) {
		return refused, true
	}
	return nil, false
}

func correctionRejected(field, state string) *CorrectionError {
	return &CorrectionError{Code: CorrectionRejectedCode, Field: field, State: state}
}

func snapshotFrozen(field, state string) *CorrectionError {
	return &CorrectionError{Code: SnapshotFrozenCode, Field: field, State: state}
}

// PayRunState is the closed payroll-run lifecycle vocabulary.
type PayRunState string

// The payroll-run states.
const (
	PayRunOpen      PayRunState = "OPEN"
	PayRunFinalized PayRunState = "FINALIZED"
)

// Valid reports whether the run state is declared.
func (s PayRunState) Valid() bool { return s == PayRunOpen || s == PayRunFinalized }

// PayRun is one immutable payroll run: its assignment digests are pinned
// at finalization and never rewritten in place.
type PayRun struct {
	RunID             string
	PeriodID          string
	State             PayRunState
	AssignmentDigests []string
	CanonicalDigest   string
}

// Validate implements validation.
func (r PayRun) Validate() error {
	if strings.TrimSpace(r.RunID) == "" || strings.TrimSpace(r.PeriodID) == "" {
		return fmt.Errorf("%w: run and period ids are required", ErrInvalidCalculation)
	}
	if !r.State.Valid() {
		return fmt.Errorf("%w: run state is not declared", ErrInvalidCalculation)
	}
	if len(r.AssignmentDigests) == 0 {
		return fmt.Errorf("%w: run assignment digests are required", ErrInvalidCalculation)
	}
	seen := make(map[string]struct{}, len(r.AssignmentDigests))
	previous := ""
	for _, digest := range r.AssignmentDigests {
		if strings.TrimSpace(digest) == "" {
			return fmt.Errorf("%w: run assignment digest must not be blank", ErrInvalidCalculation)
		}
		if _, dup := seen[digest]; dup {
			return fmt.Errorf("%w: duplicate run assignment digest", ErrInvalidCalculation)
		}
		seen[digest] = struct{}{}
		if digest < previous {
			return fmt.Errorf("%w: run assignment digests are not sorted", ErrInvalidCalculation)
		}
		previous = digest
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != r.computedDigest() {
		return fmt.Errorf("%w: run digest mismatch", ErrInvalidCalculation)
	}
	return nil
}

func (r PayRun) body() []byte {
	digests := append([]string(nil), r.AssignmentDigests...)
	sort.Strings(digests)
	b, err := canonicalbytes.New("hcmnext.domains.payinput.PayRun", schemaVersion).
		String("run_id", r.RunID).String("period_id", r.PeriodID).String("state", string(r.State)).
		SortedStrings("assignment_digest", digests).Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (r PayRun) computedDigest() string { return canonicalbytes.Digest(r.body()) }

// Canonical implements canonicalbytes.Canonicalizer.
func (r PayRun) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}

// NewPayRun validates, sorts and digests a payroll run.
func NewPayRun(run PayRun) (PayRun, error) {
	run.AssignmentDigests = append([]string(nil), run.AssignmentDigests...)
	sort.Strings(run.AssignmentDigests)
	run.CanonicalDigest = ""
	if err := run.Validate(); err != nil {
		return PayRun{}, err
	}
	run.CanonicalDigest = run.computedDigest()
	return run, nil
}

// CorrectionRequest is one explicit append-only correction of a superseded
// assignment revision. DependentPeriods must cover every affected run
// period; RequestedAt is the injected clock.
type CorrectionRequest struct {
	CorrectionID     string
	AssignmentDigest string
	DependentPeriods []string
	PriorAmount      values.Decimal
	CorrectedAmount  values.Decimal
	Reason           string
	RequestedAt      values.Instant
}

// Validate implements validation.
func (r CorrectionRequest) Validate() error {
	if strings.TrimSpace(r.CorrectionID) == "" {
		return correctionRejected("correction_id", "missing")
	}
	if strings.TrimSpace(r.AssignmentDigest) == "" {
		return correctionRejected("assignment_digest", "missing")
	}
	if len(r.DependentPeriods) == 0 {
		return correctionRejected("dependent_periods", "missing")
	}
	seen := make(map[string]struct{}, len(r.DependentPeriods))
	previous := ""
	for _, period := range r.DependentPeriods {
		if strings.TrimSpace(period) == "" {
			return correctionRejected("dependent_periods", "blank")
		}
		if _, dup := seen[period]; dup {
			return correctionRejected("dependent_periods", "duplicate")
		}
		seen[period] = struct{}{}
		if period < previous {
			return correctionRejected("dependent_periods", "unsorted")
		}
		previous = period
	}
	if r.PriorAmount.Validate() != nil || r.CorrectedAmount.Validate() != nil {
		return correctionRejected("amounts", "invalid")
	}
	if strings.TrimSpace(r.Reason) == "" {
		return correctionRejected("reason", "missing")
	}
	if r.RequestedAt.Validate() != nil {
		return correctionRejected("requested_at", "invalid")
	}
	return nil
}

// CorrectionPlan is the emitted affected-run plan. Finalized runs are
// named here and corrected by a separate explicit payroll correction —
// never rewritten in place.
type CorrectionPlan struct {
	CorrectionID     string
	SupersedesDigest string
	AffectedRuns     []string
	DependentPeriods []string
	PriorAmount      values.Decimal
	CorrectedAmount  values.Decimal
	TotalDelta       values.Decimal
	RequestedAt      values.Instant
	CanonicalDigest  string
}

func (p CorrectionPlan) body() []byte {
	runs := append([]string(nil), p.AffectedRuns...)
	sort.Strings(runs)
	periods := append([]string(nil), p.DependentPeriods...)
	sort.Strings(periods)
	w := canonicalbytes.New("hcmnext.domains.payinput.CorrectionPlan", schemaVersion).
		String("correction_id", p.CorrectionID).String("supersedes_digest", p.SupersedesDigest).
		SortedStrings("affected_run", runs).SortedStrings("dependent_period", periods).
		Value("prior_amount", p.PriorAmount).Value("corrected_amount", p.CorrectedAmount).
		Value("total_delta", p.TotalDelta).Value("requested_at", p.RequestedAt)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (p CorrectionPlan) computedDigest() string { return canonicalbytes.Digest(p.body()) }

// Canonical implements canonicalbytes.Canonicalizer.
func (p CorrectionPlan) Canonical() []byte {
	if p.CorrectionID == "" || p.SupersedesDigest == "" || len(p.AffectedRuns) == 0 || p.CanonicalDigest != p.computedDigest() {
		return nil
	}
	return p.body()
}

// Explain renders the bounded human-readable account: correction id,
// affected-run count and exact delta only — never worker identities.
func (p CorrectionPlan) Explain() string {
	return fmt.Sprintf("correction %s: runs=%d delta=%s", p.CorrectionID, len(p.AffectedRuns), p.TotalDelta.String())
}

// PlanCorrection emits the affected-run plan for one explicit correction.
// The ledger is read, never written: finalized runs are named, not
// mutated. Amounts use exact decimal subtraction.
func PlanCorrection(ledger []PayRun, request CorrectionRequest, now values.Instant) (CorrectionPlan, error) {
	if err := request.Validate(); err != nil {
		return CorrectionPlan{}, err
	}
	if now.Validate() != nil {
		return CorrectionPlan{}, correctionRejected("clock", "invalid")
	}
	if len(ledger) == 0 {
		return CorrectionPlan{}, correctionRejected("ledger", "empty")
	}
	periods := make(map[string]struct{}, len(ledger))
	var affected []string
	for _, run := range ledger {
		if err := run.Validate(); err != nil {
			return CorrectionPlan{}, correctionRejected("ledger", "invalid-run")
		}
		periods[run.PeriodID] = struct{}{}
		for _, digest := range run.AssignmentDigests {
			if digest == request.AssignmentDigest {
				affected = append(affected, run.RunID)
				break
			}
		}
	}
	if len(affected) == 0 {
		return CorrectionPlan{}, correctionRejected("assignment_digest", "unknown")
	}
	for _, period := range request.DependentPeriods {
		if _, ok := periods[period]; !ok {
			return CorrectionPlan{}, correctionRejected("dependent_periods", "unknown-period")
		}
	}
	covered := make(map[string]struct{}, len(request.DependentPeriods))
	for _, period := range request.DependentPeriods {
		covered[period] = struct{}{}
	}
	for _, run := range ledger {
		for _, digest := range run.AssignmentDigests {
			if digest == request.AssignmentDigest {
				if _, ok := covered[run.PeriodID]; !ok {
					return CorrectionPlan{}, correctionRejected("dependent_periods", "omitted")
				}
			}
		}
	}
	delta, err := request.CorrectedAmount.Sub(request.PriorAmount)
	if err != nil {
		return CorrectionPlan{}, correctionRejected("amounts", "inexact")
	}
	sort.Strings(affected)
	plan := CorrectionPlan{
		CorrectionID: request.CorrectionID, SupersedesDigest: request.AssignmentDigest,
		AffectedRuns: affected, DependentPeriods: append([]string(nil), request.DependentPeriods...),
		PriorAmount: request.PriorAmount, CorrectedAmount: request.CorrectedAmount,
		TotalDelta: delta, RequestedAt: request.RequestedAt,
	}
	plan.CanonicalDigest = plan.computedDigest()
	return plan, nil
}

// InputSource is the closed vocabulary of payroll-input origins a
// snapshot must record.
type InputSource string

// The required snapshot sources.
const (
	SourceTime        InputSource = "TIME"
	SourceBenefit     InputSource = "BENEFIT"
	SourceTax         InputSource = "TAX"
	SourceGarnishment InputSource = "GARNISHMENT"
)

// Valid reports whether the source is declared.
func (s InputSource) Valid() bool {
	switch s {
	case SourceTime, SourceBenefit, SourceTax, SourceGarnishment:
		return true
	default:
		return false
	}
}

// SnapshotInput records one source revision: its digest, presence flag
// and the watermark it was observed at.
type SnapshotInput struct {
	Source    InputSource
	Revision  string
	Digest    string
	Present   bool
	Watermark values.Instant
}

// Validate implements validation.
func (in SnapshotInput) Validate() error {
	if !in.Source.Valid() {
		return correctionRejected("source", "unknown")
	}
	if strings.TrimSpace(in.Revision) == "" || strings.TrimSpace(in.Digest) == "" {
		return correctionRejected("input", "missing")
	}
	if !in.Present {
		return correctionRejected("input", "absent")
	}
	if in.Watermark.Validate() != nil {
		return correctionRejected("watermark", "invalid")
	}
	return nil
}

// PayInputSnapshot is one immutable, complete payroll-input manifest
// frozen behind its cutoff instant.
type PayInputSnapshot struct {
	SnapshotID      string
	PeriodID        string
	Inputs          []SnapshotInput
	Cutoff          values.Instant
	CanonicalDigest string
}

func (s PayInputSnapshot) body() []byte {
	inputs := append([]SnapshotInput(nil), s.Inputs...)
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].Source < inputs[j].Source })
	w := canonicalbytes.New("hcmnext.domains.payinput.PayInputSnapshot", schemaVersion).
		String("snapshot_id", s.SnapshotID).String("period_id", s.PeriodID).
		Value("cutoff", s.Cutoff).Count("inputs", len(inputs))
	for _, input := range inputs {
		w.String("source", string(input.Source)).String("revision", input.Revision).
			String("digest", input.Digest).Bool("present", input.Present).Value("watermark", input.Watermark)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (s PayInputSnapshot) computedDigest() string { return canonicalbytes.Digest(s.body()) }

// Canonical implements canonicalbytes.Canonicalizer.
func (s PayInputSnapshot) Canonical() []byte {
	if s.Validate() != nil {
		return nil
	}
	return s.body()
}

// Validate implements validation.
func (s PayInputSnapshot) Validate() error {
	if strings.TrimSpace(s.SnapshotID) == "" || strings.TrimSpace(s.PeriodID) == "" {
		return correctionRejected("snapshot", "missing")
	}
	if s.Cutoff.Validate() != nil {
		return correctionRejected("cutoff", "invalid")
	}
	if len(s.Inputs) != 4 {
		return correctionRejected("inputs", "incomplete")
	}
	seen := make(map[InputSource]struct{}, len(s.Inputs))
	previous := ""
	for _, input := range s.Inputs {
		if err := input.Validate(); err != nil {
			return err
		}
		if _, dup := seen[input.Source]; dup {
			return correctionRejected("inputs", "duplicate-source")
		}
		seen[input.Source] = struct{}{}
		if string(input.Source) < previous {
			return correctionRejected("inputs", "unsorted")
		}
		previous = string(input.Source)
	}
	for _, source := range []InputSource{SourceTime, SourceBenefit, SourceTax, SourceGarnishment} {
		if _, ok := seen[source]; !ok {
			return correctionRejected("inputs", "missing-source")
		}
	}
	if s.CanonicalDigest != "" && s.CanonicalDigest != s.computedDigest() {
		return snapshotFrozen("snapshot", "digest-mismatch")
	}
	return nil
}

// Explain renders the bounded human-readable account: snapshot id,
// input count and cutoff only.
func (s PayInputSnapshot) Explain() string {
	return fmt.Sprintf("snapshot %s: inputs=%d cutoff=%s", s.SnapshotID, len(s.Inputs), s.Cutoff.String())
}

// Effective reports whether the frozen snapshot is well-formed.
func (s PayInputSnapshot) Effective() error { return s.Validate() }

// FreezeSnapshot freezes one complete manifest behind the injected
// cutoff. Every source must be present with a watermark at or before
// the cutoff; anything less refuses and freezes nothing.
func FreezeSnapshot(snapshotID, periodID string, inputs []SnapshotInput, now values.Instant) (PayInputSnapshot, error) {
	if strings.TrimSpace(snapshotID) == "" || strings.TrimSpace(periodID) == "" {
		return PayInputSnapshot{}, correctionRejected("snapshot", "missing")
	}
	if now.Validate() != nil {
		return PayInputSnapshot{}, correctionRejected("clock", "invalid")
	}
	snapshot := PayInputSnapshot{
		SnapshotID: snapshotID, PeriodID: periodID,
		Inputs: append([]SnapshotInput(nil), inputs...), Cutoff: now,
	}
	sort.Slice(snapshot.Inputs, func(i, j int) bool { return snapshot.Inputs[i].Source < snapshot.Inputs[j].Source })
	if err := snapshot.Validate(); err != nil {
		return PayInputSnapshot{}, err
	}
	for _, input := range snapshot.Inputs {
		if input.Watermark.After(now) {
			return PayInputSnapshot{}, correctionRejected("watermark", "after-cutoff")
		}
	}
	snapshot.CanonicalDigest = snapshot.computedDigest()
	return snapshot, nil
}

// VerifySnapshot refuses any input set that differs from the frozen
// manifest: after the cutoff the snapshot changes only through a new
// freeze, never through edits.
func VerifySnapshot(snapshot PayInputSnapshot, inputs []SnapshotInput, now values.Instant) error {
	if err := snapshot.Validate(); err != nil {
		return err
	}
	if now.Validate() != nil {
		return correctionRejected("clock", "invalid")
	}
	current := append([]SnapshotInput(nil), inputs...)
	sort.Slice(current, func(i, j int) bool { return current[i].Source < current[j].Source })
	if len(current) != len(snapshot.Inputs) {
		return snapshotFrozen("inputs", "changed-after-cutoff")
	}
	for i := range current {
		want, got := snapshot.Inputs[i], current[i]
		if want.Source != got.Source || want.Revision != got.Revision || want.Digest != got.Digest ||
			want.Present != got.Present || want.Watermark != got.Watermark {
			return snapshotFrozen("inputs", "changed-after-cutoff")
		}
	}
	return nil
}
