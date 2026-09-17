package payroll

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// InputCategory is the closed vocabulary of payroll input families bound by a
// locked manifest.
type InputCategory string

const (
	InputCategoryEarnings    InputCategory = "EARNINGS"
	InputCategoryTime        InputCategory = "TIME"
	InputCategoryDeductions  InputCategory = "DEDUCTIONS"
	InputCategoryTax         InputCategory = "TAX"
	InputCategoryBenefits    InputCategory = "BENEFITS"
	InputCategoryCorrections InputCategory = "CORRECTIONS"
)

// Valid reports whether c is a declared payroll input category.
func (c InputCategory) Valid() bool {
	switch c {
	case InputCategoryEarnings, InputCategoryTime, InputCategoryDeductions,
		InputCategoryTax, InputCategoryBenefits, InputCategoryCorrections:
		return true
	default:
		return false
	}
}

// InputDisposition is the closed classification vocabulary for one submitted
// input. Only INCLUDED lines enter the locked manifest; every other
// disposition names the exact gate that excluded the input.
type InputDisposition string

const (
	InputDispositionIncluded   InputDisposition = "INCLUDED"
	InputDispositionStale      InputDisposition = "STALE"
	InputDispositionLate       InputDisposition = "LATE"
	InputDispositionUnapproved InputDisposition = "UNAPPROVED"
	InputDispositionUnattested InputDisposition = "UNATTESTED"
)

// Valid reports whether d is a declared input disposition.
func (d InputDisposition) Valid() bool {
	switch d {
	case InputDispositionIncluded, InputDispositionStale, InputDispositionLate,
		InputDispositionUnapproved, InputDispositionUnattested:
		return true
	default:
		return false
	}
}

var (
	// ErrInvalidPayrollInput identifies an incomplete or incoherent submission.
	ErrInvalidPayrollInput = errors.New("payroll: invalid payroll input")
	// ErrInputLockRejected is the typed PAYRUN-003 refusal boundary.
	ErrInputLockRejected = errors.New("PAYRUN_003_REJECTED")
)

// InputLockError reports the offending field and reason without creating an
// authoritative side effect.
type InputLockError struct {
	Code   string
	Field  string
	Reason string
	Cause  error
}

func (e *InputLockError) Error() string {
	return fmt.Sprintf("%s: field=%s: %s", e.Code, e.Field, e.Reason)
}

// Is reports the typed PAYRUN-003 boundary and any wrapped cause.
func (e *InputLockError) Is(target error) bool {
	return target == ErrInputLockRejected || target == e.Cause
}

// Unwrap returns the wrapped cause, if any.
func (e *InputLockError) Unwrap() error { return e.Cause }

func inputLockRefusal(field, reason string, cause error) error {
	return &InputLockError{Code: ErrInputLockRejected.Error(), Field: field, Reason: reason, Cause: cause}
}

// InputCutoff is the fence a lock applies: inputs observed after Cutoff are
// LATE and inputs whose source watermark precedes MinWatermark are STALE.
type InputCutoff struct {
	Cutoff       values.Instant
	MinWatermark values.Instant
}

// NewInputCutoff validates the cutoff fence. Both instants are required.
func NewInputCutoff(cutoff, minWatermark values.Instant) (InputCutoff, error) {
	c := InputCutoff{Cutoff: cutoff, MinWatermark: minWatermark}
	if err := c.Validate(); err != nil {
		return InputCutoff{}, err
	}
	return c, nil
}

// Validate reports whether the fence carries two usable instants.
func (c InputCutoff) Validate() error {
	if err := c.Cutoff.Validate(); err != nil {
		return inputLockRefusal("cutoff", "cutoff instant is required", err)
	}
	if err := c.MinWatermark.Validate(); err != nil {
		return inputLockRefusal("min_watermark", "minimum source watermark is required", err)
	}
	return nil
}

