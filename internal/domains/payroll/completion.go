package payroll

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

var (
	// ErrInvalidCompletion identifies a malformed completion observation.
	ErrInvalidCompletion = errors.New("payroll: invalid payroll completion")
	// ErrCompletionRejected is the typed PAYRUN-009 refusal boundary.
	ErrCompletionRejected = errors.New("PAYRUN_009_REJECTED")
)

// CompletionError reports the offending field and reason without creating an
// authoritative side effect.
type CompletionError struct {
	Code   string
	Field  string
	Reason string
	Cause  error
}

func (e *CompletionError) Error() string {
	return fmt.Sprintf("%s: field=%s: %s", e.Code, e.Field, e.Reason)
}

// Is reports the typed PAYRUN-009 boundary and any wrapped cause.
func (e *CompletionError) Is(target error) bool {
	return target == ErrCompletionRejected || target == e.Cause
}

// Unwrap returns the wrapped cause, if any.
func (e *CompletionError) Unwrap() error { return e.Cause }

func completionRefusal(field, reason string, cause error) error {
	return &CompletionError{Code: ErrCompletionRejected.Error(), Field: field, Reason: reason, Cause: cause}
}

// CompletionDimension is the closed vocabulary of reconciled completion
// dimensions. Every dimension reports independently.
type CompletionDimension string

const (
	CompletionEmployeeResults      CompletionDimension = "EMPLOYEE_RESULTS"
	CompletionPayments             CompletionDimension = "PAYMENTS"
	CompletionGeneralLedger        CompletionDimension = "GENERAL_LEDGER"
	CompletionTaxFilings           CompletionDimension = "TAX_FILINGS"
	CompletionStatements           CompletionDimension = "STATEMENTS"
	CompletionExternalObservations CompletionDimension = "EXTERNAL_OBSERVATIONS"
)

// AllCompletionDimensions returns every reconciled dimension in canonical
// order.
func AllCompletionDimensions() []CompletionDimension {
	return []CompletionDimension{
		CompletionEmployeeResults, CompletionPayments, CompletionGeneralLedger,
		CompletionTaxFilings, CompletionStatements, CompletionExternalObservations,
	}
}

// Valid reports whether d is a declared completion dimension.
func (d CompletionDimension) Valid() bool {
	for _, known := range AllCompletionDimensions() {
		if d == known {
			return true
		}
	}
	return false
}

// CompletionStatus is the closed per-dimension completion vocabulary.
type CompletionStatus string

const (
	CompletionConsistent     CompletionStatus = "CONSISTENT"
	CompletionDegraded       CompletionStatus = "DEGRADED"
	CompletionRepairRequired CompletionStatus = "REPAIR_REQUIRED"
	CompletionUnknown        CompletionStatus = "UNKNOWN"
)

// Valid reports whether s is a declared completion status.
func (s CompletionStatus) Valid() bool {
	switch s {
	case CompletionConsistent, CompletionDegraded, CompletionRepairRequired, CompletionUnknown:
		return true
	default:
		return false
	}
}

// severity ranks statuses for the overall rollup: repair dominates unknown,
// unknown dominates degraded, degraded dominates consistent. Unobserved work
// is worse than degraded work, and no success masks another failure.
func (s CompletionStatus) severity() int {
	switch s {
	case CompletionRepairRequired:
		return 3
	case CompletionUnknown:
		return 2
	case CompletionDegraded:
		return 1
	default:
		return 0
	}
}

// rollup returns the worse of two statuses.
func (s CompletionStatus) rollup(o CompletionStatus) CompletionStatus {
	if o.severity() > s.severity() {
		return o
	}
	return s
}

// DimensionObservation is one independently reported dimension: its status
// and the evidence digest behind it. An UNKNOWN dimension carries no
// evidence; every other status requires it.
type DimensionObservation struct {
	Dimension      CompletionDimension
	Status         CompletionStatus
	EvidenceDigest string
}

// DimensionResult is the reconciled record for one dimension.
type DimensionResult struct {
	Dimension      CompletionDimension
	Status         CompletionStatus
	EvidenceDigest string
}

// CompletionReconciliation is the immutable, digested multidimensional
// completion record for one released payroll revision.
type CompletionReconciliation struct {
	ReconciliationID     string
	ReleaseID            string
	RunID                string
	RunRevision          uint64
	ReleaseDigest        string
	Dimensions           []DimensionResult
	Overall              CompletionStatus
	ReconciliationDigest string
}

func (r CompletionReconciliation) body() *canonicalbytes.Writer {
	w := canonicalbytes.New("hcmnext.domains.payroll.CompletionReconciliation", 1).
		String("reconciliation_id", r.ReconciliationID).
		String("release_id", r.ReleaseID).
		String("run_id", r.RunID).
		Int("run_revision", int64(r.RunRevision)).
		String("release_digest", r.ReleaseDigest).
		String("overall", string(r.Overall)).
		Int("dimensions", int64(len(r.Dimensions)))
	for _, d := range r.Dimensions {
		prefix := "dimension." + string(d.Dimension) + "."
		w = w.String(prefix+"status", string(d.Status)).
			String(prefix+"evidence", d.EvidenceDigest)
	}
	return w
}

func (r CompletionReconciliation) computedDigest() string {
	digest, err := r.body().Digest()
	if err != nil {
		return ""
	}
	return digest
}

