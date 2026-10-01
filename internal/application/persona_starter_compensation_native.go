package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/bandfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/personacompfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

var ErrPersonaCompensationUnavailable = errors.New("application: authorized canonical compensation unavailable")

// NativePersonaCompensationConfig is server composition, never skill input.
// WorkerFacts must be the persisted People projection. Annualization declares
// the deployment's factors; its FTE is replaced by the worker's actual fact.
type NativePersonaCompensationConfig struct {
	DB             dbport.Beginner
	TenantUUID     func(values.TenantId) uuid.UUID
	WorkerFacts    people.WorkerFacts
	CatalogVersion string
	BandBlocking   bool
	Annualization  rewards.AnnualizationRule
	Now            func() time.Time
}

type personaNativeCompensationFacts interface {
	CompensationAt(context.Context, values.EntityRef, values.Instant) (personacompfacts.Snapshot, error)
}

type personaNativeCompensationDirectory interface {
	AgentRoleDirectory
	AgentOrganizationDirectory
	AgentSubjectDirectory
	AgentFieldPolicy
}

// NativePersonaCompensationSource obtains salaries and all package components
// from compensation_package/component, and bands from compensation_band.
// Every request checks fresh durable roles, target scope and field policy.
type NativePersonaCompensationSource struct {
	facts         personaNativeCompensationFacts
	workers       people.WorkerFacts
	bands         rewards.PayBandCatalog
	directory     personaNativeCompensationDirectory
	annualization rewards.AnnualizationRule
	now           func() time.Time
}

