package payroll

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

var (
	// ErrInvalidPayrollRelease identifies an incomplete release request.
	ErrInvalidPayrollRelease = errors.New("payroll: invalid payroll release")
	// ErrReleaseStale refuses a lock that does not bind the released run.
	ErrReleaseStale = errors.New("payroll: payroll lock is stale for release")
	// ErrReleaseObligation refuses a release with an unsatisfied obligation.
	ErrReleaseObligation = errors.New("payroll: payroll release obligation is unsatisfied")
	// ErrReleaseDuplicate refuses a repeated release of the same run.
	ErrReleaseDuplicate = errors.New("payroll: payroll release is a duplicate")
	// ErrReleaseRejected is the typed PAYRUN-007 refusal boundary.
	ErrReleaseRejected = errors.New("PAYRUN_007_REJECTED")
)

// ReleaseError reports the offending field and reason without creating an
// authoritative side effect.
type ReleaseError struct {
	Code   string
	Field  string
	Reason string
	Cause  error
}

func (e *ReleaseError) Error() string {
	return fmt.Sprintf("%s: field=%s: %s", e.Code, e.Field, e.Reason)
}

// Is reports the typed PAYRUN-007 boundary and any wrapped cause.
func (e *ReleaseError) Is(target error) bool {
	return target == ErrReleaseRejected || target == e.Cause
}

// Unwrap returns the wrapped cause, if any.
func (e *ReleaseError) Unwrap() error { return e.Cause }

func releaseRefusal(field, reason string, cause error) error {
	return &ReleaseError{Code: ErrReleaseRejected.Error(), Field: field, Reason: reason, Cause: cause}
}

// ReleaseEffects compiles the exact effect manifests a release publishes.
// Each digest is supplied by the owning payments, statements, balances,
// accounting, or reporting subsystem; the release boundary never interprets
// those payloads as facts.
type ReleaseEffects struct {
	PaymentsDigest   string
	StatementsDigest string
	BalancesDigest   string
	AccountingDigest string
	ReportingDigest  string
}

// Validate reports whether every effect manifest is bound.
func (e ReleaseEffects) Validate() error {
	for name, value := range map[string]string{
		"payments_digest": e.PaymentsDigest, "statements_digest": e.StatementsDigest,
		"balances_digest": e.BalancesDigest, "accounting_digest": e.AccountingDigest,
		"reporting_digest": e.ReportingDigest,
	} {
		if strings.TrimSpace(value) == "" {
			return releaseRefusal("effects."+name, "effect manifest is required", ErrInvalidPayrollRelease)
		}
	}
	return nil
}

// ReleaseObligations carries the funding, filing, and settlement receipts a
// release must observe before finalizing effects.
type ReleaseObligations struct {
	FundingDigest    string
	FilingDigest     string
	SettlementDigest string
}

// Validate reports whether every release obligation is satisfied.
func (o ReleaseObligations) Validate() error {
	for name, value := range map[string]string{
		"funding_digest": o.FundingDigest, "filing_digest": o.FilingDigest,
		"settlement_digest": o.SettlementDigest,
	} {
		if strings.TrimSpace(value) == "" {
			return releaseRefusal("obligations."+name, "release obligation is unsatisfied", ErrReleaseObligation)
		}
	}
	return nil
}

// ReleaseRequest is the complete material for finalizing one released run:
// the approval lock, the current released revision, the compiled effects,
// the observed obligations, and the caller idempotency key.
type ReleaseRequest struct {
	Lock           PayrollLock
	Run            PayrollRun
	Effects        ReleaseEffects
	Obligations    ReleaseObligations
	IdempotencyKey string
}

// PayrollRelease is the immutable, digested finalization of one released
// payroll revision.
type PayrollRelease struct {
	ReleaseID      string
	RunID          string
	RunRevision    uint64
	LockDigest     string
	Effects        ReleaseEffects
	Obligations    ReleaseObligations
	IdempotencyKey string
	ReleaseDigest  string
}

func (r PayrollRelease) body() *canonicalbytes.Writer {
	return canonicalbytes.New("hcmnext.domains.payroll.PayrollRelease", 1).
		String("release_id", r.ReleaseID).
		String("run_id", r.RunID).
		Int("run_revision", int64(r.RunRevision)).
		String("lock_digest", r.LockDigest).
		String("effects.payments_digest", r.Effects.PaymentsDigest).
		String("effects.statements_digest", r.Effects.StatementsDigest).
		String("effects.balances_digest", r.Effects.BalancesDigest).
		String("effects.accounting_digest", r.Effects.AccountingDigest).
		String("effects.reporting_digest", r.Effects.ReportingDigest).
		String("obligations.funding_digest", r.Obligations.FundingDigest).
		String("obligations.filing_digest", r.Obligations.FilingDigest).
		String("obligations.settlement_digest", r.Obligations.SettlementDigest).
		String("idempotency_key", r.IdempotencyKey)
}

