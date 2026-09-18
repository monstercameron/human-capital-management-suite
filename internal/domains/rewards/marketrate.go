package rewards

import (
	"context"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/payband"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Intent identity for the market-rate read contract.
const (
	// MarketRateIntentType is the catalog identifier.
	MarketRateIntentType = "hcmnext.rewards.market_rate"
	// MarketRateIntentVersion is the contract version.
	MarketRateIntentVersion = "v1"
)

// Market-rate errors. All are matchable with errors.Is.
var (
	// ErrMarketQueryInvalid is returned for a malformed market query.
	ErrMarketQueryInvalid = errors.New("rewards: market rate query is invalid")
	// ErrMarketRateNotFound is returned by a source when no market anchor
	// covers the query. It is a typed miss, not a zero anchor: an unmatched
	// scope must surface as NEEDS_DATA, never as a market of zero.
	ErrMarketRateNotFound = errors.New("rewards: no market rate covers the requested scope")
	// ErrMarketSourceFailed wraps a transport or storage failure from the port.
	ErrMarketSourceFailed = errors.New("rewards: market rate source failed")
	// ErrMarketSourceUnpinned is returned when a source answers without
	// naming its own version. An unpinned anchor cannot be cited in a
	// proposal digest.
	ErrMarketSourceUnpinned = errors.New("rewards: market rate source answered without a source version")
	// ErrMarketScopeMismatch is returned when the source answers with an
	// anchor scoped to a different job, grade or pay zone than was asked
	// about, or denominated in a different currency.
	ErrMarketScopeMismatch = errors.New("rewards: market source answered outside the requested scope")
	// ErrMarketAnchorInvalid is returned for a malformed market anchor.
	ErrMarketAnchorInvalid = errors.New("rewards: market rate anchor is invalid")
)

// MarketQuery addresses one market anchor. Currency is part of the question,
// not an inference from the worker: a query that does not say which currency
// it means cannot be answered without guessing an FX decision.
type MarketQuery struct {
	Tenant   values.TenantId
	JobCode  string
	Grade    string
	PayZone  string
	Currency string
	// AsOf is the business date the anchor must be effective on.
	AsOf values.LocalDate
}

// Validate reports whether the query is fully specified.
func (q MarketQuery) Validate() error {
	if err := q.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %w", ErrMarketQueryInvalid, err)
	}
	switch {
	case q.JobCode == "":
		return fmt.Errorf("%w: job code is required", ErrMarketQueryInvalid)
	case q.Grade == "":
		return fmt.Errorf("%w: grade is required", ErrMarketQueryInvalid)
	case q.PayZone == "":
		return fmt.Errorf("%w: pay zone is required", ErrMarketQueryInvalid)
	case q.Currency == "":
		return fmt.Errorf("%w: currency is required", ErrMarketQueryInvalid)
	}
	if err := q.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: as-of: %w", ErrMarketQueryInvalid, err)
	}
	return nil
}

