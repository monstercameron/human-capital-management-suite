// BEN-008: reconcile carrier coverage and payroll deductions.
//
// ReconcileCoverage compares the expected election (dependents, effective
// dates, rates, deductions) against carrier and payroll observations.
// Exact agreement is MATCH; anything partial, stale or unknown creates a
// scoped repair case. The function is kernel-pure.
package benefits

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrCoverageReconRejected is the BEN-008 sentinel for malformed input.
	ErrCoverageReconRejected = errors.New("BEN_008_REJECTED")
)

// CoverageReconRejection is the stable BEN-008 failure shape.
type CoverageReconRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *CoverageReconRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrCoverageReconRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the BEN_008_REJECTED sentinel to errors.Is.
func (r *CoverageReconRejection) Unwrap() error { return ErrCoverageReconRejected }

func coverageReject(field, state, reason string) error {
	return &CoverageReconRejection{Field: field, State: state, Version: ElectionVersion, Reason: reason}
}

// ReconOutcome is the closed BEN-008 comparison vocabulary.
type ReconOutcome string

const (
	ReconMatch    ReconOutcome = "MATCH"
	ReconMismatch ReconOutcome = "MISMATCH"
	ReconPartial  ReconOutcome = "PARTIAL"
	ReconStale    ReconOutcome = "STALE"
	ReconUnknown  ReconOutcome = "UNKNOWN"
)

// CarrierObservation is what the carrier reports for one election.
type CarrierObservation struct {
	Tier          TierCode
	Dependents    []string
	EffectiveDate time.Time
	FreshAsOf     time.Time
	Stale         bool
	Unknown       bool
}

// PayrollObservation is what payroll deducted for one election.
type PayrollObservation struct {
	PerPeriodDeduction values.Decimal
	RateVersion        string
	FreshAsOf          time.Time
	Stale              bool
	Unknown            bool
}

// CoverageExpectation is the election truth both sides must match.
type CoverageExpectation struct {
	Tenant             string
	WorkerRef          string
	ElectionDigest     string
	Tier               TierCode
	Dependents         []string
	EffectiveDate      time.Time
	RateVersion        string
	PerPeriodDeduction values.Decimal
}

// RepairCase is the scoped repair created for any non-match.
type RepairCase struct {
	CaseID    string
	Scope     string
	Fields    []string
	CreatedAt time.Time
}

// CoverageReconResult is the deterministic reconciliation outcome.
type CoverageReconResult struct {
	Outcome ReconOutcome
	Fields  []string
	Repair  *RepairCase
	Digest  string
}

func (r CoverageReconResult) computedDigest(exp CoverageExpectation) string {
	fields := append([]string(nil), r.Fields...)
	sort.Strings(fields)
	w := canonicalbytes.New("hcmnext.domains.benefits.CoverageRecon", 1).
		String("tenant", exp.Tenant).
		String("worker_ref", exp.WorkerRef).
		String("election_digest", exp.ElectionDigest).
		String("outcome", string(r.Outcome)).
		SortedStrings("fields", fields)
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ReconcileCoverage compares expected election truth against carrier and
// payroll observations as of now. Partial, stale or unknown observations
// create a repair case; exact agreement is the only MATCH.
func ReconcileCoverage(exp CoverageExpectation, carrier CarrierObservation, payroll PayrollObservation, now time.Time) (CoverageReconResult, error) {
	if strings.TrimSpace(exp.Tenant) == "" || strings.TrimSpace(exp.WorkerRef) == "" {
		return CoverageReconResult{}, coverageReject("coverage.scope", "MISSING", "tenant and worker are required")
	}
	if strings.TrimSpace(exp.ElectionDigest) == "" {
		return CoverageReconResult{}, coverageReject("coverage.election_digest", "MISSING", "election digest is required")
	}
	if !exp.Tier.Valid() {
		return CoverageReconResult{}, coverageReject("coverage.tier", "UNDECLARED", "expected tier is not declared")
	}
	if now.IsZero() {
		return CoverageReconResult{}, coverageReject("coverage.now", "MISSING", "evaluation instant is required")
	}
	if err := exp.PerPeriodDeduction.Validate(); err != nil {
		return CoverageReconResult{}, coverageReject("coverage.per_period_deduction", "INVALID", "expected deduction is not a valid decimal")
	}
	res := CoverageReconResult{}
	if carrier.Unknown || payroll.Unknown {
		res.Outcome = ReconUnknown
		res.Fields = []string{"carrier", "payroll"}
	} else if carrier.Stale || payroll.Stale {
		res.Outcome = ReconStale
		if carrier.Stale {
			res.Fields = append(res.Fields, "carrier")
		}
		if payroll.Stale {
			res.Fields = append(res.Fields, "payroll")
		}
	} else {
		var fields []string
		if carrier.Tier != exp.Tier {
			fields = append(fields, "carrier.tier")
		}
		if !sameStrings(sortedCopy(carrier.Dependents), sortedCopy(exp.Dependents)) {
			fields = append(fields, "carrier.dependents")
		}
		if !carrier.EffectiveDate.Equal(exp.EffectiveDate.UTC()) {
			fields = append(fields, "carrier.effective_date")
		}
		if payroll.RateVersion != exp.RateVersion {
			fields = append(fields, "payroll.rate_version")
		}
		if !payroll.PerPeriodDeduction.Equal(exp.PerPeriodDeduction) {
			fields = append(fields, "payroll.deduction")
		}
		switch len(fields) {
		case 0:
			res.Outcome = ReconMatch
		case 1, 2:
			res.Outcome = ReconPartial
			res.Fields = fields
		default:
			res.Outcome = ReconMismatch
			res.Fields = fields
		}
	}
	if res.Outcome != ReconMatch {
		sort.Strings(res.Fields)
		res.Repair = &RepairCase{
			CaseID:    "repair:" + exp.ElectionDigest,
			Scope:     exp.Tenant + "/" + exp.WorkerRef,
			Fields:    append([]string(nil), res.Fields...),
			CreatedAt: now.UTC(),
		}
	}
	res.Digest = res.computedDigest(exp)
	return res, nil
}