func (r PayrollRelease) computedDigest() string {
	digest, err := r.body().Digest()
	if err != nil {
		return ""
	}
	return digest
}

// Validate checks the release bindings and self-digest.
func (r PayrollRelease) Validate() error {
	if strings.TrimSpace(r.ReleaseID) == "" || strings.TrimSpace(r.RunID) == "" || r.RunRevision == 0 || strings.TrimSpace(r.LockDigest) == "" || strings.TrimSpace(r.IdempotencyKey) == "" {
		return releaseRefusal("release", "release identity is incomplete", ErrInvalidPayrollRelease)
	}
	if r.ReleaseID != "payroll-release/"+r.IdempotencyKey {
		return releaseRefusal("release_id", "release id is not key-bound", ErrInvalidPayrollRelease)
	}
	if err := r.Effects.Validate(); err != nil {
		return err
	}
	if err := r.Obligations.Validate(); err != nil {
		return err
	}
	if r.ReleaseDigest == "" || r.ReleaseDigest != r.computedDigest() {
		return releaseRefusal("release_digest", "release digest mismatch", ErrInvalidPayrollRelease)
	}
	return nil
}

// Canonical returns the release evidence bytes, or nil when invalid.
func (r PayrollRelease) Canonical() []byte {
	if err := r.Validate(); err != nil {
		return nil
	}
	raw, err := r.body().Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest returns the release digest.
func (r PayrollRelease) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.ReleaseDigest, nil
}

// ReleaseExplanation is the read-only summary of a finalized release.
type ReleaseExplanation struct {
	ReleaseID   string
	RunID       string
	RunRevision uint64
	LockDigest  string
	Digest      string
}

// Explain returns the release facts.
func (r PayrollRelease) Explain() (ReleaseExplanation, error) {
	if err := r.Validate(); err != nil {
		return ReleaseExplanation{}, err
	}
	return ReleaseExplanation{
		ReleaseID: r.ReleaseID, RunID: r.RunID,
		RunRevision: r.RunRevision, LockDigest: r.LockDigest, Digest: r.ReleaseDigest,
	}, nil
}

// ReleasePayroll finalizes the effects of one released run against its
// approval lock. A stale lock, an unsatisfied funding, filing, or settlement
// obligation, a run that is not released, or a duplicate of a prior release
// fails with a typed refusal and yields no release. Prior releases are
// caller-held idempotency evidence: the same idempotency key or the same run
// revision never releases twice. Nothing is mutated.
func ReleasePayroll(req ReleaseRequest, prior []PayrollRelease) (PayrollRelease, error) {
	if err := req.Lock.Validate(); err != nil {
		return PayrollRelease{}, releaseRefusal("lock", "payroll lock is invalid", err)
	}
	if err := req.Run.Validate(); err != nil {
		return PayrollRelease{}, releaseRefusal("run", "run is invalid", err)
	}
	if err := req.Effects.Validate(); err != nil {
		return PayrollRelease{}, err
	}
	if err := req.Obligations.Validate(); err != nil {
		return PayrollRelease{}, err
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return PayrollRelease{}, releaseRefusal("idempotency_key", "idempotency key is required", ErrInvalidPayrollRelease)
	}
	if req.Run.State != PayrollRunStateReleased {
		return PayrollRelease{}, releaseRefusal("run", fmt.Sprintf("release requires a RELEASED run, got %s", req.Run.State), ErrInvalidPayrollRelease)
	}
	if req.Lock.RunID != req.Run.RunID || req.Lock.RunRevision >= req.Run.Revision {
		return PayrollRelease{}, releaseRefusal("lock", "payroll lock does not bind the released run", ErrReleaseStale)
	}
	for _, recorded := range prior {
		if recorded.IdempotencyKey == req.IdempotencyKey || (recorded.RunID == req.Run.RunID && recorded.RunRevision == req.Run.Revision) {
			return PayrollRelease{}, releaseRefusal("idempotency_key", "run revision was already released", ErrReleaseDuplicate)
		}
	}
	release := PayrollRelease{
		ReleaseID: "payroll-release/" + req.IdempotencyKey,
		RunID:     req.Run.RunID, RunRevision: req.Run.Revision,
		LockDigest: req.Lock.LockDigest,
		Effects:    req.Effects, Obligations: req.Obligations,
		IdempotencyKey: req.IdempotencyKey,
	}
	release.ReleaseDigest = release.computedDigest()
	if err := release.Validate(); err != nil {
		return PayrollRelease{}, err
	}
	return release, nil
}