// Scope returns the engine-level scope this query addresses.
func (q MarketQuery) Scope() payband.Scope {
	return payband.Scope{JobCode: q.JobCode, Grade: q.Grade, PayZone: q.PayZone}
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (q MarketQuery) Canonical() []byte {
	if q.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.rewards.MarketQuery", rewardsSchemaVer).
		String("tenant", string(q.Tenant)).
		String("job_code", q.JobCode).
		String("grade", q.Grade).
		String("pay_zone", q.PayZone).
		String("currency", q.Currency).
		Value("as_of", q.AsOf).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// MarketAnchor is the market position for one scope: the 25th, 50th and 75th
// percentile base amounts plus the business date they describe. All three
// legs share one currency; a mixed-currency anchor is a refusal, not a
// number, because no leg may be compared against a band or a raise without
// an explicit FX decision.
type MarketAnchor struct {
	P25 values.Money
	P50 values.Money
	P75 values.Money
	// AsOf is the business date the percentiles describe.
	AsOf values.LocalDate
}

// Validate reports whether the anchor is ordered, single-currency and dated.
func (a MarketAnchor) Validate(currency string) error {
	for name, leg := range map[string]values.Money{"p25": a.P25, "p50": a.P50, "p75": a.P75} {
		if err := leg.Validate(); err != nil {
			return fmt.Errorf("%w: %s: %w", ErrMarketAnchorInvalid, name, err)
		}
		if leg.Currency() != currency {
			return fmt.Errorf("%w: %s is %s, query is %s",
				ErrMarketScopeMismatch, name, leg.Currency(), currency)
		}
	}
	lo, err := a.P25.Cmp(a.P50)
	if err != nil {
		return fmt.Errorf("%w: p25/p50: %w", ErrMarketAnchorInvalid, err)
	}
	hi, err := a.P50.Cmp(a.P75)
	if err != nil {
		return fmt.Errorf("%w: p50/p75: %w", ErrMarketAnchorInvalid, err)
	}
	if lo > 0 || hi > 0 {
		return fmt.Errorf("%w: percentiles are unordered (p25=%s p50=%s p75=%s)",
			ErrMarketAnchorInvalid, a.P25, a.P50, a.P75)
	}
	if err := a.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: as-of: %w", ErrMarketAnchorInvalid, err)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
// Currency travels on each leg, so the caller passes the validated query
// currency only as a consistency check, not as encoding input.
func (a MarketAnchor) Canonical(currency string) []byte {
	if a.Validate(currency) != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.rewards.MarketAnchor", rewardsSchemaVer).
		Value("p25", a.P25).
		Value("p50", a.P50).
		Value("p75", a.P75).
		Value("as_of", a.AsOf).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// MarketRecord is one source answer: the anchor plus the governance metadata
// that decides how it is cited later.
type MarketRecord struct {
	Scope  payband.Scope
	Anchor MarketAnchor
	// SourceVersion pins the published source the anchor was read from.
	SourceVersion string
	// Authority is the source-authority decision for the market data.
	Authority evidence.SourceAuthority
	// Provenance is where the anchor came from.
	Provenance evidence.Provenance
}

// Validate reports whether the record is complete and citable.
func (r MarketRecord) Validate(currency string) error {
	if err := r.Anchor.Validate(currency); err != nil {
		return err
	}
	if r.SourceVersion == "" {
		return fmt.Errorf("%w: anchor for %+v", ErrMarketSourceUnpinned, r.Scope)
	}
	if err := r.Authority.Validate(); err != nil {
		return fmt.Errorf("rewards: market anchor for %+v: %w", r.Scope, err)
	}
	return r.Provenance.Validate()
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (r MarketRecord) Canonical(currency string) []byte {
	if r.Validate(currency) != nil {
		return nil
	}
	anchor := r.Anchor.Canonical(currency)
	if anchor == nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.rewards.MarketRecord", rewardsSchemaVer).
		String("job_code", r.Scope.JobCode).
		String("grade", r.Scope.Grade).
		String("pay_zone", r.Scope.PayZone).
		Field("anchor", anchor).
		String("source_version", r.SourceVersion).
		Value("authority", r.Authority).
		Value("provenance", r.Provenance).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// MarketRateSource is the market-data read port the high-performer workflow
// invokes through its fetch_market_rate node. It is the only way the
// compensation path obtains a market anchor.
type MarketRateSource interface {
	// LookupMarketRate returns the anchor covering the query, or
	// ErrMarketRateNotFound.
	LookupMarketRate(ctx context.Context, q MarketQuery) (MarketRecord, error)
}

// MarketRateEvaluation is the governed result of one market-rate read: the
// pure anchor, the query it answers, and every version needed to replay it.
type MarketRateEvaluation struct {
	IntentType    string
	IntentVersion string

	Query  MarketQuery
	Anchor MarketAnchor
	Scope  payband.Scope

	SourceVersion string
	Authority     evidence.SourceAuthority
	Provenance    evidence.Provenance

	InputsDigest string
	ResultDigest string
	Effects      evidence.EffectCounters
}

// canonicalBody encodes everything the result digest covers.
func (e MarketRateEvaluation) canonicalBody() ([]byte, error) {
	return canonicalbytes.New("hcmnext.domains.rewards.MarketRateEvaluation", rewardsSchemaVer).
		String("intent_type", e.IntentType).
		String("intent_version", e.IntentVersion).
		Value("query", e.Query).
		String("job_code", e.Scope.JobCode).
		String("grade", e.Scope.Grade).
		String("pay_zone", e.Scope.PayZone).
		Field("anchor", e.Anchor.Canonical(e.Query.Currency)).
		String("source_version", e.SourceVersion).
		Value("authority", e.Authority).
		Value("provenance", e.Provenance).
		Value("effects", e.Effects).
		Bytes()
}

// Canonical returns the canonical byte encoding, or nil when incoherent.
func (e MarketRateEvaluation) Canonical() []byte {
	body, err := e.canonicalBody()
	if err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.rewards.MarketRateEvaluationEnvelope", rewardsSchemaVer).
		Field("body", body).
		String("inputs_digest", e.InputsDigest).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// LookupMarketRate resolves the market anchor covering the query.
//
// The source answer is checked against the question. A source that returns
// an anchor for a different job, grade or pay zone is a fault rather than a
// finding: silently accepting it would make the raise floor a number about
// some other population.
func LookupMarketRate(ctx context.Context, source MarketRateSource, q MarketQuery) (MarketRateEvaluation, error) {
	if source == nil {
		return MarketRateEvaluation{}, fmt.Errorf("%w: no market rate source", ErrMarketQueryInvalid)
	}
	if err := q.Validate(); err != nil {
		return MarketRateEvaluation{}, err
	}

	inputsDigest, err := canonicalbytes.New("hcmnext.domains.rewards.LookupMarketRateRequest", rewardsSchemaVer).
		String("intent_type", MarketRateIntentType).
		String("intent_version", MarketRateIntentVersion).
		Value("query", q).
		Digest()
	if err != nil {
		return MarketRateEvaluation{}, err
	}

	record, err := source.LookupMarketRate(ctx, q)
	if err != nil {
		if errors.Is(err, ErrMarketRateNotFound) {
			return MarketRateEvaluation{}, err
		}
		return MarketRateEvaluation{}, fmt.Errorf("%w: %w", ErrMarketSourceFailed, err)
	}
	if err := record.Validate(q.Currency); err != nil {
		return MarketRateEvaluation{}, err
	}
	if record.Scope != q.Scope() {
		return MarketRateEvaluation{}, fmt.Errorf("%w: asked %+v, answered %+v",
			ErrMarketScopeMismatch, q.Scope(), record.Scope)
	}

	result := MarketRateEvaluation{
		IntentType:    MarketRateIntentType,
		IntentVersion: MarketRateIntentVersion,
		Query:         q,
		Anchor:        record.Anchor,
		Scope:         record.Scope,
		SourceVersion: record.SourceVersion,
		Authority:     record.Authority,
		Provenance:    record.Provenance,
		InputsDigest:  inputsDigest,
		Effects:       evidence.ZeroEffects(),
	}
	body, err := result.canonicalBody()
	if err != nil {
		return MarketRateEvaluation{}, err
	}
	result.ResultDigest = canonicalbytes.Digest(body)
	return result, nil
}
