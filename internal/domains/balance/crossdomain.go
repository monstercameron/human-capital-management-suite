package balance

// This file owns BAL-010: the cross-domain balance conformance prover. One
// shared entry/time/correction model must pass a conformance vector per
// domain -- Payroll, Leave, Time, Benefits and Tax -- without erasing the
// statutory/domain-specific composition each vector declares.
//
// The prover is kernel-pure: it reads definitions, entries and instants,
// keeps no state, and offers no sink for rows, events, outbox entries, human
// work or provider requests, so a rejected vector and an accepted one alike
// persist nothing.
import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Domain identities in the BAL-010 conformance vector set.
const (
	DomainPayroll  = "PAYROLL"
	DomainLeave    = "LEAVE"
	DomainTime     = "TIME"
	DomainBenefits = "BENEFITS"
	DomainTax      = "TAX"
)

func isKnownDomain(domain string) bool {
	switch domain {
	case DomainPayroll, DomainLeave, DomainTime, DomainBenefits, DomainTax:
		return true
	}
	return false
}

var (
	// ErrCrossDomainRejected identifies a domain vector that cannot be
	// explained by the shared model. It always carries a BAL_010_REJECTED
	// marker with the offending field, state and definition version.
	ErrCrossDomainRejected = errors.New("balance: cross-domain conformance rejected")
)

// Closed vocabulary of cross-domain rejection states.
const (
	crossStateDefinitionInvalid = "DEFINITION_INVALID"
	crossStateRequestInvalid    = "REQUEST_INVALID"
	crossStateEntryInvalid      = "ENTRY_INVALID"
	crossStateMissingDimension  = "MISSING_DIMENSION"
	crossStateMissingCorrection = "MISSING_CORRECTION"
	crossStateCompositionErased = "COMPOSITION_ERASED"
	crossStateExcludedEntry     = "EXCLUDED_ENTRY"
	crossStateTotalMismatch     = "TOTAL_MISMATCH"
	crossStateDuplicateDomain   = "DUPLICATE_DOMAIN"
	crossStateUnknownDomain     = "UNKNOWN_DOMAIN"
)

// CrossDomainRejectedError names the exact field, derivation state and
// definition version that left a domain vector unexplained.
type CrossDomainRejectedError struct {
	Field   string
	State   string
	Version string
}

func (e *CrossDomainRejectedError) Error() string {
	return fmt.Sprintf("%v: BAL_010_REJECTED field=%s state=%s version=%s",
		ErrCrossDomainRejected, e.Field, e.State, e.Version)
}

func (e *CrossDomainRejectedError) Unwrap() error { return ErrCrossDomainRejected }

func rejectCrossDomain(field, state, version string) error {
	return &CrossDomainRejectedError{Field: field, State: state, Version: version}
}

// DomainVector is one domain's conformance vector: its accumulator
// definition (carrying the domain's statutory/specific dimensions), the
// ledger to explain, the bitemporal instants and opening the shared
// calculation runs under, and the pinned expectation it must reach.
// Composition carries the statutory/domain-specific tags that must survive
// the shared model end to end: every key must be a declared definition
// dimension and every counted entry must carry its exact value.
type DomainVector struct {
	Domain         string
	Definition     AccumulatorDefinition
	Opening        values.Decimal
	Ledger         []BalanceEntry
	EffectiveAsOf  values.Instant
	KnownAt        values.Instant
	Composition    map[string]string
	ExpectedEnding values.Decimal
}

// CrossDomainRequest is the full BAL-010 vector set: one vector per domain.
type CrossDomainRequest struct {
	Vectors []DomainVector
}

// DomainOutcome is one domain's proven result: the shared-model ending, the
// counted entry total, the preserved composition and a binding digest.
type DomainOutcome struct {
	Domain      string
	Ending      values.Decimal
	Counted     int
	Composition map[string]string
	Digest      string
}

// CrossDomainResult is the proven vector set in request order with a binding
// digest over every outcome.
type CrossDomainResult struct {
	Outcomes []DomainOutcome
	Digest   string
}

// ProveCrossDomainConformance proves every domain vector through the shared
// entry/time/correction model or rejects the first unexplained vector with
// BAL_010_REJECTED. It is pure: outcomes are a function of the request alone.
func ProveCrossDomainConformance(req CrossDomainRequest) (CrossDomainResult, error) {
	if len(req.Vectors) == 0 {
		return CrossDomainResult{}, rejectCrossDomain("vectors", crossStateRequestInvalid, "")
	}
	seen := make(map[string]bool, len(req.Vectors))
	outcomes := make([]DomainOutcome, 0, len(req.Vectors))
	for i, v := range req.Vectors {
		if !isKnownDomain(v.Domain) {
			return CrossDomainResult{}, rejectCrossDomain(fmt.Sprintf("vectors[%d].domain", i), crossStateUnknownDomain, v.Definition.Version)
		}
		if seen[v.Domain] {
			return CrossDomainResult{}, rejectCrossDomain(fmt.Sprintf("vectors[%d].domain", i), crossStateDuplicateDomain, v.Definition.Version)
		}
		seen[v.Domain] = true
		out, err := proveDomainVector(i, v)
		if err != nil {
			return CrossDomainResult{}, err
		}
		outcomes = append(outcomes, out)
	}
	w := canonicalbytes.New("balance/cross-domain-conformance", 1)
	w.Count("outcomes", len(outcomes))
	for _, o := range outcomes {
		w.String("outcome", o.Digest)
	}
	digest, err := w.Digest()
	if err != nil {
		return CrossDomainResult{}, rejectCrossDomain("digest", crossStateRequestInvalid, "")
	}
	return CrossDomainResult{Outcomes: outcomes, Digest: digest}, nil
}

