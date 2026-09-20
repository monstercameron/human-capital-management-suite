package rewards

import (
	"context"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fx"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Intent identity for COMP-006.
const (
	// SimulateCompensationIntentType is the catalog identifier.
	SimulateCompensationIntentType = "hcmnext.rewards.simulate_compensation"
	// SimulateCompensationIntentVersion is the contract version.
	SimulateCompensationIntentVersion = "v1"
	// CompensationRulePackVersion versions the delta and annualization rules
	// implemented here. Any change to a formula, a scale or a rounding mode
	// changes this string, because a stored result must remain explainable by
	// the rules that produced it.
	CompensationRulePackVersion = "rewards.compensation.rules/1.0.0"
)

// Simulation errors. All are matchable with errors.Is.
var (
	// ErrSnapshotIncomplete is returned when a compensation snapshot does not
	// declare itself complete or does not pin the revision it was read at. A
	// partial snapshot produces a confident number about facts nobody checked.
	ErrSnapshotIncomplete = errors.New("rewards: compensation snapshot is incomplete or unpinned")
	// ErrMaterialInputUnavailable is returned when a material input is
	// REDACTED, UNKNOWN or UNAVAILABLE. The simulation refuses rather than
	// substituting a value the caller was not allowed to see.
	ErrMaterialInputUnavailable = errors.New("rewards: material compensation input is not a disclosed value")
	// ErrCurrencyMismatch is returned when current and proposed compensation
	// are in different currencies. There is no implicit FX in P1A.
	ErrCurrencyMismatch = errors.New("rewards: current and proposed compensation are in different currencies")
	// ErrAnnualizationInvalid is returned for an unusable annualization rule.
	ErrAnnualizationInvalid = errors.New("rewards: annualization rule is invalid")
	// ErrPayBasisUnspecified is returned when a snapshot declares no pay basis.
	ErrPayBasisUnspecified = errors.New("rewards: pay basis is mandatory for a period amount")
	// ErrSimulationInputInvalid is returned for a malformed request.
	ErrSimulationInputInvalid = errors.New("rewards: compensation simulation input is invalid")
)

// PayBasis is how a base amount is expressed. It is mandatory: the same number
// is a fair offer as an annual salary and a fantasy as an hourly rate.
type PayBasis uint8

// Pay bases supported by P1A.
const (
	// PayBasisUnspecified is the zero value and is never legal.
	PayBasisUnspecified PayBasis = iota
	// PayBasisAnnualSalary is an annual salary amount.
	PayBasisAnnualSalary
	// PayBasisMonthlySalary is a monthly salary amount.
	PayBasisMonthlySalary
	// PayBasisHourly is an hourly rate.
	PayBasisHourly
)

var payBasisWire = map[PayBasis]string{
	PayBasisAnnualSalary:  "ANNUAL_SALARY",
	PayBasisMonthlySalary: "MONTHLY_SALARY",
	PayBasisHourly:        "HOURLY",
}

// String returns the stable wire token, or "PAY_BASIS_UNSPECIFIED".
func (b PayBasis) String() string {
	if s, ok := payBasisWire[b]; ok {
		return s
	}
	return "PAY_BASIS_UNSPECIFIED"
}

// Valid reports whether b is a legal pay basis.
func (b PayBasis) Valid() bool { _, ok := payBasisWire[b]; return ok }

// AnnualizationRule is the pinned formula for turning a period amount into an
// annual one. Every constant it uses is explicit, including the ones everybody
// "knows": 52 weeks and 12 months are configuration, not arithmetic truth.
type AnnualizationRule struct {
	Version              string
	StandardHoursPerWeek values.Decimal
	WeeksPerYear         values.Decimal
	MonthsPerYear        values.Decimal
	FTE                  values.Decimal
	// MoneyScale and MoneyRounding are the declared rounding point for every
	// annualized money result.
	MoneyScale    int32
	MoneyRounding values.RoundingMode
}

// factorScale is the working scale for the multiplicative factor. It is wider
// than the money scale so the single rounding point stays on the money result
// rather than being smeared across the factor.
const factorScale int32 = 8

// DefaultAnnualization is the P1A rule: a 40-hour week, 52 weeks, 12 months,
// full time, money at two decimals rounding half to even.
func DefaultAnnualization() AnnualizationRule {
	return AnnualizationRule{
		Version:              "rewards.annualization/1.0.0",
		StandardHoursPerWeek: values.MustDecimal("40.00", 2, values.RoundingHalfEven),
		WeeksPerYear:         values.MustDecimal("52.00", 2, values.RoundingHalfEven),
		MonthsPerYear:        values.MustDecimal("12.00", 2, values.RoundingHalfEven),
		FTE:                  values.MustDecimal("1.0000", 4, values.RoundingHalfEven),
		MoneyScale:           2,
		MoneyRounding:        values.RoundingHalfEven,
	}
}

// Validate reports whether the rule is usable.
func (a AnnualizationRule) Validate() error {
	if a.Version == "" {
		return fmt.Errorf("%w: version is required", ErrAnnualizationInvalid)
	}
	for name, d := range map[string]values.Decimal{
		"standard_hours_per_week": a.StandardHoursPerWeek,
		"weeks_per_year":          a.WeeksPerYear,
		"months_per_year":         a.MonthsPerYear,
		"fte":                     a.FTE,
	} {
		if err := d.Validate(); err != nil {
			return fmt.Errorf("%w: %s: %w", ErrAnnualizationInvalid, name, err)
		}
		if d.Sign() <= 0 {
			return fmt.Errorf("%w: %s must be positive, is %s", ErrAnnualizationInvalid, name, d)
		}
	}
	if a.MoneyScale < 0 || a.MoneyScale > values.MaxScale {
		return fmt.Errorf("%w: money scale %d", ErrAnnualizationInvalid, a.MoneyScale)
	}
	if !a.MoneyRounding.Valid() || a.MoneyRounding == values.RoundingUnspecified {
		return fmt.Errorf("%w: money rounding is unspecified", ErrAnnualizationInvalid)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (a AnnualizationRule) Canonical() []byte {
	if a.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.rewards.AnnualizationRule", rewardsSchemaVer).
		String("version", a.Version).
		Value("standard_hours_per_week", a.StandardHoursPerWeek).
		Value("weeks_per_year", a.WeeksPerYear).
		Value("months_per_year", a.MonthsPerYear).
		Value("fte", a.FTE).
		Int("money_scale", int64(a.MoneyScale)).
		String("money_rounding", a.MoneyRounding.String()).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// factorFor returns the multiplier that turns a basis amount into an annual
// amount, at factorScale.
func (a AnnualizationRule) factorFor(basis PayBasis) (values.Decimal, error) {
	switch basis {
	case PayBasisAnnualSalary:
		return a.FTE.Quantize(factorScale, a.MoneyRounding)
	case PayBasisMonthlySalary:
		return a.MonthsPerYear.Mul(a.FTE, factorScale, a.MoneyRounding)
	case PayBasisHourly:
		hoursPerYear, err := a.StandardHoursPerWeek.Mul(a.WeeksPerYear, factorScale, a.MoneyRounding)
		if err != nil {
			return values.Decimal{}, err
		}
		return hoursPerYear.Mul(a.FTE, factorScale, a.MoneyRounding)
	default:
		return values.Decimal{}, fmt.Errorf("%w: %s", ErrPayBasisUnspecified, basis)
	}
}

// Annualize converts a basis amount to an annual amount, rounding exactly once
// at the rule's declared money scale.
func (a AnnualizationRule) Annualize(amount values.Money, basis PayBasis) (values.Money, error) {
	if err := a.Validate(); err != nil {
		return values.Money{}, err
	}
	if err := amount.Validate(); err != nil {
		return values.Money{}, fmt.Errorf("rewards: annualize: %w", err)
	}
	factor, err := a.factorFor(basis)
	if err != nil {
		return values.Money{}, err
	}
	return amount.MulDecimal(factor, a.MoneyScale, a.MoneyRounding)
}

// CompensationSnapshot is one side of the simulation: a pinned, effective-dated
// view of a worker's compensation.
//
// The amounts are Presence-wrapped because a caller running under field-level
// authorization may legitimately hold a REDACTED base pay. That must fail the
// simulation loudly rather than be coerced to zero, so the state travels with
// the value all the way into Validate.
type CompensationSnapshot struct {
	Base     values.Presence[values.Money]
	PayBasis PayBasis
	// BonusTargetPercent is the bonus target as a fraction of annualized base.
	// ABSENT and NULL mean "no bonus target" and are recorded as an assumption;
	// REDACTED, UNKNOWN and UNAVAILABLE are refusals.
	BonusTargetPercent values.Presence[values.Percentage]
	EffectiveDate      values.LocalDate
	// Watermark pins the revision the snapshot was read at.
	Watermark values.RevisionToken
	// Complete asserts that the snapshot covers every material component. A
	// snapshot that cannot assert it is refused.
	Complete bool
}

// requireDisclosed rejects the presence states that mean "you cannot have this
// number", as distinct from the states that mean "there is no such number".
func requireDisclosed(state values.PresenceState, field string) error {
	switch state {
	case values.PresenceRedacted, values.PresenceUnknown, values.PresenceUnavailable:
		return fmt.Errorf("%w: %s is %s", ErrMaterialInputUnavailable, field, state)
	default:
		return nil
	}
}

// Validate reports whether the snapshot can support an exact simulation.
func (s CompensationSnapshot) Validate(label string) error {
	if !s.Complete {
		return fmt.Errorf("%w: %s snapshot is not marked complete", ErrSnapshotIncomplete, label)
	}
	if !s.Watermark.IsSpecified() {
		return fmt.Errorf("%w: %s snapshot pins no revision", ErrSnapshotIncomplete, label)
	}
	if err := s.Base.Validate(); err != nil {
		return fmt.Errorf("%w: %s base: %w", ErrSimulationInputInvalid, label, err)
	}
	if err := requireDisclosed(s.Base.State(), label+".base"); err != nil {
		return err
	}
	base, ok := s.Base.Get()
	if !ok {
		return fmt.Errorf("%w: %s base is %s, a simulation needs a base amount",
			ErrSimulationInputInvalid, label, s.Base.State())
	}
	if err := base.Validate(); err != nil {
		return fmt.Errorf("%w: %s base: %w", ErrSimulationInputInvalid, label, err)
	}
	if !s.PayBasis.Valid() {
		return fmt.Errorf("%w: %s", ErrPayBasisUnspecified, label)
	}
	if err := s.BonusTargetPercent.Validate(); err != nil {
		return fmt.Errorf("%w: %s bonus target: %w", ErrSimulationInputInvalid, label, err)
	}
	if err := requireDisclosed(s.BonusTargetPercent.State(), label+".bonus_target_percent"); err != nil {
		return err
	}
	if pct, present := s.BonusTargetPercent.Get(); present {
		if err := pct.Validate(); err != nil {
			return fmt.Errorf("%w: %s bonus target: %w", ErrSimulationInputInvalid, label, err)
		}
	}
	return s.EffectiveDate.Validate()
}

// BaseAmount returns the disclosed base amount. Callers must have validated.
func (s CompensationSnapshot) BaseAmount() values.Money {
	m, _ := s.Base.Get()
	return m
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (s CompensationSnapshot) Canonical() []byte {
	base, err := values.MarshalPresence(s.Base, moneyCodec{})
	if err != nil {
		return nil
	}
	bonus, err := values.MarshalPresence(s.BonusTargetPercent, percentageCodec{})
	if err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.rewards.CompensationSnapshot", rewardsSchemaVer).
		Field("base", base).
		String("pay_basis", s.PayBasis.String()).
		Field("bonus_target_percent", bonus).
		Value("effective_date", s.EffectiveDate).
		Value("watermark", s.Watermark).
		Bool("complete", s.Complete).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// moneyCodec and percentageCodec encode a presence payload through the kernel's
// own canonical encoding rather than a second spelling of the same value.
type moneyCodec struct{}

// EncodeValue implements values.ValueCodec.
func (moneyCodec) EncodeValue(m values.Money) ([]byte, error) {
	raw := m.Canonical()
	if raw == nil {
		return nil, fmt.Errorf("%w: money has no canonical encoding", ErrSimulationInputInvalid)
	}
	return raw, nil
}

// DecodeValue implements values.ValueCodec. Decoding is not part of the P1A
// calculation path, so it is deliberately unsupported rather than approximate.
func (moneyCodec) DecodeValue([]byte) (values.Money, error) {
	return values.Money{}, errors.New("rewards: money presence decoding is not supported")
}

type percentageCodec struct{}

// EncodeValue implements values.ValueCodec.
func (percentageCodec) EncodeValue(p values.Percentage) ([]byte, error) {
	raw := p.Canonical()
	if raw == nil {
		return nil, fmt.Errorf("%w: percentage has no canonical encoding", ErrSimulationInputInvalid)
	}
	return raw, nil
}

// DecodeValue implements values.ValueCodec.
func (percentageCodec) DecodeValue([]byte) (values.Percentage, error) {
	return values.Percentage{}, errors.New("rewards: percentage presence decoding is not supported")
}

// Projection is one side's computed compensation picture.
type Projection struct {
	Base                  values.Money
	PayBasis              PayBasis
	AnnualizedBase        values.Money
	BonusTargetPercent    values.Percentage
	AnnualizedBonusTarget values.Money
	AnnualizedTotalCash   values.Money
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (p Projection) Canonical() []byte {
	raw, err := canonicalbytes.New("hcmnext.domains.rewards.Projection", rewardsSchemaVer).
		Value("base", p.Base).
		String("pay_basis", p.PayBasis.String()).
		Value("annualized_base", p.AnnualizedBase).
		Value("bonus_target_percent", p.BonusTargetPercent).
		Value("annualized_bonus_target", p.AnnualizedBonusTarget).
		Value("annualized_total_cash", p.AnnualizedTotalCash).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Delta is the difference between the two projections.
//
// BaseAmount is Presence-wrapped because subtracting an hourly rate from an
// annual salary is not a number: when the pay basis changes, the raw base
// delta is NOT_APPLICABLE and only the annualized deltas are meaningful.
type Delta struct {
	BaseAmount            values.Presence[values.Money]
	AnnualizedBase        values.Money
	AnnualizedBonusTarget values.Money
	AnnualizedTotalCash   values.Money
	// IncreasePercent is the annualized base increase in percent (not a
	// fraction), at IncreasePercentScale. It is the number the legacy
	// ten-percent review threshold was expressed in.
	IncreasePercent values.Decimal
}

// IncreasePercent rounding contract.
const (
	// IncreasePercentScale is the declared scale of the increase percentage.
	IncreasePercentScale int32 = 4
	// IncreasePercentRounding is its declared tie rule.
	IncreasePercentRounding = values.RoundingHalfEven
)

// Canonical returns the canonical byte encoding, or nil when invalid.
func (d Delta) Canonical() []byte {
	base, err := values.MarshalPresence(d.BaseAmount, moneyCodec{})
	if err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.rewards.Delta", rewardsSchemaVer).
		Field("base_amount", base).
		Value("annualized_base", d.AnnualizedBase).
		Value("annualized_bonus_target", d.AnnualizedBonusTarget).
		Value("annualized_total_cash", d.AnnualizedTotalCash).
		Value("increase_percent", d.IncreasePercent).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Assumption records a value the simulation supplied because the input did not.
// Assumptions are part of the result, not a log line: a reviewer approving a
// number is entitled to see what was assumed to produce it.
type Assumption struct {
	Key    string
	Value  string
	Reason string
}

// SimulateCompensationInput is the whole input to a compensation simulation.
type SimulateCompensationInput struct {
	Tenant  values.TenantId
	Subject values.EntityRef

	Current  CompensationSnapshot
	Proposed CompensationSnapshot

	Annualization AnnualizationRule
	EffectiveDate values.LocalDate

	// Band, when set, is evaluated against the proposed annualized base.
	Band *BandQuery
	FX   *PinnedFXConversion
	// Market, when set, floors the raise at the max of the band floor and
	// the market anchor (HIPERF-003). Nil means no market floor; a nil
	// Market encodes as absent, so pre-market goldens stay byte-identical.
	Market *MarketAnchorInput
}

// PinnedFXConversion contains the immutable conversion inputs selected by the
// owner of the simulation. Possession of these values is not approval or
// authority to publish a rate; ConvertMoney validates them before use.
type PinnedFXConversion struct {
	TargetCurrency string
	AsOf, KnownAt  values.Instant
	Profile        fx.ConversionProfileRevision
	Sources        []fx.RateSourceRevision
	Quotes         []fx.FXQuoteRevision
}

func (p PinnedFXConversion) validate(sample values.Money) error {
	if _, err := values.NewMoneyFromDecimal(sample.Amount(), p.TargetCurrency); err != nil {
		return fmt.Errorf("%w: target currency: %w", ErrSimulationInputInvalid, err)
	}
	if err := p.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: FX as-of: %w", ErrSimulationInputInvalid, err)
	}
	if err := p.KnownAt.Validate(); err != nil {
		return fmt.Errorf("%w: FX known-at: %w", ErrSimulationInputInvalid, err)
	}
	if err := p.Profile.Validate(); err != nil {
		return fmt.Errorf("%w: FX profile: %w", ErrSimulationInputInvalid, err)
	}
	for i, source := range p.Sources {
		if err := source.Validate(); err != nil {
			return fmt.Errorf("%w: FX source %d: %w", ErrSimulationInputInvalid, i, err)
		}
	}
	for i, quote := range p.Quotes {
		if err := quote.Validate(); err != nil {
			return fmt.Errorf("%w: FX quote %d: %w", ErrSimulationInputInvalid, i, err)
		}
	}
	return nil
}

func (p PinnedFXConversion) canonical() ([]byte, error) {
	w := canonicalbytes.New("hcmnext.domains.rewards.PinnedFXConversion", rewardsSchemaVer).
		String("target_currency", p.TargetCurrency).Value("as_of", p.AsOf).Value("known_at", p.KnownAt).
		Field("profile", p.Profile.Canonical()).Count("sources", len(p.Sources))
	for _, s := range p.Sources {
		w.Field("source", s.Canonical())
	}
	w.Count("quotes", len(p.Quotes))
	for _, q := range p.Quotes {
		w.Field("quote", q.Canonical())
	}
	return w.Bytes()
}

func (p PinnedFXConversion) convert(m values.Money) (values.Money, string, error) {
	r, err := fx.ConvertMoney(fx.MoneyConversionRequest{Amount: m, TargetCurrency: p.TargetCurrency, AsOf: p.AsOf, KnownAt: p.KnownAt, Profile: p.Profile, Sources: p.Sources, Quotes: p.Quotes})
	if err != nil {
		return values.Money{}, "", err
	}
	return r.Converted, r.CanonicalDigest, nil
}

// Validate reports whether the input can produce an exact result.
func (in SimulateCompensationInput) Validate() error {
	if err := in.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %w", ErrSimulationInputInvalid, err)
	}
	if err := in.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: subject: %w", ErrSimulationInputInvalid, err)
	}
	if in.Subject.Tenant != in.Tenant {
		return fmt.Errorf("%w: subject %s is outside tenant %s", ErrSimulationInputInvalid, in.Subject, in.Tenant)
	}
	if err := in.Current.Validate("current"); err != nil {
		return err
	}
	if err := in.Proposed.Validate("proposed"); err != nil {
		return err
	}
	if in.FX == nil && in.Current.BaseAmount().Currency() != in.Proposed.BaseAmount().Currency() {
		return fmt.Errorf("%w: current %s, proposed %s", ErrCurrencyMismatch,
			in.Current.BaseAmount().Currency(), in.Proposed.BaseAmount().Currency())
	}
	if in.FX != nil {
		if err := in.FX.validate(in.Current.BaseAmount()); err != nil {
			return err
		}
	}
	if err := in.Annualization.Validate(); err != nil {
		return err
	}
	if err := in.EffectiveDate.Validate(); err != nil {
		return fmt.Errorf("%w: effective date: %w", ErrSimulationInputInvalid, err)
	}
	if in.Band != nil {
		if err := in.Band.Validate(); err != nil {
			return err
		}
		bandCurrency := in.Proposed.BaseAmount().Currency()
		if in.FX != nil {
			bandCurrency = in.FX.TargetCurrency
		}
		if in.Band.Currency != bandCurrency {
			return fmt.Errorf("%w: band query %s, proposed %s", ErrCurrencyMismatch,
				in.Band.Currency, in.Proposed.BaseAmount().Currency())
		}
	}
	if in.Market != nil {
		comparison := in.Proposed.BaseAmount().Currency()
		if in.FX != nil {
			comparison = in.FX.TargetCurrency
		}
		if in.Market.SourceVersion == "" {
			return fmt.Errorf("%w: market anchor names no source version", ErrSimulationInputInvalid)
		}
		if in.Market.Currency == "" || in.Market.Currency != comparison {
			return fmt.Errorf("%w: market anchor %s, comparison %s", ErrCurrencyMismatch,
				in.Market.Currency, comparison)
		}
	}
	return nil
}

// Digest returns the canonical digest of the input. Two identical inputs
// produce the same digest in every process; a difference anywhere - including
// in the annualization constants or the pinned watermarks - produces a
// different one.
func (in SimulateCompensationInput) Digest() (string, error) {
	w := canonicalbytes.New("hcmnext.domains.rewards.SimulateCompensationInput", rewardsSchemaVer).
		String("intent_type", SimulateCompensationIntentType).
		String("intent_version", SimulateCompensationIntentVersion).
		String("tenant", string(in.Tenant)).
		Value("subject", in.Subject).
		Value("current", in.Current).
		Value("proposed", in.Proposed).
		Value("annualization", in.Annualization).
		Value("effective_date", in.EffectiveDate).
		Bool("band?", in.Band != nil)
	if in.Band != nil {
		w.Value("band", *in.Band)
	}
	if in.Market != nil {
		// The digest covers the defaulted anchor: an explicit as-of and a
		// defaulted one mean the same market position. Nothing is written
		// when the market is absent, so pre-market digests are unchanged.
		raw := in.Market.withDefaultAsOf(in.EffectiveDate).Canonical()
		if raw == nil {
			return "", fmt.Errorf("%w: market anchor is invalid", ErrMarketAnchorInvalid)
		}
		w.Bool("market?", true).Field("market", raw)
	}
	if in.FX != nil {
		pinned, err := in.FX.canonical()
		if err != nil {
			return "", fmt.Errorf("%w: pinned FX conversion: %w", ErrSimulationInputInvalid, err)
		}
		w.Bool("fx?", true).Field("fx", pinned)
	}
	return w.Digest()
}

// BandResultState says why a band result is or is not present. UNKNOWN is a
// first-class answer: a simulation that quietly omits the band check reads as
// a simulation that passed it.
type BandResultState uint8

// Band result states.
const (
	// BandResultNotRequested means no band query was supplied.
	BandResultNotRequested BandResultState = iota
	// BandResultEvaluated means a band was found and evaluated.
	BandResultEvaluated
	// BandResultUnknown means a band was requested but could not be resolved.
	BandResultUnknown
)

var bandResultWire = map[BandResultState]string{
	BandResultNotRequested: "NOT_REQUESTED",
	BandResultEvaluated:    "EVALUATED",
	BandResultUnknown:      "UNKNOWN",
}

// String returns the stable wire token.
func (s BandResultState) String() string {
	if v, ok := bandResultWire[s]; ok {
		return v
	}
	return "NOT_REQUESTED"
}

// BandResult is the band section of a simulation result.
type BandResult struct {
	State      BandResultState
	Evaluation PayBandEvaluation
	// Reason explains an UNKNOWN state.
	Reason string
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (b BandResult) Canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.rewards.BandResult", rewardsSchemaVer).
		String("state", b.State.String()).
		String("reason", b.Reason)
	if b.State == BandResultEvaluated {
		w.Value("evaluation", b.Evaluation)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// SimulateCompensationResult is the exact, versioned, zero-effect result of
// COMP-006.
type SimulateCompensationResult struct {
	IntentType    string
	IntentVersion string

	Tenant        values.TenantId
	Subject       values.EntityRef
	EffectiveDate values.LocalDate

	Current  Projection
	Proposed Projection
	Delta    Delta
	Band     BandResult
	// Market is the market-informed raise floor section. Nil when the
	// request carried no anchor; nil encodes as absent.
	Market *MarketResult

	Assumptions []Assumption

	RulePackVersion      string
	AnnualizationVersion string
	CatalogVersion       string

	InputsDigest string
	ResultDigest string
	Effects      evidence.EffectCounters
	Receipt      evidence.ZeroEffectReceipt

	// CurrentSnapshotMark and ProposedSnapshotMark carry the pinned revisions
	// the two snapshots were read at, so a replay can prove it read the same
	// baseline rather than a later one that happens to agree.
	CurrentSnapshotMark  values.RevisionToken
	ProposedSnapshotMark values.RevisionToken
	// FXCurrentConversionDigest and FXProposedConversionDigest are the exact
	// ConvertMoney receipt digests, not hashes of the requested quote set.
	FXCurrentConversionDigest  string
	FXProposedConversionDigest string
}

// canonicalBody encodes everything the result digest covers.
func (r SimulateCompensationResult) canonicalBody() ([]byte, error) {
	w := canonicalbytes.New("hcmnext.domains.rewards.SimulateCompensationResult", rewardsSchemaVer).
		String("intent_type", r.IntentType).
		String("intent_version", r.IntentVersion).
		String("tenant", string(r.Tenant)).
		Value("subject", r.Subject).
		Value("effective_date", r.EffectiveDate).
		Value("current", r.Current).
		Value("proposed", r.Proposed).
		Value("delta", r.Delta).
		Value("band", r.Band)
	if r.Market != nil {
		// Nothing is written when the market is absent, so pre-market
		// result bytes are unchanged.
		raw := r.Market.Canonical()
		if raw == nil {
			return nil, fmt.Errorf("%w: market result is incoherent", ErrMarketAnchorInvalid)
		}
		w.Bool("market?", true).Field("market", raw)
	}
	w.Count("assumptions", len(r.Assumptions))
	for _, a := range r.Assumptions {
		w.String("assumption.key", a.Key).
			String("assumption.value", a.Value).
			String("assumption.reason", a.Reason)
	}
	w.String("rule_pack_version", r.RulePackVersion).
		String("annualization_version", r.AnnualizationVersion).
		String("catalog_version", r.CatalogVersion).
		Value("current_snapshot_mark", r.CurrentSnapshotMark).
		Value("proposed_snapshot_mark", r.ProposedSnapshotMark)
	if r.FXCurrentConversionDigest != "" || r.FXProposedConversionDigest != "" {
		w.String("fx_current_conversion_digest", r.FXCurrentConversionDigest).
			String("fx_proposed_conversion_digest", r.FXProposedConversionDigest)
	}
	w.Value("effects", r.Effects)
	return w.Bytes()
}

// Canonical returns the canonical byte encoding of the whole result, or nil
// when incoherent.
func (r SimulateCompensationResult) Canonical() []byte {
	body, err := r.canonicalBody()
	if err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.rewards.SimulateCompensationEnvelope", rewardsSchemaVer).
		Field("body", body).
		String("inputs_digest", r.InputsDigest).
		Value("receipt", r.Receipt).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// zeroPercentage is the bonus target assumed when a snapshot asserts there is
// none. It is recorded as an assumption rather than applied silently.
func zeroPercentage() values.Percentage {
	p, err := values.NewPercentage("0.0000", 4, values.RoundingHalfEven)
	if err != nil {
		panic("rewards: zero percentage is not representable: " + err.Error())
	}
	return p
}

// project computes one side's picture and returns the assumptions it made.
func project(s CompensationSnapshot, rule AnnualizationRule, label string) (Projection, []Assumption, error) {
	var assumptions []Assumption
	base := s.BaseAmount()
	annualBase, err := rule.Annualize(base, s.PayBasis)
	if err != nil {
		return Projection{}, nil, fmt.Errorf("rewards: %s: %w", label, err)
	}
	pct, present := s.BonusTargetPercent.Get()
	if !present {
		pct = zeroPercentage()
		assumptions = append(assumptions, Assumption{
			Key:    label + ".bonus_target_percent",
			Value:  pct.String(),
			Reason: "snapshot asserts " + s.BonusTargetPercent.State().String() + "; treated as no bonus target",
		})
	}
	bonus, err := pct.ApplyTo(annualBase, rule.MoneyScale, rule.MoneyRounding)
	if err != nil {
		return Projection{}, nil, fmt.Errorf("rewards: %s bonus target: %w", label, err)
	}
	total, err := annualBase.Add(bonus)
	if err != nil {
		return Projection{}, nil, fmt.Errorf("rewards: %s total cash: %w", label, err)
	}
	return Projection{
		Base:                  base,
		PayBasis:              s.PayBasis,
		AnnualizedBase:        annualBase,
		BonusTargetPercent:    pct,
		AnnualizedBonusTarget: bonus,
		AnnualizedTotalCash:   total,
	}, assumptions, nil
}

// hundred is the multiplier that turns a fraction into a percentage.
var hundred = values.MustDecimal("100", 0, values.RoundingHalfEven)

// computeDelta subtracts the two projections.
func computeDelta(current, proposed Projection) (Delta, error) {
	annualBase, err := proposed.AnnualizedBase.Sub(current.AnnualizedBase)
	if err != nil {
		return Delta{}, fmt.Errorf("rewards: annualized base delta: %w", err)
	}
	annualBonus, err := proposed.AnnualizedBonusTarget.Sub(current.AnnualizedBonusTarget)
	if err != nil {
		return Delta{}, fmt.Errorf("rewards: annualized bonus delta: %w", err)
	}
	annualTotal, err := proposed.AnnualizedTotalCash.Sub(current.AnnualizedTotalCash)
	if err != nil {
		return Delta{}, fmt.Errorf("rewards: annualized total cash delta: %w", err)
	}

	baseDelta := values.NotApplicable[values.Money](
		"pay basis changed from " + current.PayBasis.String() + " to " + proposed.PayBasis.String())
	if current.PayBasis == proposed.PayBasis {
		raw, err := proposed.Base.Sub(current.Base)
		if err != nil {
			return Delta{}, fmt.Errorf("rewards: base delta: %w", err)
		}
		baseDelta = values.Value(raw)
	}

	increase, err := percentIncrease(current.AnnualizedBase, annualBase)
	if err != nil {
		return Delta{}, err
	}
	return Delta{
		BaseAmount:            baseDelta,
		AnnualizedBase:        annualBase,
		AnnualizedBonusTarget: annualBonus,
		AnnualizedTotalCash:   annualTotal,
		IncreasePercent:       increase,
	}, nil
}

// percentIncrease returns delta/base*100. A zero current base makes the
// percentage undefined; it returns zero, and the caller records the assumption
// rather than reporting an infinite raise.
func percentIncrease(currentAnnual, delta values.Money) (values.Decimal, error) {
	if currentAnnual.Amount().IsZero() {
		return values.NewDecimal("0", IncreasePercentScale, IncreasePercentRounding)
	}
	fraction, err := delta.Amount().Div(currentAnnual.Amount(), factorScale, IncreasePercentRounding)
	if err != nil {
		return values.Decimal{}, fmt.Errorf("rewards: increase fraction: %w", err)
	}
	return fraction.Mul(hundred, IncreasePercentScale, IncreasePercentRounding)
}

// SimulateCompensation is the COMP-006 entry point: an exact, version-pinned,
// zero-effect compensation simulation.
//
// The catalog argument may be nil when the input requests no band evaluation.
// When a band is requested and cannot be resolved, the result records an
// UNKNOWN band rather than failing: an unmatched band is information the
// reviewer needs, whereas a missing band section would read as a pass.
func SimulateCompensation(ctx context.Context, catalog PayBandCatalog, in SimulateCompensationInput) (SimulateCompensationResult, error) {
	if err := in.Validate(); err != nil {
		return SimulateCompensationResult{}, err
	}
	inputsDigest, err := in.Digest()
	if err != nil {
		return SimulateCompensationResult{}, err
	}

	currentInput, proposedInput := in.Current, in.Proposed
	currentFXDigest, proposedFXDigest := "", ""
	if in.FX != nil {
		if currentInput.BaseAmount().Currency() != in.FX.TargetCurrency {
			currentMoney, currentDigest, conversionErr := in.FX.convert(currentInput.BaseAmount())
			if conversionErr != nil {
				return SimulateCompensationResult{}, fmt.Errorf("rewards: current FX conversion: %w", conversionErr)
			}
			currentFXDigest = currentDigest
			currentInput.Base = values.Value(currentMoney)
		}
		if proposedInput.BaseAmount().Currency() != in.FX.TargetCurrency {
			proposedMoney, proposedDigest, conversionErr := in.FX.convert(proposedInput.BaseAmount())
			if conversionErr != nil {
				return SimulateCompensationResult{}, fmt.Errorf("rewards: proposed FX conversion: %w", conversionErr)
			}
			proposedFXDigest = proposedDigest
			proposedInput.Base = values.Value(proposedMoney)
		}
	}
	current, currentAssumptions, err := project(currentInput, in.Annualization, "current")
	if err != nil {
		return SimulateCompensationResult{}, err
	}
	proposed, proposedAssumptions, err := project(proposedInput, in.Annualization, "proposed")
	if err != nil {
		return SimulateCompensationResult{}, err
	}
	delta, err := computeDelta(current, proposed)
	if err != nil {
		return SimulateCompensationResult{}, err
	}

	assumptions := make([]Assumption, 0, len(currentAssumptions)+len(proposedAssumptions)+2)
	assumptions = append(assumptions, currentAssumptions...)
	assumptions = append(assumptions, proposedAssumptions...)
	if current.AnnualizedBase.Amount().IsZero() {
		assumptions = append(assumptions, Assumption{
			Key:    "delta.increase_percent",
			Value:  "0",
			Reason: "current annualized base is zero; a relative increase is undefined",
		})
	}
	assumptions = append(assumptions, Assumption{
		Key:    "annualization.fte",
		Value:  in.Annualization.FTE.String(),
		Reason: "pinned by annualization rule " + in.Annualization.Version,
	})

	band := BandResult{State: BandResultNotRequested}
	catalogVersion := ""
	if in.Band != nil {
		switch {
		case catalog == nil:
			band = BandResult{State: BandResultUnknown, Reason: "no pay band catalog was wired"}
		default:
			evaluation, evalErr := EvaluatePayBandPosition(ctx, catalog, *in.Band, proposed.AnnualizedBase)
			switch {
			case evalErr == nil:
				band = BandResult{State: BandResultEvaluated, Evaluation: evaluation}
				catalogVersion = evaluation.CatalogVersion
			case errors.Is(evalErr, ErrBandNotFound):
				band = BandResult{State: BandResultUnknown, Reason: ErrBandNotFound.Error()}
			default:
				return SimulateCompensationResult{}, evalErr
			}
		}
	}

	var market *MarketResult
	if in.Market != nil {
		comparison := in.Proposed.BaseAmount().Currency()
		if in.FX != nil {
			comparison = in.FX.TargetCurrency
		}
		anchor, err := in.Market.resolve(in.EffectiveDate, comparison)
		if err != nil {
			return SimulateCompensationResult{}, err
		}
		var bandFloor values.Money
		bandKnown := false
		if band.State == BandResultEvaluated {
			// The evaluation above already resolved this band against the
			// same query; re-reading its minimum is a second governed read
			// of the same answer, never a new decision.
			record, lerr := catalog.LookupBand(ctx, *in.Band)
			if lerr != nil {
				return SimulateCompensationResult{}, fmt.Errorf("%w: market band floor re-read: %w", ErrCatalogFailed, lerr)
			}
			bandFloor, bandKnown = record.Band.Minimum, true
		}
		floor, err := marketFloor(anchor, bandFloor, bandKnown)
		if err != nil {
			return SimulateCompensationResult{}, err
		}
		market = &MarketResult{
			Anchor:         anchor,
			Currency:       comparison,
			SourceVersion:  in.Market.SourceVersion,
			RaiseFloor:     floor,
			BandFloor:      bandFloor,
			BandFloorKnown: bandKnown,
		}
		reason := "market p25 " + anchor.P25.String() + " from " + in.Market.SourceVersion
		if bandKnown {
			reason = "max of band floor " + bandFloor.String() + " and " + reason
		}
		assumptions = append(assumptions, Assumption{
			Key:    "market.raise_floor",
			Value:  floor.String(),
			Reason: reason,
		})
	}

	result := SimulateCompensationResult{
		IntentType:                 SimulateCompensationIntentType,
		IntentVersion:              SimulateCompensationIntentVersion,
		Tenant:                     in.Tenant,
		Subject:                    in.Subject,
		EffectiveDate:              in.EffectiveDate,
		Current:                    current,
		Proposed:                   proposed,
		Delta:                      delta,
		Band:                       band,
		Market:                     market,
		Assumptions:                assumptions,
		RulePackVersion:            CompensationRulePackVersion,
		AnnualizationVersion:       in.Annualization.Version,
		CatalogVersion:             catalogVersion,
		InputsDigest:               inputsDigest,
		Effects:                    evidence.ZeroEffects(),
		CurrentSnapshotMark:        in.Current.Watermark,
		ProposedSnapshotMark:       in.Proposed.Watermark,
		FXCurrentConversionDigest:  currentFXDigest,
		FXProposedConversionDigest: proposedFXDigest,
	}
	body, err := result.canonicalBody()
	if err != nil {
		return SimulateCompensationResult{}, err
	}
	result.ResultDigest = canonicalbytes.Digest(body)

	controls := []evidence.ControlVersion{
		{Name: "compensation_rule_pack", Version: result.RulePackVersion},
		{Name: "annualization_rule", Version: result.AnnualizationVersion},
	}
	if catalogVersion != "" {
		controls = append(controls, evidence.ControlVersion{Name: "pay_band_catalog", Version: catalogVersion})
		controls = append(controls, evidence.ControlVersion{Name: "pay_band_rule_pack", Version: BandRulePackVersion})
	}
	if market != nil {
		controls = append(controls, evidence.ControlVersion{Name: "market_rate_source", Version: market.SourceVersion})
	}
	receipt, err := evidence.NewZeroEffectReceipt(
		result.IntentType, result.IntentVersion,
		evidence.ModeSimulate, evidence.RequestStateSimulated,
		controls, result.InputsDigest, result.ResultDigest, result.Effects,
	)
	if err != nil {
		return SimulateCompensationResult{}, err
	}
	result.Receipt = receipt
	return result, nil
}