// SubmittedPayrollInput is one collected input awaiting the cutoff lock.
// ApprovalDigest and AttestationDigest are opaque evidence supplied by the
// owning approval and attestation subsystems; this package classifies on
// presence, never by interpreting those payloads.
type SubmittedPayrollInput struct {
	InputID           string
	WorkerRef         string
	Category          InputCategory
	SourceDigest      string
	ApprovalDigest    string
	AttestationDigest string
	ObservedAt        values.Instant
	SourceWatermark   values.Instant
}

func (in SubmittedPayrollInput) validate() error {
	if strings.TrimSpace(in.InputID) == "" {
		return inputLockRefusal("input_id", "input id is required", ErrInvalidPayrollInput)
	}
	if strings.TrimSpace(in.WorkerRef) == "" {
		return inputLockRefusal("worker_ref", "worker reference is required", ErrInvalidPayrollInput)
	}
	if !in.Category.Valid() {
		return inputLockRefusal("category", fmt.Sprintf("unknown input category %q", in.Category), ErrInvalidPayrollInput)
	}
	if strings.TrimSpace(in.SourceDigest) == "" {
		return inputLockRefusal("source_digest", "source digest is required", ErrInvalidPayrollInput)
	}
	if err := in.ObservedAt.Validate(); err != nil {
		return inputLockRefusal("observed_at", "observed-at instant is required", err)
	}
	if err := in.SourceWatermark.Validate(); err != nil {
		return inputLockRefusal("source_watermark", "source watermark is required", err)
	}
	return nil
}

// classify applies the cutoff fence. Precedence is deterministic: a
// superseded revision is meaningless before timing is considered, a late
// input misses the fence regardless of governance, and approval precedes
// attestation.
func (in SubmittedPayrollInput) classify(cutoff InputCutoff) (InputDisposition, string) {
	if in.SourceWatermark.Before(cutoff.MinWatermark) {
		return InputDispositionStale, "source watermark precedes the required minimum"
	}
	if in.ObservedAt.After(cutoff.Cutoff) {
		return InputDispositionLate, "input was observed after the cutoff"
	}
	if strings.TrimSpace(in.ApprovalDigest) == "" {
		return InputDispositionUnapproved, "approval evidence is absent"
	}
	if strings.TrimSpace(in.AttestationDigest) == "" {
		return InputDispositionUnattested, "attestation evidence is absent"
	}
	return InputDispositionIncluded, ""
}

// LockedInputLine is one included input bound into the manifest.
type LockedInputLine struct {
	InputID           string
	WorkerRef         string
	Category          InputCategory
	SourceDigest      string
	ApprovalDigest    string
	AttestationDigest string
}

// ClassifiedInput is one excluded input with its exact disposition.
type ClassifiedInput struct {
	InputID      string
	WorkerRef    string
	Category     InputCategory
	SourceDigest string
	Disposition  InputDisposition
	Reason       string
}

// LockedInputManifest is the immutable, digested result of collecting inputs
// at cutoff. It binds the run revision, the frozen population, the fence,
// every included line, and every classified exclusion.
type LockedInputManifest struct {
	RunID            string
	RunRevision      uint64
	PopulationDigest string
	Cutoff           values.Instant
	MinWatermark     values.Instant
	Included         []LockedInputLine
	Excluded         []ClassifiedInput
	ManifestDigest   string
}

func (m LockedInputManifest) body() *canonicalbytes.Writer {
	w := canonicalbytes.New("hcmnext.domains.payroll.LockedInputManifest", 1).
		String("run_id", m.RunID).
		Int("run_revision", int64(m.RunRevision)).
		String("population_digest", m.PopulationDigest).
		Value("cutoff", m.Cutoff).
		Value("min_watermark", m.MinWatermark).
		Count("included", len(m.Included))
	for _, line := range m.Included {
		w.String("included.input_id", line.InputID).
			String("included.worker_ref", line.WorkerRef).
			String("included.category", string(line.Category)).
			String("included.source_digest", line.SourceDigest).
			String("included.approval_digest", line.ApprovalDigest).
			String("included.attestation_digest", line.AttestationDigest)
	}
	w.Count("excluded", len(m.Excluded))
	for _, excluded := range m.Excluded {
		w.String("excluded.input_id", excluded.InputID).
			String("excluded.worker_ref", excluded.WorkerRef).
			String("excluded.category", string(excluded.Category)).
			String("excluded.source_digest", excluded.SourceDigest).
			String("excluded.disposition", string(excluded.Disposition)).
			String("excluded.reason", excluded.Reason)
	}
	return w
}

