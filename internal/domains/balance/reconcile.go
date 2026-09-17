package balance

// This file owns BAL-007: reconciling an external system's observed balance
// against this package's canonical derivation. The canonical balance is
// authoritative; the external observer always owns the repair, so every
// verdict -- match or mismatch -- names the observer as Owner along with the
// observed dimensions and period and the expected/observed/delta amounts.
//
// A MATCH requires all of: the same account, period and effective-as-of
// instant; a cited basis digest equal to the canonical derivation digest; an
// observation instant no earlier than the canonical known-at (freshness);
// coverage of exactly the canonical counted entries (completeness); and
// amount equality at the canonical scale. A stale or partial observation can
// therefore never report a match -- it reports a mismatch that tells the
// owner what to re-observe.
import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrReconciliationInvalid identifies a reconciliation request that cannot
// be decided: a malformed envelope, never a mere disagreement (which is a
// MISMATCH verdict, not an error).
var ErrReconciliationInvalid = errors.New("balance: invalid reconciliation request")

// ReconciliationVerdict is the closed outcome vocabulary.
type ReconciliationVerdict string

const (
	ReconciliationMatch    ReconciliationVerdict = "MATCH"
	ReconciliationMismatch ReconciliationVerdict = "MISMATCH"
)

// ReconciliationReason names exactly why the verdict was reached.
type ReconciliationReason string

const (
	ReasonValueMatch       ReconciliationReason = "VALUE_MATCH"
	ReasonAccountMismatch  ReconciliationReason = "ACCOUNT_MISMATCH"
	ReasonPeriodMismatch   ReconciliationReason = "PERIOD_MISMATCH"
	ReasonAsOfMismatch     ReconciliationReason = "EFFECTIVE_AS_OF_MISMATCH"
	ReasonStaleObservation ReconciliationReason = "STALE_OBSERVATION"
	ReasonPartialCoverage  ReconciliationReason = "PARTIAL_COVERAGE"
	ReasonBasisMismatch    ReconciliationReason = "BASIS_MISMATCH"
	ReasonValueMismatch    ReconciliationReason = "VALUE_MISMATCH"
)

// ExternalBalance is one external system's observed balance. BasisDigest is
// the canonical derivation digest the observer claims to have read and
// CoveredDigests are the canonical counted-entry digests its amount
// incorporates; together they prove the observation is fresh and complete
// instead of asking the reconciler to trust it.
type ExternalBalance struct {
	AccountID      string
	Dimensions     map[string]string
	Period         string
	EffectiveAsOf  values.Instant
	BasisDigest    string
	CoveredDigests []string
	Observed       values.Decimal
	ObservedAt     values.Instant
	SourceID       string
}

// ReconciliationRequest pairs the authoritative derivation with the
// observation under test.
type ReconciliationRequest struct {
	Canonical AuthorizedBalance
	Observed  ExternalBalance
}

// BalanceDiscrepancy is the decided reconciliation. Delta is
// observed-minus-expected at the canonical scale, so Expected+Delta always
// equals Observed. Owner is the external source: it repairs by re-observing
// (stale/partial/basis findings) or by correcting its own books (value
// findings), never by editing the canonical ledger.
type BalanceDiscrepancy struct {
	Verdict         ReconciliationVerdict
	Reason          ReconciliationReason
	Dimensions      map[string]string
	Period          string
	Expected        values.Decimal
	Observed        values.Decimal
	Delta           values.Decimal
	Owner           string
	CanonicalDigest string
	BasisDigest     string
	Digest          string
}