func NewNativePersonaCompensationSource(config NativePersonaCompensationConfig) (*NativePersonaCompensationSource, error) {
	if config.DB == nil || config.TenantUUID == nil || config.WorkerFacts == nil || config.CatalogVersion == "" || config.Annualization.Validate() != nil {
		return nil, ErrPersonaCompensationUnavailable
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	return &NativePersonaCompensationSource{
		facts: personacompfacts.Reader{DB: config.DB, TenantUUID: config.TenantUUID}, workers: config.WorkerFacts,
		bands:     bandfacts.Catalog{DB: config.DB, TenantUUID: config.TenantUUID, CatalogVersion: config.CatalogVersion, Blocking: config.BandBlocking, MoneyScale: config.Annualization.MoneyScale},
		directory: NewAgentDirectoryDB(config.DB, config.TenantUUID), annualization: config.Annualization, now: config.Now,
	}, nil
}

var _ PersonaCompensationPort = (*NativePersonaCompensationSource)(nil)
var _ PersonaCompaRatioPort = (*NativePersonaCompensationSource)(nil)
var _ PersonaCompensationScenarioPort = (*NativePersonaCompensationSource)(nil)

func (s *NativePersonaCompensationSource) authorize(ctx context.Context, tenant values.TenantId, worker *values.EntityRef) error {
	if s == nil || ctx == nil || s.facts == nil || s.workers == nil || s.bands == nil || s.directory == nil || s.now == nil {
		return ErrPersonaCompensationUnavailable
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.SubjectKind() != trust.SubjectKindHuman || p.Tenant() != tenant || !p.AuthorizesPurpose(authz.PurposeCompensationReview) {
		return ErrPersonaCompensationUnavailable
	}
	now := s.now().UTC()
	if now.Before(p.IssuedAt()) || !now.Before(p.ExpiresAt()) {
		return ErrPersonaCompensationUnavailable
	}
	roles, err := s.directory.CurrentRoles(ctx, tenant, p.Subject())
	if err != nil || len(roles) == 0 {
		return fmt.Errorf("%w: current role authority unavailable", ErrPersonaCompensationUnavailable)
	}
	organizations, err := s.directory.CurrentOrganizationScopes(ctx, tenant, p.Subject(), roles)
	if err != nil || organizations == nil {
		return fmt.Errorf("%w: current organization authority unavailable", ErrPersonaCompensationUnavailable)
	}
	var selected []agentgate.Subject
	if worker != nil {
		if worker.Validate() != nil || worker.Kind != people.KindWorker || worker.Tenant != tenant {
			return ErrPersonaCompensationUnavailable
		}
		subjects, err := s.directory.CurrentSubjects(ctx, tenant, p.Subject(), authz.PurposeCompensationReview, roles, organizations)
		if err != nil {
			return fmt.Errorf("%w: current target authority unavailable", ErrPersonaCompensationUnavailable)
		}
		for _, subject := range subjects {
			if subject.Ref == *worker && subject.Organization.Tenant == tenant && !subject.Organization.IsZero() {
				selected = append(selected, subject)
			}
		}
		if len(selected) != 1 {
			return ErrPersonaCompensationUnavailable
		}
	}
	fields, err := s.directory.CurrentFields(ctx, p, authz.PurposeCompensationReview, roles, organizations, selected)
	if err != nil || !slices.Contains(fields, authz.FieldBaseSalary) {
		return fmt.Errorf("%w: compensation field not disclosable", ErrPersonaCompensationUnavailable)
	}
	return nil
}

func (s *NativePersonaCompensationSource) LookupBand(ctx context.Context, query rewards.BandQuery) (rewards.BandRecord, error) {
	if err := query.Validate(); err != nil {
		return rewards.BandRecord{}, err
	}
	if err := s.authorize(ctx, query.Tenant, nil); err != nil {
		return rewards.BandRecord{}, err
	}
	return s.readBand(ctx, query, values.NewInstant(s.now().UTC()))
}

func (s *NativePersonaCompensationSource) readBand(ctx context.Context, query rewards.BandQuery, knownAt values.Instant) (rewards.BandRecord, error) {
	record, err := s.bands.LookupBand(ctx, query)
	if err != nil {
		return rewards.BandRecord{}, err
	}
	if err := record.Validate(); err != nil {
		return rewards.BandRecord{}, err
	}
	if record.Band.Scope != query.Scope() || record.Band.Currency() != query.Currency || record.Provenance.RecordedAt.Instant().Time().After(knownAt.Time()) {
		return rewards.BandRecord{}, ErrPersonaCompensationUnavailable
	}
	return record, nil
}

type personaCompensationBaseline struct {
	snapshot personacompfacts.Snapshot
	base     personacompfacts.Component
	query    rewards.BandQuery
	rule     rewards.AnnualizationRule
}

func (s *NativePersonaCompensationSource) baseline(ctx context.Context, worker values.EntityRef, asOf values.Instant) (personaCompensationBaseline, error) {
	var result personaCompensationBaseline
	if err := asOf.Validate(); err != nil {
		return result, err
	}
	if err := s.authorize(ctx, worker.Tenant, &worker); err != nil {
		return result, err
	}
	if asOf.Time().After(s.now().UTC()) {
		return result, ErrPersonaCompensationUnavailable
	}
	snapshot, err := s.facts.CompensationAt(ctx, worker, asOf)
	if err != nil {
		return result, err
	}
	if snapshot.Worker != worker || !snapshot.Revision.IsSpecified() {
		return result, ErrPersonaCompensationUnavailable
	}
	baseCount := 0
	for _, component := range snapshot.Components {
		contains, intervalErr := component.Effective.ContainsInstant(asOf)
		if component.ID == "" || component.Amount.Validate() != nil || component.KnownAt.Canonical() == nil || component.Provenance.Validate() != nil || component.Authority.Validate() != nil || values.ValidateKnowledgeOrder(component.KnownAt, component.Provenance.RecordedAt, false) != nil || intervalErr != nil || !contains || component.KnownAt.Instant().Time().After(asOf.Time()) || component.Provenance.RecordedAt.Instant().Time().After(asOf.Time()) {
			return result, ErrPersonaCompensationUnavailable
		}
		if component.Kind == "BASE_PAY" {
			baseCount++
			result.base = component
		}
	}
	if baseCount != 1 {
		return result, ErrPersonaCompensationUnavailable
	}
	date, err := personaCompensationDate(asOf)
	if err != nil {
		return result, err
	}
	knownAt, err := values.NewKnownAt(asOf)
	if err != nil {
		return result, err
	}
	placement, err := s.workers.WorkerFactsAt(ctx, people.FactQuery{Tenant: worker.Tenant, Worker: worker, AsOf: people.AsOf{EffectiveOn: date, KnownAt: knownAt}, Fields: []people.FieldID{people.FieldJobCode, people.FieldGrade, people.FieldPayZone, people.FieldFTE}})
	if err != nil {
		return result, err
	}
	if placement.Worker != worker || !placement.Exists || placement.Validate() != nil {
		return result, ErrPersonaCompensationUnavailable
	}
	read := func(field people.FieldID) (string, error) {
		fact, ok := placement.Lookup(field)
		if !ok {
			return "", ErrPersonaCompensationUnavailable
		}
		contains, err := fact.Effective.ContainsDate(date)
		value, disclosed := fact.Value.Get()
		if err != nil || !contains || !disclosed || value == "" || fact.KnownAt.Instant().Time().After(asOf.Time()) || fact.Provenance.RecordedAt.Instant().Time().After(asOf.Time()) {
			return "", ErrPersonaCompensationUnavailable
		}
		return value, nil
	}
	job, err := read(people.FieldJobCode)
	if err != nil {
		return result, err
	}
	grade, err := read(people.FieldGrade)
	if err != nil {
		return result, err
	}
	zone, err := read(people.FieldPayZone)
	if err != nil {
		return result, err
	}
	fteText, err := read(people.FieldFTE)
	if err != nil {
		return result, err
	}
	result.rule = s.annualization
	result.rule.FTE, err = values.NewDecimal(fteText, 4, values.RoundingExactRequired)
	if err != nil || result.rule.Validate() != nil {
		return result, ErrPersonaCompensationUnavailable
	}
	result.snapshot = snapshot
	result.query = rewards.BandQuery{Tenant: worker.Tenant, JobCode: job, Grade: grade, PayZone: zone, Currency: result.base.Amount.Currency(), AsOf: date}
	return result, nil
}

func personaCompensationDate(instant values.Instant) (values.LocalDate, error) {
	year, month, day := instant.Time().UTC().Date()
	return values.NewLocalDate(year, month, day)
}

func personaCompensationBasis(frequency string) (rewards.PayBasis, error) {
	switch frequency {
	case "ANNUAL":
		return rewards.PayBasisAnnualSalary, nil
	case "MONTHLY":
		return rewards.PayBasisMonthlySalary, nil
	case "HOURLY":
		return rewards.PayBasisHourly, nil
	default:
		return rewards.PayBasisUnspecified, ErrPersonaCompensationUnavailable
	}
}

type personaCompensationPinnedCatalog struct {
	record rewards.BandRecord
	query  rewards.BandQuery
}

func (c personaCompensationPinnedCatalog) LookupBand(_ context.Context, query rewards.BandQuery) (rewards.BandRecord, error) {
	if query != c.query {
		return rewards.BandRecord{}, ErrPersonaCompensationUnavailable
	}
	return c.record, nil
}

func (s *NativePersonaCompensationSource) PayBandPosition(ctx context.Context, worker values.EntityRef, asOf values.Instant) (rewards.PayBandEvaluation, error) {
	baseline, err := s.baseline(ctx, worker, asOf)
	if err != nil {
		return rewards.PayBandEvaluation{}, err
	}
	basis, err := personaCompensationBasis(baseline.base.Frequency)
	if err != nil {
		return rewards.PayBandEvaluation{}, err
	}
	amount, err := baseline.rule.Annualize(baseline.base.Amount, basis)
	if err != nil {
		return rewards.PayBandEvaluation{}, err
	}
	record, err := s.readBand(ctx, baseline.query, asOf)
	if err != nil {
		return rewards.PayBandEvaluation{}, err
	}
	return rewards.EvaluatePayBandPosition(ctx, personaCompensationPinnedCatalog{record, baseline.query}, baseline.query, amount)
}

// SimulateCompensation replaces all current amounts, factors and band inputs
// with authorized native facts. Proposed is a private, zero-effect scenario.
func (s *NativePersonaCompensationSource) SimulateCompensation(ctx context.Context, input rewards.SimulateCompensationInput) (rewards.SimulateCompensationResult, error) {
	if s == nil || s.now == nil || input.Tenant != input.Subject.Tenant || input.EffectiveDate.Validate() != nil || input.FX != nil || input.Market != nil {
		return rewards.SimulateCompensationResult{}, ErrPersonaCompensationUnavailable
	}
	if err := input.Proposed.Validate("proposed"); err != nil {
		return rewards.SimulateCompensationResult{}, err
	}
	asOf := values.NewInstant(s.now().UTC())
	baseline, err := s.baseline(ctx, input.Subject, asOf)
	if err != nil {
		return rewards.SimulateCompensationResult{}, err
	}
	basis, err := personaCompensationBasis(baseline.base.Frequency)
	if err != nil {
		return rewards.SimulateCompensationResult{}, err
	}
	// Rewards subtracts the original basis amounts when the pay basis agrees.
	// Persisted amounts use the column's scale while proposals may declare a
	// different scale. Widen both exactly before comparison; narrowing to the
	// annualization output scale would discard real hourly-pay precision.
	scale := max(baseline.base.Amount.Amount().Scale(), input.Proposed.BaseAmount().Amount().Scale())
	canonicalBase := func(amount values.Money) (values.Money, error) {
		decimal, err := amount.Amount().Quantize(scale, values.RoundingExactRequired)
		if err != nil {
			return values.Money{}, err
		}
		return values.NewMoney(decimal.String(), amount.Currency(), scale, values.RoundingExactRequired)
	}
	currentBase, err := canonicalBase(baseline.base.Amount)
	if err != nil {
		return rewards.SimulateCompensationResult{}, err
	}
	proposedBase, err := canonicalBase(input.Proposed.BaseAmount())
	if err != nil {
		return rewards.SimulateCompensationResult{}, err
	}
	input.Proposed.Base = values.Value(proposedBase)
	current := rewards.CompensationSnapshot{Base: values.Value(currentBase), PayBasis: basis, BonusTargetPercent: values.Absent[values.Percentage](), EffectiveDate: baseline.query.AsOf, Watermark: baseline.snapshot.Revision, Complete: true}
	annualizedBase, err := baseline.rule.Annualize(currentBase, basis)
	if err != nil {
		return rewards.SimulateCompensationResult{}, err
	}
	bonusCount := 0
	for _, component := range baseline.snapshot.Components {
		switch component.Kind {
		case "BASE_PAY":
		case "BONUS_TARGET":
			bonusCount++
			if bonusCount != 1 || component.Amount.Currency() != baseline.base.Amount.Currency() {
				return rewards.SimulateCompensationResult{}, ErrPersonaCompensationUnavailable
			}
			bonusBasis, err := personaCompensationBasis(component.Frequency)
			if err != nil {
				return rewards.SimulateCompensationResult{}, err
			}
			bonus, err := baseline.rule.Annualize(component.Amount, bonusBasis)
			if err != nil {
				return rewards.SimulateCompensationResult{}, err
			}
			fraction, err := bonus.Amount().Div(annualizedBase.Amount(), 12, values.RoundingExactRequired)
			if err != nil {
				return rewards.SimulateCompensationResult{}, err
			}
			percentage, err := values.NewPercentage(fraction.String(), 12, values.RoundingExactRequired)
			if err != nil {
				return rewards.SimulateCompensationResult{}, err
			}
			current.BonusTargetPercent = values.Value(percentage)
		default:
			return rewards.SimulateCompensationResult{}, fmt.Errorf("%w: current package has a component unsupported by the scenario contract", ErrPersonaCompensationUnavailable)
		}
	}
	baseline.query.AsOf = input.EffectiveDate
	record, err := s.readBand(ctx, baseline.query, asOf)
	if err != nil {
		return rewards.SimulateCompensationResult{}, err
	}
	input.Current, input.Annualization, input.Band = current, baseline.rule, &baseline.query
	return rewards.SimulateCompensation(ctx, personaCompensationPinnedCatalog{record, baseline.query}, input)
}