func (m LockedInputManifest) computedDigest() string {
	digest, err := m.body().Digest()
	if err != nil {
		return ""
	}
	return digest
}

// Validate checks the manifest bindings and self-digest.
func (m LockedInputManifest) Validate() error {
	if strings.TrimSpace(m.RunID) == "" || m.RunRevision == 0 {
		return inputLockRefusal("run", "run identity and revision are required", ErrInvalidPayrollInput)
	}
	if strings.TrimSpace(m.PopulationDigest) == "" {
		return inputLockRefusal("population_digest", "population digest is required", ErrInvalidPayrollInput)
	}
	if err := m.Cutoff.Validate(); err != nil {
		return inputLockRefusal("cutoff", "cutoff instant is required", err)
	}
	if err := m.MinWatermark.Validate(); err != nil {
		return inputLockRefusal("min_watermark", "minimum source watermark is required", err)
	}
	seen := make(map[string]struct{}, len(m.Included)+len(m.Excluded))
	for _, line := range m.Included {
		if strings.TrimSpace(line.InputID) == "" || strings.TrimSpace(line.WorkerRef) == "" || !line.Category.Valid() || strings.TrimSpace(line.SourceDigest) == "" {
			return inputLockRefusal("included", "included line is incomplete", ErrInvalidPayrollInput)
		}
		if strings.TrimSpace(line.ApprovalDigest) == "" || strings.TrimSpace(line.AttestationDigest) == "" {
			return inputLockRefusal("included", "included line lacks governance evidence", ErrInvalidPayrollInput)
		}
		if _, ok := seen[line.InputID]; ok {
			return inputLockRefusal("included", "duplicate input id", ErrInvalidPayrollInput)
		}
		seen[line.InputID] = struct{}{}
	}
	for _, excluded := range m.Excluded {
		if strings.TrimSpace(excluded.InputID) == "" || !excluded.Disposition.Valid() || excluded.Disposition == InputDispositionIncluded || strings.TrimSpace(excluded.Reason) == "" {
			return inputLockRefusal("excluded", "excluded classification is incomplete", ErrInvalidPayrollInput)
		}
		if _, ok := seen[excluded.InputID]; ok {
			return inputLockRefusal("excluded", "duplicate input id", ErrInvalidPayrollInput)
		}
		seen[excluded.InputID] = struct{}{}
	}
	if m.ManifestDigest == "" || m.ManifestDigest != m.computedDigest() {
		return inputLockRefusal("manifest_digest", "manifest digest mismatch", ErrInvalidPayrollInput)
	}
	return nil
}