// Validate checks release identity, dimension coverage and order, evidence
// bindings, the severity rollup, and the self-digest.
func (r CompletionReconciliation) Validate() error {
	if strings.TrimSpace(r.ReleaseID) == "" || strings.TrimSpace(r.RunID) == "" || r.RunRevision == 0 || strings.TrimSpace(r.ReleaseDigest) == "" {
		return completionRefusal("reconciliation", "reconciliation identity is incomplete", ErrInvalidCompletion)
	}
	if r.ReconciliationID != "payroll-completion/"+r.ReleaseDigest {
		return completionRefusal("reconciliation_id", "reconciliation id is not release-bound", ErrInvalidCompletion)
	}
	want := AllCompletionDimensions()
	if len(r.Dimensions) != len(want) {
		return completionRefusal("dimensions", "every completion dimension must report", ErrInvalidCompletion)
	}
	overall := CompletionConsistent
	for i, d := range r.Dimensions {
		if d.Dimension != want[i] {
			return completionRefusal("dimensions", "dimensions must cover every vocabulary entry in canonical order", ErrInvalidCompletion)
		}
		if !d.Status.Valid() {
			return completionRefusal("dimensions.status", "dimension "+string(d.Dimension)+" status is unknown", ErrInvalidCompletion)
		}
		if d.Status == CompletionUnknown {
			if strings.TrimSpace(d.EvidenceDigest) != "" {
				return completionRefusal("dimensions.evidence", "dimension "+string(d.Dimension)+" is unobserved and carries no evidence", ErrInvalidCompletion)
			}
		} else if strings.TrimSpace(d.EvidenceDigest) == "" {
			return completionRefusal("dimensions.evidence", "dimension "+string(d.Dimension)+" requires evidence", ErrInvalidCompletion)
		}
		overall = overall.rollup(d.Status)
	}
	if r.Overall != overall {
		return completionRefusal("overall", "overall does not roll up the dimensions", ErrInvalidCompletion)
	}
	if r.ReconciliationDigest == "" || r.ReconciliationDigest != r.computedDigest() {
		return completionRefusal("reconciliation_digest", "reconciliation digest mismatch", ErrInvalidCompletion)
	}
	return nil
}

// Canonical returns the reconciliation evidence bytes, or nil when invalid.
func (r CompletionReconciliation) Canonical() []byte {
	if err := r.Validate(); err != nil {
		return nil
	}
	raw, err := r.body().Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest returns the reconciliation digest.
func (r CompletionReconciliation) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.ReconciliationDigest, nil
}

// CompletionExplanation is the read-only summary of one reconciliation.
type CompletionExplanation struct {
	ReconciliationID string
	RunID            string
	Overall          CompletionStatus
	Digest           string
}

// Explain returns the reconciliation facts.
func (r CompletionReconciliation) Explain() (CompletionExplanation, error) {
	if err := r.Validate(); err != nil {
		return CompletionExplanation{}, err
	}
	return CompletionExplanation{
		ReconciliationID: r.ReconciliationID, RunID: r.RunID,
		Overall: r.Overall, Digest: r.ReconciliationDigest,
	}, nil
}

// ReconcileCompletion reconciles the multidimensional completion of one
// released payroll revision. Every dimension reports independently; a
// dimension with no observation reports UNKNOWN, never an assumed success.
// The overall status is the worst observed severity, so one success can
// never mask another failure. Nothing is mutated.
func ReconcileCompletion(release PayrollRelease, observations []DimensionObservation) (CompletionReconciliation, error) {
	if err := release.Validate(); err != nil {
		return CompletionReconciliation{}, completionRefusal("release", "released payroll is invalid", err)
	}
	byDim := map[CompletionDimension]DimensionObservation{}
	for _, o := range observations {
		if !o.Dimension.Valid() {
			return CompletionReconciliation{}, completionRefusal("dimensions.dimension", "observation dimension is unknown", ErrInvalidCompletion)
		}
		if !o.Status.Valid() {
			return CompletionReconciliation{}, completionRefusal("dimensions.status", "observation status is unknown", ErrInvalidCompletion)
		}
		if _, dup := byDim[o.Dimension]; dup {
			return CompletionReconciliation{}, completionRefusal("dimensions", "dimension "+string(o.Dimension)+" reports twice", ErrInvalidCompletion)
		}
		if o.Status != CompletionUnknown && strings.TrimSpace(o.EvidenceDigest) == "" {
			return CompletionReconciliation{}, completionRefusal("dimensions.evidence", "dimension "+string(o.Dimension)+" requires evidence", ErrInvalidCompletion)
		}
		if o.Status == CompletionUnknown && strings.TrimSpace(o.EvidenceDigest) != "" {
			return CompletionReconciliation{}, completionRefusal("dimensions.evidence", "dimension "+string(o.Dimension)+" is unobserved and carries no evidence", ErrInvalidCompletion)
		}
		byDim[o.Dimension] = o
	}
	recon := CompletionReconciliation{
		ReconciliationID: "payroll-completion/" + release.ReleaseDigest,
		ReleaseID:        release.ReleaseID, RunID: release.RunID,
		RunRevision: release.RunRevision, ReleaseDigest: release.ReleaseDigest,
		Overall: CompletionConsistent,
	}
	for _, dim := range AllCompletionDimensions() {
		o, ok := byDim[dim]
		if !ok {
			recon.Dimensions = append(recon.Dimensions, DimensionResult{Dimension: dim, Status: CompletionUnknown})
		} else {
			recon.Dimensions = append(recon.Dimensions, DimensionResult{Dimension: dim, Status: o.Status, EvidenceDigest: o.EvidenceDigest})
		}
		recon.Overall = recon.Overall.rollup(recon.Dimensions[len(recon.Dimensions)-1].Status)
	}
	recon.ReconciliationDigest = recon.computedDigest()
	if err := recon.Validate(); err != nil {
		return CompletionReconciliation{}, err
	}
	return recon, nil
}