// ReconcileExternalBalance decides the observation against the canonical
// derivation. It never mutates, persists, or emits anything.
func ReconcileExternalBalance(req ReconciliationRequest) (BalanceDiscrepancy, error) {
	if strings.TrimSpace(req.Canonical.Digest) == "" {
		return BalanceDiscrepancy{}, fmt.Errorf("%w: canonical derivation digest is required", ErrReconciliationInvalid)
	}
	if !req.Canonical.KnownAt.IsSet() {
		return BalanceDiscrepancy{}, fmt.Errorf("%w: canonical known-at is required", ErrReconciliationInvalid)
	}
	if err := req.Canonical.Ending.Validate(); err != nil {
		return BalanceDiscrepancy{}, fmt.Errorf("%w: canonical ending: %v", ErrReconciliationInvalid, err)
	}
	obs := req.Observed
	if strings.TrimSpace(obs.AccountID) == "" || strings.TrimSpace(obs.Period) == "" || strings.TrimSpace(obs.SourceID) == "" {
		return BalanceDiscrepancy{}, fmt.Errorf("%w: account, period and source are required", ErrReconciliationInvalid)
	}
	if !obs.EffectiveAsOf.IsSet() || !obs.ObservedAt.IsSet() {
		return BalanceDiscrepancy{}, fmt.Errorf("%w: effective-as-of and observed-at are required", ErrReconciliationInvalid)
	}
	if err := obs.Observed.Validate(); err != nil {
		return BalanceDiscrepancy{}, fmt.Errorf("%w: observed amount: %v", ErrReconciliationInvalid, err)
	}
	expected := req.Canonical.Ending
	observed, err := obs.Observed.Quantize(expected.Scale(), expected.Rounding())
	if err != nil {
		return BalanceDiscrepancy{}, fmt.Errorf("%w: observed amount cannot be expressed at canonical scale %d: %v", ErrReconciliationInvalid, expected.Scale(), err)
	}
	discrepancy := BalanceDiscrepancy{
		Dimensions:      mapsCopy(obs.Dimensions),
		Period:          obs.Period,
		Expected:        expected,
		Observed:        observed,
		Owner:           obs.SourceID,
		CanonicalDigest: req.Canonical.Digest,
		BasisDigest:     obs.BasisDigest,
	}
	decide := func(verdict ReconciliationVerdict, reason ReconciliationReason) (BalanceDiscrepancy, error) {
		delta, err := observed.Sub(expected)
		if err != nil {
			return BalanceDiscrepancy{}, fmt.Errorf("%w: delta: %v", ErrReconciliationInvalid, err)
		}
		discrepancy.Verdict, discrepancy.Reason, discrepancy.Delta = verdict, reason, delta
		digest, err := discrepancy.canonicalDigest()
		if err != nil {
			return BalanceDiscrepancy{}, fmt.Errorf("%w: digest: %v", ErrReconciliationInvalid, err)
		}
		discrepancy.Digest = digest
		return discrepancy, nil
	}
	switch {
	case obs.AccountID != req.Canonical.AccountID:
		return decide(ReconciliationMismatch, ReasonAccountMismatch)
	case obs.Period != req.Canonical.Period():
		return decide(ReconciliationMismatch, ReasonPeriodMismatch)
	case obs.EffectiveAsOf.Compare(req.Canonical.EffectiveAsOf) != 0:
		return decide(ReconciliationMismatch, ReasonAsOfMismatch)
	case obs.BasisDigest != req.Canonical.Digest:
		return decide(ReconciliationMismatch, ReasonBasisMismatch)
	case obs.ObservedAt.Before(req.Canonical.KnownAt):
		return decide(ReconciliationMismatch, ReasonStaleObservation)
	case !sameCoverage(req.Canonical, obs.CoveredDigests):
		return decide(ReconciliationMismatch, ReasonPartialCoverage)
	case !observed.Equal(expected):
		return decide(ReconciliationMismatch, ReasonValueMismatch)
	default:
		return decide(ReconciliationMatch, ReasonValueMatch)
	}
}

// Period reports the canonical derivation's balance period: every counted
// entry validates against one definition, so the first counted entry's
// period names it.
func (b AuthorizedBalance) Period() string {
	if len(b.Entries) > 0 {
		return b.Entries[0].Period
	}
	if len(b.Adjustments) > 0 {
		return b.Adjustments[0].Period
	}
	return ""
}

// sameCoverage reports whether covered names exactly the canonical counted
// entry digests -- no missing entry, no extra one, duplicates included.
func sameCoverage(canonical AuthorizedBalance, covered []string) bool {
	want := map[string]int{}
	for _, e := range canonical.Entries {
		want[e.Digest()]++
	}
	for _, e := range canonical.Adjustments {
		want[e.Digest()]++
	}
	got := map[string]int{}
	for _, digest := range covered {
		got[digest]++
	}
	if len(got) != len(want) {
		return false
	}
	for digest, count := range want {
		if got[digest] != count {
			return false
		}
	}
	return true
}

func (d BalanceDiscrepancy) canonicalDigest() (string, error) {
	keys := make([]string, 0, len(d.Dimensions))
	for k := range d.Dimensions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	w := canonicalbytes.New("hcmnext.domains.balance.BalanceDiscrepancy", 1).
		String("verdict", string(d.Verdict)).
		String("reason", string(d.Reason)).
		String("period", d.Period).
		Value("expected", d.Expected).
		Value("observed", d.Observed).
		Value("delta", d.Delta).
		String("owner", d.Owner).
		String("canonical", d.CanonicalDigest).
		String("basis", d.BasisDigest).
		Count("dimensions", len(keys))
	for _, k := range keys {
		w = w.String("dimension."+k, d.Dimensions[k])
	}
	return w.Digest()
}