// Canonical returns the manifest evidence bytes, or nil when invalid.
func (m LockedInputManifest) Canonical() []byte {
	if err := m.Validate(); err != nil {
		return nil
	}
	raw, err := m.body().Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest returns the manifest digest.
func (m LockedInputManifest) Digest() (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	return m.ManifestDigest, nil
}

// InputManifestExplanation is the read-only summary of a locked manifest.
type InputManifestExplanation struct {
	RunID          string
	RunRevision    uint64
	IncludedCount  int
	ExcludedCount  int
	ByDisposition  map[InputDisposition]int
	Digest         string
	PopulationBind string
}

// Explain returns the manifest facts and per-disposition exclusion counts.
func (m LockedInputManifest) Explain() (InputManifestExplanation, error) {
	if err := m.Validate(); err != nil {
		return InputManifestExplanation{}, err
	}
	by := make(map[InputDisposition]int, len(m.Excluded))
	for _, excluded := range m.Excluded {
		by[excluded.Disposition]++
	}
	return InputManifestExplanation{
		RunID: m.RunID, RunRevision: m.RunRevision,
		IncludedCount: len(m.Included), ExcludedCount: len(m.Excluded),
		ByDisposition: by, Digest: m.ManifestDigest, PopulationBind: m.PopulationDigest,
	}, nil
}

// LockPayrollInputs collects submissions against the run's current frozen
// population and locks them at the cutoff fence. Inputs for workers outside
// the frozen population, duplicate input ids, and unknown categories refuse
// the whole lock; stale, late, unapproved, and unattested inputs are
// classified into the manifest and never silently included. The lock never
// mutates the run, the population, or the caller submissions.
func LockPayrollInputs(run PayrollRun, population FrozenPopulation, cutoff InputCutoff, inputs []SubmittedPayrollInput) (LockedInputManifest, error) {
	if err := run.Validate(); err != nil {
		return LockedInputManifest{}, inputLockRefusal("run", "run is invalid", err)
	}
	if run.State != PayrollRunStateDraft {
		return LockedInputManifest{}, inputLockRefusal("run", fmt.Sprintf("inputs lock requires a DRAFT run, got %s", run.State), nil)
	}
	if population.State == PopulationStateSuperseded {
		return LockedInputManifest{}, ErrPopulationSuperseded
	}
	if population.State != PopulationStateFrozen {
		return LockedInputManifest{}, ErrPopulationUnfrozen
	}
	if err := population.Validate(); err != nil {
		return LockedInputManifest{}, err
	}
	if population.RunID != run.RunID || population.Binding != run.Population || population.PayGroupRef != run.PayGroupRef {
		return LockedInputManifest{}, ErrPopulationBindingMismatch
	}
	if err := cutoff.Validate(); err != nil {
		return LockedInputManifest{}, err
	}
	byWorker := make(map[string]struct{}, len(population.Members))
	for _, member := range population.Members {
		byWorker[member.workerRef()] = struct{}{}
	}
	seen := make(map[string]struct{}, len(inputs))
	manifest := LockedInputManifest{
		RunID: run.RunID, RunRevision: run.Revision,
		PopulationDigest: population.Digest,
		Cutoff:           cutoff.Cutoff,
		MinWatermark:     cutoff.MinWatermark,
	}
	for _, in := range inputs {
		if err := in.validate(); err != nil {
			return LockedInputManifest{}, err
		}
		if _, ok := byWorker[in.WorkerRef]; !ok {
			return LockedInputManifest{}, inputLockRefusal("worker_ref", fmt.Sprintf("worker %q is not in the frozen population", in.WorkerRef), ErrInvalidPayrollInput)
		}
		if _, ok := seen[in.InputID]; ok {
			return LockedInputManifest{}, inputLockRefusal("input_id", fmt.Sprintf("duplicate input %q", in.InputID), ErrInvalidPayrollInput)
		}
		seen[in.InputID] = struct{}{}
		disposition, reason := in.classify(cutoff)
		if disposition == InputDispositionIncluded {
			manifest.Included = append(manifest.Included, LockedInputLine{
				InputID: in.InputID, WorkerRef: in.WorkerRef, Category: in.Category,
				SourceDigest: in.SourceDigest, ApprovalDigest: in.ApprovalDigest, AttestationDigest: in.AttestationDigest,
			})
			continue
		}
		manifest.Excluded = append(manifest.Excluded, ClassifiedInput{
			InputID: in.InputID, WorkerRef: in.WorkerRef, Category: in.Category,
			SourceDigest: in.SourceDigest, Disposition: disposition, Reason: reason,
		})
	}
	sort.Slice(manifest.Included, func(i, j int) bool { return manifest.Included[i].InputID < manifest.Included[j].InputID })
	sort.Slice(manifest.Excluded, func(i, j int) bool { return manifest.Excluded[i].InputID < manifest.Excluded[j].InputID })
	manifest.ManifestDigest = manifest.computedDigest()
	if err := manifest.Validate(); err != nil {
		return LockedInputManifest{}, err
	}
	return manifest, nil
}