func proveDomainVector(i int, v DomainVector) (DomainOutcome, error) {
	def := v.Definition
	if err := def.Validate(); err != nil {
		var detail *DefinitionError
		if errors.As(err, &detail) {
			return DomainOutcome{}, rejectCrossDomain(fmt.Sprintf("vectors[%d].definition.%s", i, detail.Field), crossStateDefinitionInvalid, def.Version)
		}
		return DomainOutcome{}, rejectCrossDomain(fmt.Sprintf("vectors[%d].definition", i), crossStateDefinitionInvalid, def.Version)
	}
	if err := v.Opening.Validate(); err != nil {
		return DomainOutcome{}, rejectCrossDomain(fmt.Sprintf("vectors[%d].opening", i), crossStateRequestInvalid, def.Version)
	}
	if err := v.ExpectedEnding.Validate(); err != nil {
		return DomainOutcome{}, rejectCrossDomain(fmt.Sprintf("vectors[%d].expected_ending", i), crossStateTotalMismatch, def.Version)
	}
	for j, e := range v.Ledger {
		for _, dim := range def.Dimensions {
			if !dim.Required {
				continue
			}
			if strings.TrimSpace(e.Dimensions[dim.Name]) == "" {
				return DomainOutcome{}, rejectCrossDomain(
					fmt.Sprintf("vectors[%d].ledger[%d].dimensions.%s", i, j, dim.Name),
					crossStateMissingDimension, def.Version)
			}
		}
		if err := e.Validate(def); err != nil {
			return DomainOutcome{}, rejectCrossDomain(fmt.Sprintf("vectors[%d].ledger[%d]", i, j), crossStateEntryInvalid, def.Version)
		}
		if strings.EqualFold(e.EntryType, AdjustmentEntryType) && strings.TrimSpace(e.SupersedesDigest) == "" {
			return DomainOutcome{}, rejectCrossDomain(fmt.Sprintf("vectors[%d].ledger[%d].supersedes", i, j), crossStateMissingCorrection, def.Version)
		}
	}
	declared := make(map[string]bool, len(def.Dimensions))
	for _, dim := range def.Dimensions {
		declared[dim.Name] = true
	}
	keys := make([]string, 0, len(v.Composition))
	for key := range v.Composition {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !declared[key] {
			return DomainOutcome{}, rejectCrossDomain(fmt.Sprintf("vectors[%d].composition.%s", i, key), crossStateCompositionErased, def.Version)
		}
		for _, e := range v.Ledger {
			if e.Dimensions[key] != v.Composition[key] {
				return DomainOutcome{}, rejectCrossDomain(fmt.Sprintf("vectors[%d].composition.%s", i, key), crossStateCompositionErased, def.Version)
			}
		}
	}
	bal, err := CalculateAuthorizedBalance(AuthorizedBalanceRequest{
		AccountID:     accountOf(v),
		EffectiveAsOf: v.EffectiveAsOf,
		KnownAt:       v.KnownAt,
		Opening:       v.Opening,
		Scale:         v.Opening.Scale(),
		Rounding:      v.Opening.Rounding(),
	}, v.Ledger)
	if err != nil {
		return DomainOutcome{}, rejectCrossDomain(fmt.Sprintf("vectors[%d].request", i), crossStateRequestInvalid, def.Version)
	}
	if len(bal.Excluded) > 0 {
		return DomainOutcome{}, rejectCrossDomain(fmt.Sprintf("vectors[%d].excluded", i), crossStateExcludedEntry, def.Version)
	}
	if !bal.Ending.Equal(v.ExpectedEnding) {
		return DomainOutcome{}, rejectCrossDomain(fmt.Sprintf("vectors[%d].expected_ending", i), crossStateTotalMismatch, def.Version)
	}
	composition := make(map[string]string, len(v.Composition))
	for _, key := range keys {
		composition[key] = v.Composition[key]
	}
	out := DomainOutcome{
		Domain:      v.Domain,
		Ending:      bal.Ending,
		Counted:     len(bal.Entries) + len(bal.Adjustments),
		Composition: composition,
	}
	digest, err := outcomeDigest(def, bal, out, keys)
	if err != nil {
		return DomainOutcome{}, rejectCrossDomain(fmt.Sprintf("vectors[%d].digest", i), crossStateRequestInvalid, def.Version)
	}
	out.Digest = digest
	return out, nil
}

// accountOf scopes the shared calculation to the vector's account. Vectors
// are single-account by contract; the account is the first ledger entry's.
func accountOf(v DomainVector) string {
	if len(v.Ledger) > 0 {
		return v.Ledger[0].AccountID
	}
	return ""
}

func outcomeDigest(def AccumulatorDefinition, bal AuthorizedBalance, out DomainOutcome, compositionKeys []string) (string, error) {
	w := canonicalbytes.New("balance/cross-domain-outcome", 1).
		String("domain", out.Domain).
		String("definition_id", def.ID).
		String("definition_version", def.Version).
		String("ending", out.Ending.String()).
		Int("counted", int64(out.Counted)).
		String("balance", bal.Digest)
	for _, key := range compositionKeys {
		w.String("composition."+key, out.Composition[key])
	}
	return w.Digest()
}
