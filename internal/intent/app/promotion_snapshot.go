package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/budget"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simassign"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simcomp"
	promosnapshot "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/snapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// Governed promotion snapshot reads (REV-006-01).
//
// The production ProposePromotion resolver builds its baseline through
// internal/domains/promotion/snapshot.Build instead of trusting the
// caller-pinned compensation sides the request payload carries. The payload
// still states the intention (who, into what, for how much, when, why); every
// server-owned fact -- current pay, the manager chain, the target position
// revision and the budget pool observation -- is read here, through the same
// corpus and created-population stores the journey baseline already reads.
//
// Three adapters below close the three read ports the corpus does not ship a
// production reader for: the compensation-pool observation, the subject's
// current compensation fact and the manager-chain walk for created workers
// (corpus workers resolve through the fixture org graph). points of
// deliberate parity with journeyBaseline are marked: the compensable
// populations and the single universal pool are the same answers the journey
// serves today, moved server-side rather than re-decided.

// corpusPoolBaselineVersion is the finance baseline the journey payload
// already cites for the corpus pool observation.
const corpusPoolBaselineVersion = "finance.budget.baseline/2026.09"

// corpusPoolOwner is the finance system that owns the corpus pool
// observation, as the journey payload already cites it.
const corpusPoolOwner = "finance.incumbent.erp"

// corpusObservationWatermark is the source watermark of the corpus pool
// observation. The corpus declares one pool without its own clock, so the
// observation carries fixed coordinates known to precede every promotion
// horizon rather than a wall-clock reading that would move the digest.
var corpusObservationWatermark = time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)

// corpusObservationRetrieved is the retrieval coordinate of the corpus pool
// observation, after its watermark as ObservationEvidence requires.
var corpusObservationRetrieved = time.Date(2026, 5, 10, 0, 5, 0, 0, time.UTC)

// corpusBudgetFacts answers the compensation-pool observation from the
// fixture corpus. The corpus declares exactly one pool (the legacy scenario
// set's budget availability), and journeyBaseline serves that same pool for
// every worker and org unit today; this reader keeps that semantic -- one
// universal pool -- while moving the read server-side so the snapshot, not
// the payload, binds the amount a promotion is weighed against. Overrides
// exist so tests can prove exhaustion and absence without editing the corpus.
type corpusBudgetFacts struct {
	amount    values.Decimal
	currency  string
	overrides map[string]budget.BudgetAuthorityRef
	absent    map[string]bool
}

func poolKey(scope, period string) string { return scope + "\x00" + period }

// newCorpusBudgetFacts loads the corpus pool observation.
func newCorpusBudgetFacts() (*corpusBudgetFacts, error) {
	set, err := fixtures.LegacyScenarios()
	if err != nil {
		return nil, fmt.Errorf("app: read the corpus budget pool: %w", err)
	}
	if len(set.Scenarios) == 0 {
		return nil, fmt.Errorf("app: the promotion corpus declares no scenario")
	}
	amount, err := values.NewDecimal(set.BudgetAvailable, 2, values.RoundingHalfEven)
	if err != nil {
		return nil, fmt.Errorf("app: corpus budget pool amount: %w", err)
	}
	return &corpusBudgetFacts{
		amount:    amount,
		currency:  set.Scenarios[0].CurrentCurrency,
		overrides: map[string]budget.BudgetAuthorityRef{},
		absent:    map[string]bool{},
	}, nil
}

// setPoolOverride replaces the pool observation for one scope and period.
// Tests use it to prove exhaustion (a zero-quantity pool) and absence
// without editing the corpus.
func (b *corpusBudgetFacts) setPoolOverride(scope, period string, ref budget.BudgetAuthorityRef, exists bool) {
	if !exists {
		b.absent[poolKey(scope, period)] = true
		return
	}
	b.overrides[poolKey(scope, period)] = ref
}

// CompensationBudgetAt implements snapshot.BudgetFacts.
func (b *corpusBudgetFacts) CompensationBudgetAt(_ context.Context, q promosnapshot.BudgetQuery) (budget.BudgetAuthorityRef, bool, error) {
	if err := q.Validate(); err != nil {
		return budget.BudgetAuthorityRef{}, false, err
	}
	if b.absent[poolKey(q.Scope, q.Period)] {
		return budget.BudgetAuthorityRef{}, false, nil
	}
	if ref, ok := b.overrides[poolKey(q.Scope, q.Period)]; ok {
		return ref, true, nil
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{q.Scope, q.Period, b.amount.String(), b.currency, corpusPoolBaselineVersion}, "\x00")))
	return budget.BudgetAuthorityRef{
		BudgetType:        budget.CompensationPool,
		OwnerSystem:       corpusPoolOwner,
		Scope:             q.Scope,
		Period:            q.Period,
		Currency:          b.currency,
		Unit:              budget.UnitMoney,
		BaselineVersion:   corpusPoolBaselineVersion,
		AvailableQuantity: b.amount,
		Evidence: budget.ObservationEvidence{
			ObservationID:   "obs_budget_" + journeySanitize(q.Scope) + "_" + journeySanitize(q.Period),
			SourceWatermark: values.NewInstant(corpusObservationWatermark),
			RetrievedAt:     values.NewInstant(corpusObservationRetrieved),
			Digest:          "sha256:" + hex.EncodeToString(sum[:]),
		},
	}, true, nil
}

// corpusPayKnownAt is the knowledge coordinate of the corpus compensation
// observation, mirroring the org graph: the scenario set states current pay
// without its own clock, so the observation carries fixed coordinates known
// to precede every promotion horizon.
var corpusPayKnownAt = time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)

// corpusPayRecordedAt is the recording coordinate of the corpus compensation
// observation, after its knowledge coordinate as provenance requires.
var corpusPayRecordedAt = time.Date(2024, 4, 2, 0, 0, 0, 0, time.UTC)

// promotionCompensationFacts answers the subject's current compensation fact
// from the same three populations journeyBaseline serves: the legacy
// scenario worker, jane-doe, and created workers through the cell locator.
// Any other worker is unknown to the compensation record (Exists false),
// which refuses the snapshot rather than letting a payload side stand in.
// The pay band reference is derived from the corpus band catalog over the
// worker's current placement: it names the band the current pay sits in,
// resolved rather than asserted.
type promotionCompensationFacts struct {
	scenarios fixtures.LegacyScenarioSet
	bands     rewards.PayBandCatalog
	locate    WorkerLocator
	profiles  map[string]fixtures.WorkerProfile
}

func newPromotionCompensationFacts(locate WorkerLocator, bands rewards.PayBandCatalog) (*promotionCompensationFacts, error) {
	set, err := fixtures.LegacyScenarios()
	if err != nil {
		return nil, fmt.Errorf("app: read the corpus compensation baseline: %w", err)
	}
	list, err := fixtures.Workers()
	if err != nil {
		return nil, fmt.Errorf("app: read the corpus workers: %w", err)
	}
	profiles := make(map[string]fixtures.WorkerProfile, len(list))
	for _, profile := range list {
		profiles[profile.Key] = profile
	}
	return &promotionCompensationFacts{scenarios: set, bands: bands, locate: locate, profiles: profiles}, nil
}

// compensationPaySource mirrors journeyBaseline: the scenario worker reads
// the ported scenario, jane-doe reads the declared demo constants, and a
// created worker reads its own durable row.
func compensationPaySource(location WorkerLocation, set fixtures.LegacyScenarioSet) (base, currency, bonus string, err error) {
	if location.Created != nil {
		return location.Created.BasePay, location.Created.Currency, location.Created.BonusTarget, nil
	}
	if len(set.Scenarios) == 0 {
		return "", "", "", fmt.Errorf("app: the promotion corpus declares no scenario")
	}
	s := set.Scenarios[0]
	if location.Key == "jane-doe" {
		return fixtures.JanePromotionBase, "USD", fixtures.JanePromotionBonus, nil
	}
	if location.Key != set.Worker {
		return "", "", "", fmt.Errorf("app: worker %q has no declared compensation baseline", location.Key)
	}
	return s.CurrentAmount, s.CurrentCurrency, s.BonusTarget, nil
}

// CompensationFactsAt implements rewards.CompensationFacts.
func (a *promotionCompensationFacts) CompensationFactsAt(ctx context.Context, q rewards.CompensationFactsQuery) (rewards.CompensationFactSet, error) {
	if err := q.Validate(); err != nil {
		return rewards.CompensationFactSet{}, err
	}
	location, found, err := a.locate(ctx, q.Tenant, q.Worker.Id)
	if err != nil {
		return rewards.CompensationFactSet{}, fmt.Errorf("app: locate the compensation subject: %w", err)
	}
	if !found {
		return rewards.CompensationFactSet{Worker: q.Worker, Exists: false}, nil
	}
	base, currency, _, err := compensationPaySource(location, a.scenarios)
	if err != nil {
		return rewards.CompensationFactSet{Worker: q.Worker, Exists: false}, nil
	}
	basePay, err := fixtures.Money(base, currency)
	if err != nil {
		return rewards.CompensationFactSet{}, fmt.Errorf("app: corpus base pay: %w", err)
	}
	profile := a.profiles[location.Key]
	bandRef := ""
	if a.bands != nil && profile.JobCode != "" {
		asOf, dateErr := values.ParseLocalDate(q.AsOf.Time().UTC().Format(time.DateOnly))
		if dateErr == nil {
			band, bandErr := a.bands.LookupBand(ctx, rewards.BandQuery{
				Tenant: q.Tenant, JobCode: profile.JobCode, Grade: profile.Grade,
				PayZone: profile.PayZone, Currency: currency, AsOf: asOf,
			})
			if bandErr == nil {
				bandRef = band.Band.ID + "@" + band.Band.Version
			}
		}
	}
	if bandRef == "" && location.Created != nil {
		row := location.Created
		if a.bands != nil && row.JobCode != "" {
			asOf, dateErr := values.ParseLocalDate(q.AsOf.Time().UTC().Format(time.DateOnly))
			if dateErr == nil {
				band, bandErr := a.bands.LookupBand(ctx, rewards.BandQuery{
					Tenant: q.Tenant, JobCode: row.JobCode, Grade: row.Grade,
					PayZone: row.PayZone, Currency: currency, AsOf: asOf,
				})
				if bandErr == nil {
					bandRef = band.Band.ID + "@" + band.Band.Version
				}
			}
		}
	}
	if bandRef == "" {
		return rewards.CompensationFactSet{}, fmt.Errorf("app: no catalogued band for the subject's current placement")
	}
	knownAt, err := values.NewKnownAt(values.NewInstant(corpusPayKnownAt))
	if err != nil {
		return rewards.CompensationFactSet{}, err
	}
	recordedAt, err := values.NewRecordedAt(values.NewInstant(corpusPayRecordedAt))
	if err != nil {
		return rewards.CompensationFactSet{}, err
	}
	revision, err := values.NewSequenceRevision("rewards.package."+location.Key, 1)
	if err != nil {
		return rewards.CompensationFactSet{}, err
	}
	effective, err := values.NewOpenInstantInterval(q.AsOf)
	if err != nil {
		return rewards.CompensationFactSet{}, err
	}
	policyVersion := "harborcare.policy/2026.1"
	fact := rewards.CompensationFact{
		Worker:     q.Worker,
		BasePay:    basePay,
		PayBandRef: bandRef,
		Currency:   currency,
		PayBasis:   rewards.PayBasisAnnualSalary,
		Frequency:  "MONTHLY",
		Effective:  effective,
		KnownAt:    knownAt,
		Revision:   revision,
		Authority:  evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: "hcmnext.rewards", PolicyRef: policyVersion},
		Provenance: evidence.Provenance{Source: "hcmnext.rewards", EvidenceRef: "evd_comp_" + journeySanitize(location.Key) + "_r1", RecordedAt: recordedAt},
	}
	if location.Created != nil {
		row := location.Created
		knownAt, err := values.NewKnownAt(values.NewInstant(row.KnownAt.UTC()))
		if err != nil {
			return rewards.CompensationFactSet{}, fmt.Errorf("app: created worker known-at: %w", err)
		}
		recordedAt, err := values.NewRecordedAt(values.NewInstant(row.RecordedAt.UTC()))
		if err != nil {
			return rewards.CompensationFactSet{}, fmt.Errorf("app: created worker recorded-at: %w", err)
		}
		revision, err := values.NewSequenceRevision(row.RevisionStream, row.RevisionSequence)
		if err != nil {
			return rewards.CompensationFactSet{}, fmt.Errorf("app: created worker revision: %w", err)
		}
		fact.KnownAt, fact.Revision = knownAt, revision
		if row.PayBasis == "HOURLY_RATE" {
			// An hourly worker's recorded base is a rate; the snapshot
			// annualizes it over the declared hours before any band or
			// salary comparison.
			fact.PayBasis = rewards.PayBasisHourly
		}
		fact.Authority = evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: "hcmnext.workforce", PolicyRef: "hcmnext.workforce/2026.1"}
		fact.Provenance = evidence.Provenance{Source: "hcmnext.workforce", EvidenceRef: "workforce." + row.WorkerID.String(), RecordedAt: recordedAt}
		policyVersion = "hcmnext.workforce/2026.1"
	}
	if err := fact.Validate(); err != nil {
		return rewards.CompensationFactSet{}, err
	}
	return rewards.CompensationFactSet{
		Worker: q.Worker, Exists: true, Fact: fact,
		Watermark: fact.Revision, PolicyVersion: policyVersion,
	}, nil
}

// promotionOrgFacts answers the manager-chain port from the corpus org graph
// for corpus workers and from the created population walk for created
// workers. Corpus and created answers carry their own graph watermark and
// policy version, so a pure chain agrees while a mixed-authority chain
// disagrees and the resolver refuses it rather than merging two graphs.
type promotionOrgFacts struct {
	graph  *fixtures.MemoryOrgFacts
	locate WorkerLocator
}

func newPromotionOrgFacts(locate WorkerLocator) (*promotionOrgFacts, error) {
	graph, err := fixtures.NewMemoryOrgFacts()
	if err != nil {
		return nil, fmt.Errorf("app: read the corpus org graph: %w", err)
	}
	return &promotionOrgFacts{graph: graph, locate: locate}, nil
}

// WorkerFactsAt implements org.WorkerFacts.
func (a *promotionOrgFacts) WorkerFactsAt(ctx context.Context, q org.WorkerFactsQuery) (org.WorkerFactSet, error) {
	if err := q.Validate(); err != nil {
		return org.WorkerFactSet{}, err
	}
	location, found, err := a.locate(ctx, q.Tenant, q.Worker.Id)
	if err != nil {
		return org.WorkerFactSet{}, fmt.Errorf("app: locate the org subject: %w", err)
	}
	if !found || location.Created == nil {
		return a.graph.WorkerFactsAt(ctx, q)
	}
	row := location.Created
	managerRef := strings.TrimSpace(row.ManagerRelationshipRef)
	if managerRef == "" {
		watermark, policy := promotionWalkCoordinates(q.Tenant)
		return org.WorkerFactSet{Worker: q.Worker, Exists: true, Watermark: watermark, PolicyVersion: policy}, nil
	}
	manager, ok, err := a.locate(ctx, q.Tenant, managerRef)
	if err != nil {
		return org.WorkerFactSet{}, fmt.Errorf("app: locate the manager: %w", err)
	}
	if !ok {
		watermark, policy := promotionWalkCoordinates(q.Tenant)
		return org.WorkerFactSet{Worker: q.Worker, Exists: true, Watermark: watermark, PolicyVersion: policy}, nil
	}
	start, err := values.ParseLocalDate(row.EffectiveFrom)
	if err != nil {
		return org.WorkerFactSet{}, fmt.Errorf("app: created worker effective date: %w", err)
	}
	effective, err := values.NewOpenInstantInterval(values.NewInstant(time.Date(int(start.Year()), start.Month(), int(start.Day()), 0, 0, 0, 0, time.UTC)))
	if err != nil {
		return org.WorkerFactSet{}, fmt.Errorf("app: created worker effective interval: %w", err)
	}
	knownAt, err := values.NewKnownAt(values.NewInstant(row.KnownAt.UTC()))
	if err != nil {
		return org.WorkerFactSet{}, fmt.Errorf("app: created worker known-at: %w", err)
	}
	recordedAt, err := values.NewRecordedAt(values.NewInstant(row.RecordedAt.UTC()))
	if err != nil {
		return org.WorkerFactSet{}, fmt.Errorf("app: created worker recorded-at: %w", err)
	}
	revision, err := values.NewSequenceRevision(row.RevisionStream, row.RevisionSequence)
	if err != nil {
		return org.WorkerFactSet{}, fmt.Errorf("app: created worker revision: %w", err)
	}
	assignmentID := strings.TrimSpace(row.AssignmentID)
	if assignmentID == "" {
		assignmentID = "asg_" + row.WorkerID.String()
	}
	fact := org.ManagerRelationshipFact{
		RelationshipID: "rel_" + row.WorkerID.String(),
		Type:           org.RelationshipDirectManager,
		Worker:         q.Worker,
		Manager:        manager.Ref,
		AssignmentID:   assignmentID,
		Effective:      effective,
		KnownAt:        knownAt,
		Revision:       revision,
		Authority:      evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: "hcmnext.workforce", PolicyRef: "hcmnext.workforce/2026.1"},
		Provenance:     evidence.Provenance{Source: "hcmnext.workforce", EvidenceRef: "workforce." + row.WorkerID.String(), RecordedAt: recordedAt},
	}
	if err := fact.Validate(); err != nil {
		return org.WorkerFactSet{}, err
	}
	watermark, policy := promotionWalkCoordinates(q.Tenant)
	return org.WorkerFactSet{
		Worker: q.Worker, Exists: true,
		Relationships: []org.ManagerRelationshipFact{fact},
		Watermark:     watermark, PolicyVersion: policy,
	}, nil
}

// promotionWalkCoordinates stamps one observation over a created-population
// walk so every hop of one walk agrees. The stream names the tenant because
// two tenants' workforces are different graphs.
func promotionWalkCoordinates(tenant values.TenantId) (values.RevisionToken, string) {
	revision, err := values.NewSequenceRevision("org.walk."+string(tenant), 1)
	if err != nil {
		return values.RevisionToken{}, ""
	}
	return revision, "hcmnext.workforce/2026.1"
}

// promotionSnapshotAuthorization projects the evaluated read decision onto
// the snapshot's disclosure set. The gate already refused a principal who
// may not see base pay or bonus target, so these rulings only shape
// partial-disclosure states: base-pay-derived fields follow the base-salary
// ruling, bonus components follow the bonus-target ruling, and the
// structural reporting line discloses for a disclosable subject.
func promotionSnapshotAuthorization(result authorizationResult, tenant values.TenantId, workerFields []people.FieldID) promosnapshot.Authorization {
	base := result.Decision.Fields[authz.FieldBaseSalary]
	bonus := result.Decision.Fields[authz.FieldBonusTarget]
	denyReason := func(ruling authz.FieldRuling, fallback string) string {
		if ruling.Reason != "" {
			return ruling.Reason
		}
		return fallback
	}
	compFields := map[rewards.CompensationField]rewards.CompensationFieldRuling{}
	if base.Effect != authz.EffectAllow {
		reason := denyReason(base, "no_grant_for_base_pay")
		compFields[rewards.FieldBasePay] = rewards.CompensationFieldRuling{Effect: people.EffectDeny, Reason: reason}
		compFields[rewards.FieldCurrency] = rewards.CompensationFieldRuling{Effect: people.EffectDeny, Reason: reason}
		compFields[rewards.FieldPayBasis] = rewards.CompensationFieldRuling{Effect: people.EffectDeny, Reason: reason}
		compFields[rewards.FieldFrequency] = rewards.CompensationFieldRuling{Effect: people.EffectDeny, Reason: reason}
		compFields[rewards.FieldPayBandRef] = rewards.CompensationFieldRuling{Effect: people.EffectDeny, Reason: reason}
	}
	if bonus.Effect != authz.EffectAllow {
		compFields[rewards.FieldComponents] = rewards.CompensationFieldRuling{Effect: people.EffectDeny, Reason: denyReason(bonus, "no_grant_for_bonus_target")}
	}
	bandFields := map[rewards.PositionField]rewards.PositionFieldRuling{}
	if base.Effect != authz.EffectAllow {
		reason := denyReason(base, "no_grant_for_base_pay")
		for _, field := range []rewards.PositionField{
			rewards.PositionFieldBand, rewards.PositionFieldAmount, rewards.PositionFieldClass,
			rewards.PositionFieldBoundary, rewards.PositionFieldCompaRatio, rewards.PositionFieldRangePenetration,
		} {
			bandFields[field] = rewards.PositionFieldRuling{Effect: people.AccessDenied, Reason: reason}
		}
	}
	subjectDenial := result.Decision.SubjectDenialReason
	if subjectDenial == "" {
		subjectDenial = "subject_not_disclosable"
	}
	return promosnapshot.Authorization{
		Worker:     peopleDecision(result, workerFields),
		ManagerHop: promotionManagerHopAuthorizer(result),
		Position:   promotionPositionAuthorizer(tenant, result.Decision.SubjectDisclosable),
		Compensation: rewards.CompensationAuthorization{
			PolicyVersion:       AuthorizationPolicyVersion,
			Purpose:             result.Purpose,
			SubjectDisclosable:  result.Decision.SubjectDisclosable,
			SubjectDenialReason: subjectDenial,
			Scopes:              []string{rewards.CompensationReadScope},
			Fields:              compFields,
		},
		PayBand: rewards.PayBandPositionAuthorization{
			PolicyVersion:       AuthorizationPolicyVersion,
			Purpose:             result.Purpose,
			SubjectDisclosable:  result.Decision.SubjectDisclosable,
			SubjectDenialReason: subjectDenial,
			Scopes:              []string{rewards.PayBandPositionReadScope},
			Fields:              bandFields,
		},
		BudgetDisclosable:  base.Effect == authz.EffectAllow,
		BudgetDenialReason: denyReason(base, "no_grant_for_base_pay"),
	}
}

// promotionManagerHopAuthorizer discloses the structural reporting line for
// a disclosable subject. Pay secrecy lives in the compensation and band
// rulings; the chain itself is what the approval graph and the no-cycle
// check read, and what the review surface already shows.
func promotionManagerHopAuthorizer(result authorizationResult) org.Authorizer {
	allow := result.Decision.SubjectDisclosable
	return func(org.ManagerRelationshipFact) people.AuthorizationDecision {
		ruling := people.FieldRuling{Effect: people.EffectDeny, Reason: "subject_not_disclosable"}
		if allow {
			ruling = people.FieldRuling{Effect: people.EffectAllow}
		}
		return people.AuthorizationDecision{
			PolicyVersion:      AuthorizationPolicyVersion,
			Purpose:            result.Purpose,
			SubjectDisclosable: allow,
			Fields:             map[people.FieldID]people.FieldRuling{people.FieldManagerRelation: ruling},
		}
	}
}

// promotionPositionAuthorizer discloses target positions in the subject's
// own tenant. Cross-tenant position references are never readable, which is
// what keeps one tenant's hiring plan out of another tenant's proposal.
func promotionPositionAuthorizer(tenant values.TenantId, disclosable bool) position.Authorizer {
	return func(rev position.PositionRevision) bool {
		return disclosable && rev.Position.Tenant == tenant
	}
}

// promotionAnnualization is the cell's declared COMP-002 rule set: a 40-hour
// week, five days, 52 weeks and 12 months in the subject's own currency, at
// the corpus money contract. Every factor is stated, matching the snapshot
// harness, so no conventional constant can move a band position silently.
func promotionAnnualization(currency string) (rewards.CompensationAnnualizationRule, error) {
	decimal := func(text string) (values.Decimal, error) {
		return values.NewDecimal(text, 4, values.RoundingHalfEven)
	}
	hours, err := decimal("40.0000")
	if err != nil {
		return rewards.CompensationAnnualizationRule{}, err
	}
	days, err := decimal("5.0000")
	if err != nil {
		return rewards.CompensationAnnualizationRule{}, err
	}
	weeks, err := decimal("52.0000")
	if err != nil {
		return rewards.CompensationAnnualizationRule{}, err
	}
	months, err := decimal("12.0000")
	if err != nil {
		return rewards.CompensationAnnualizationRule{}, err
	}
	rule := rewards.CompensationAnnualizationRule{
		Version: "rewards.annualization/2026.1", HoursPerWeek: hours, DaysPerWeek: days,
		WeeksPerYear: weeks, MonthsPerYear: months, Currency: currency,
		MoneyScale: 2, MoneyRounding: values.RoundingHalfEven,
	}
	if err := rule.Validate(); err != nil {
		return rewards.CompensationAnnualizationRule{}, err
	}
	return rule, nil
}

// promotionSnapshotInput is everything buildPromotionSnapshot needs from
// the resolve path: the governed identity, the stated intention and the
// already-evaluated authorization it reads under.
type promotionSnapshotInput struct {
	Subject   values.EntityRef
	Target    promotionTargetPlacement
	Proposed  rewards.CompensationSnapshot
	Effective values.LocalDate
	AsOf      people.AsOf
	Decision  authorizationResult
	Facts     people.FactSet
	Budget    *promotionBudgetAuthority
}

// promotionTargetPlacement mirrors the target fields the snapshot needs
// without importing the promotion domain's placement type here.
type promotionTargetPlacement struct {
	JobCode    string
	Grade      string
	OrgUnit    string
	PositionID string
	PayZone    string
}

// promotionBudgetAuthority mirrors the budget addressing the payload states.
type promotionBudgetAuthority struct {
	Scope  string
	Period string
}

// promotionSnapshotOutput is the governed material one position-bound
// proposal resolves to: the snapshot every effect derives from, the pool
// observation the compensation simulation reserves against, the direct
// manager the assignment simulation keeps, and the budget addressing and pay
// zone the request resolved to.
type promotionSnapshotOutput struct {
	Snapshot promosnapshot.PromotionInputSnapshot
	Budget   budget.BudgetAuthorityRef
	Manager  values.EntityRef
	Zone     string
	Scope    string
	Period   string
}

// buildPromotionSnapshot assembles the governed Promotion input snapshot for
// one position-bound proposal: readers over the cell's bound stores (with
// corpus fallbacks where the cell binds nothing), the request from governed
// values plus the stated intention, and snapshot.Build. A refused build -- a
// missing pool, an exhausted pool, an unknown position, an unresolvable
// chain -- is returned as the error, before any commit path can run. The
// direct manager is re-resolved under the same authorization so the
// assignment simulation keeps exactly the manager the snapshot discloses.
func (f *CorpusInputs) buildPromotionSnapshot(ctx context.Context, in promotionSnapshotInput) (promotionSnapshotOutput, error) {
	var empty promotionSnapshotOutput
	if f == nil {
		return empty, fmt.Errorf("app: promotion snapshot needs a composed resolver")
	}
	if strings.TrimSpace(in.Target.PositionID) == "" {
		return empty, fmt.Errorf("app: promotion snapshot needs a target position")
	}
	if f.locate == nil {
		return empty, fmt.Errorf("app: promotion snapshot needs a worker locator")
	}
	if f.workers == nil || f.bands == nil {
		return empty, fmt.Errorf("app: promotion snapshot needs worker facts and a band catalog")
	}
	desired, present := in.Proposed.Base.Get()
	if !present {
		return empty, fmt.Errorf("app: promotion snapshot needs a desired base pay")
	}
	targetPosition, err := f.snapshotTargetPosition(in.Subject.Tenant, in.Target.PositionID)
	if err != nil {
		return empty, err
	}
	zone := strings.TrimSpace(in.Target.PayZone)
	if zone == "" {
		zone = snapshotFactValue(in.Facts, people.FieldPayZone)
	}
	scope := ""
	period := ""
	if in.Budget != nil {
		scope, period = strings.TrimSpace(in.Budget.Scope), strings.TrimSpace(in.Budget.Period)
	}
	if scope == "" {
		unit := strings.TrimSpace(in.Target.OrgUnit)
		if unit == "" {
			unit = snapshotFactValue(in.Facts, people.FieldOrgUnit)
		}
		scope = "cost-center:" + unit
	}
	if period == "" {
		period = "FY2026"
	}
	annualization, err := promotionAnnualization(desired.Currency())
	if err != nil {
		return empty, fmt.Errorf("app: promotion annualization: %w", err)
	}
	calendar, err := fixtures.Calendar()
	if err != nil {
		return empty, fmt.Errorf("app: corpus calendar: %w", err)
	}
	orgFacts, err := newPromotionOrgFacts(f.locate)
	if err != nil {
		return empty, err
	}
	compFacts, err := newPromotionCompensationFacts(f.locate, f.bands)
	if err != nil {
		return empty, err
	}
	var compensationReader rewards.CompensationFacts = compFacts
	if f.compensationFacts != nil {
		compensationReader = f.compensationFacts
	}
	positionReader := f.positionReader
	if positionReader == nil {
		catalog, err := fixtures.NewMemoryPositionCatalog()
		if err != nil {
			return empty, fmt.Errorf("app: corpus position catalog: %w", err)
		}
		positionReader = catalog
	}
	pools := f.budgetPools
	if pools == nil {
		pools, err = newCorpusBudgetFacts()
		if err != nil {
			return empty, err
		}
	}
	source := strings.TrimSpace(f.externalSource)
	if source == "" {
		source = "corpus:fixtures"
	}
	authorization := promotionSnapshotAuthorization(in.Decision, in.Subject.Tenant, promosnapshot.WorkerFactFields())
	snap, err := promosnapshot.Build(ctx, promosnapshot.Readers{
		Worker: f.workers, Org: orgFacts, Position: positionReader,
		Compensation: compensationReader, Bands: f.bands, Budget: pools,
	}, promosnapshot.Request{
		Tenant: in.Subject.Tenant, Subject: in.Subject, TargetPosition: targetPosition,
		Target: promosnapshot.Target{
			JobCode: strings.TrimSpace(in.Target.JobCode), Grade: strings.TrimSpace(in.Target.Grade),
			OrgUnit: strings.TrimSpace(in.Target.OrgUnit), PayZone: zone,
		},
		DesiredBasePay:    desired,
		DesiredPayBasis:   in.Proposed.PayBasis,
		EffectiveOn:       in.Effective,
		KnownAt:           in.AsOf.KnownAt,
		Calendar:          calendar,
		ManagerChainDepth: maxManagerChainDepth,
		Annualization:     annualization,
		BudgetScope:       scope,
		BudgetPeriod:      period,
		Authorization:     authorization,
		ReferenceVersion:  fixtures.CorpusVersion,
		SourceConnection:  source,
	})
	if err != nil {
		return empty, fmt.Errorf("app: governed promotion snapshot: %w", err)
	}
	resolution, err := org.ResolveManagerRelationships(ctx, orgFacts, org.ManagerResolutionRequest{
		Tenant: in.Subject.Tenant, Worker: in.Subject,
		AsOf: in.AsOf.KnownAt.Instant(), KnownAt: in.AsOf.KnownAt,
		MaxDepth: maxManagerChainDepth, Authorize: authorization.ManagerHop,
	})
	if err != nil {
		return empty, fmt.Errorf("app: resolve the direct manager: %w", err)
	}
	if resolution.Direct == nil {
		return empty, fmt.Errorf("app: the subject has no direct manager at this coordinate")
	}
	poolRef, exists, err := pools.CompensationBudgetAt(ctx, promosnapshot.BudgetQuery{
		Tenant: in.Subject.Tenant, Scope: scope, Period: period, AsOf: in.AsOf.KnownAt.Instant(),
	})
	if err != nil {
		return empty, fmt.Errorf("app: read the pool observation: %w", err)
	}
	if !exists {
		return empty, fmt.Errorf("app: no compensation pool for scope %q period %q", scope, period)
	}
	return promotionSnapshotOutput{
		Snapshot: snap, Budget: poolRef, Manager: resolution.Direct.Manager.Value,
		Zone: zone, Scope: scope, Period: period,
	}, nil
}

// promotionSimulationReservationWindow bounds the resolve-time simulation
// holds: every simulated reservation expires 72 hours after the knowledge
// cut-off, so a hold that never commits can never leak into a real one.
const promotionSimulationReservationWindow = 72 * time.Hour

// PromotionSimulations are the zero-effect assignment and compensation
// simulations a position-bound proposal resolves to, bound to the governed
// snapshot and the candidate they evaluate. A refused simulation fails the
// resolve: the proposal never reaches preflight, let alone commit, on
// effects the engines would not produce.
type PromotionSimulations struct {
	CandidateRevision string
	CandidateDigest   string
	Assignment        simassign.Result
	Compensation      simcomp.Result
}

// promotionSimInput is what runPromotionSims needs beyond the governed
// snapshot output: the stated target, the bitemporal coordinate, the
// evaluated authorization and the governed worker facts.
type promotionSimInput struct {
	Target   promotionTargetPlacement
	AsOf     people.AsOf
	Decision authorizationResult
	Facts    people.FactSet
}

// runPromotionSims runs simassign and simcomp over the governed snapshot and
// returns their results bound to one candidate digest. The candidate
// revision and digest identify the pre-commit candidate the two simulations
// evaluated -- deterministic over the snapshot digest, the subject, the
// target and the desired pay -- and are replaced by the real revision when
// the proposal commits.
func runPromotionSims(out promotionSnapshotOutput, in promotionSimInput) (*PromotionSimulations, error) {
	peopleDecision := peopleDecision(in.Decision, promosnapshot.WorkerFactFields())
	canonical := peopleDecision.Canonical()
	if len(canonical) == 0 {
		return nil, fmt.Errorf("app: promotion sims need an encodable source-authority decision")
	}
	decisionDigest := sha256.Sum256(canonical)
	authorityDigest := "sha256:" + hex.EncodeToString(decisionDigest[:])
	authorityDecision := hex.EncodeToString(canonical)
	subject := out.Snapshot.Subject.String()
	desired := ""
	if text, ok := out.Snapshot.Disclosed(promosnapshot.InputPayBandPositionDesired); ok {
		desired = text
	}
	candidate := sha256.Sum256([]byte(strings.Join([]string{"promotion-candidate", out.Snapshot.Digest, subject, in.Target.PositionID, desired}, "\x00")))
	candidateHex := hex.EncodeToString(candidate[:])
	candidateDigest := "sha256:" + candidateHex
	candidateRevision := "rev-candidate-" + candidateHex[:12]
	fte, err := values.NewDecimal(snapshotFactValue(in.Facts, people.FieldFTE), 4, values.RoundingHalfEven)
	if err != nil {
		return nil, fmt.Errorf("app: governed occupancy fte: %w", err)
	}
	expiry := values.NewInstant(in.AsOf.KnownAt.Instant().Time().Add(promotionSimulationReservationWindow))
	assign, err := simassign.Simulate(simassign.Request{
		Snapshot: out.Snapshot,
		Target: simassign.Target{
			JobCode: strings.TrimSpace(in.Target.JobCode), Grade: strings.TrimSpace(in.Target.Grade),
			OrgUnit: strings.TrimSpace(in.Target.OrgUnit), PayZone: out.Zone,
		},
		ProposedManager:    out.Manager,
		ChainDepthBound:    maxManagerChainDepth,
		ProposalRevisionID: candidateRevision,
		ProposalDigest:     candidateDigest,
		Occupancy:          simassign.Occupancy{FTE: fte, Heads: 1},
		ReservationExpiry:  expiry,
		AuthorityDigest:    authorityDigest,
		AuthorityDecision:  authorityDecision,
	})
	if err != nil {
		return nil, fmt.Errorf("app: assignment simulation: %w", err)
	}
	if err := assign.Err(); err != nil {
		return nil, fmt.Errorf("app: assignment simulation refused: %w", err)
	}
	period, err := promotionPayPeriod(in.AsOf.EffectiveOn)
	if err != nil {
		return nil, err
	}
	daysPerYear, err := values.NewDecimal("365.0000", 4, values.RoundingHalfEven)
	if err != nil {
		return nil, fmt.Errorf("app: declared days per year: %w", err)
	}
	comp, err := simcomp.Simulate(simcomp.Request{
		Snapshot:           out.Snapshot,
		ProposalRevisionID: candidateRevision,
		ProposalDigest:     candidateDigest,
		MoneyScale:         fixtures.MoneyScale,
		MoneyRounding:      fixtures.MoneyRounding,
		RateScale:          6,
		DaysPerYear:        daysPerYear,
		PayPeriod:          period,
		BudgetID:           "budget.pool." + out.Scope + "." + out.Period,
		AuthorityDigest:    out.Budget.Evidence.Digest,
		ReservationExpiry:  expiry,
		AuthorityDecision:  authorityDecision,
	})
	if err != nil {
		return nil, fmt.Errorf("app: compensation simulation: %w", err)
	}
	if err := comp.Err(); err != nil {
		return nil, fmt.Errorf("app: compensation simulation refused: %w", err)
	}
	return &PromotionSimulations{
		CandidateRevision: candidateRevision, CandidateDigest: candidateDigest,
		Assignment: assign, Compensation: comp,
	}, nil
}

// promotionPayPeriod is the monthly pay period containing the effective
// date, in the corpus calendar. Monthly payroll is the declared convention
// -- the corpus states current pay at monthly frequency -- so the period is
// the calendar month, numbered by its month.
func promotionPayPeriod(effective values.LocalDate) (values.PayPeriod, error) {
	if err := effective.Validate(); err != nil {
		return values.PayPeriod{}, fmt.Errorf("app: pay period effective date: %w", err)
	}
	calendar, err := fixtures.Calendar()
	if err != nil {
		return values.PayPeriod{}, fmt.Errorf("app: corpus calendar: %w", err)
	}
	first, err := values.ParseLocalDate(fmt.Sprintf("%04d-%02d-01", effective.Year(), int(effective.Month())))
	if err != nil {
		return values.PayPeriod{}, fmt.Errorf("app: pay period start: %w", err)
	}
	next, err := values.ParseLocalDate(time.Date(int(first.Year()), first.Month()+1, 1, 0, 0, 0, 0, time.UTC).Format(time.DateOnly))
	if err != nil {
		return values.PayPeriod{}, fmt.Errorf("app: pay period end: %w", err)
	}
	interval, err := values.NewLocalDateInterval(first, next, calendar)
	if err != nil {
		return values.PayPeriod{}, fmt.Errorf("app: pay period interval: %w", err)
	}
	period, err := values.NewPayPeriod(fmt.Sprintf("pay-period-%04d-%02d", effective.Year(), int(effective.Month())), interval, int32(effective.Month()))
	if err != nil {
		return values.PayPeriod{}, fmt.Errorf("app: pay period: %w", err)
	}
	return period, nil
}

// snapshotTargetPosition resolves the proposal's target position to the
// entity reference the snapshot binds: a picker-issued revision reference
// when it decodes and stays in-tenant, else the corpus catalog code, else a
// fail-closed refusal for an unknown position.
func (f *CorpusInputs) snapshotTargetPosition(tenant values.TenantId, positionID string) (values.EntityRef, error) {
	if selected, _, decodeErr := position.RevisionRef(positionID).Decode(); decodeErr == nil {
		if selected.Tenant != tenant {
			return values.EntityRef{}, fmt.Errorf("app: target position %q is outside tenant %s", positionID, tenant)
		}
		return selected, nil
	}
	catalog, err := fixtures.NewMemoryPositionCatalog()
	if err != nil {
		return values.EntityRef{}, fmt.Errorf("app: corpus position catalog: %w", err)
	}
	if ref, ok := catalog.PositionRefForCode(positionID); ok {
		if ref.Tenant != tenant {
			return values.EntityRef{}, fmt.Errorf("app: target position %q is outside tenant %s", positionID, tenant)
		}
		return ref, nil
	}
	return values.EntityRef{}, fmt.Errorf("app: target position %q is not a catalogued position", positionID)
}

// budgetScopeOf reads the payload-stated budget scope, if any. The scope is
// addressing, not an amount: the amount always comes from the pool reader.
func budgetScopeOf(budget *promotion.BudgetAuthorityRef) string {
	if budget == nil {
		return ""
	}
	return budget.Scope
}

// budgetPeriodOf reads the payload-stated budget period, if any.
func budgetPeriodOf(budget *promotion.BudgetAuthorityRef) string {
	if budget == nil {
		return ""
	}
	return budget.Period
}

// snapshotFactValue reads one governed placement value out of the worker
// fact set the resolve path already loaded.
func snapshotFactValue(set people.FactSet, field people.FieldID) string {
	for _, fact := range set.Facts {
		if fact.Field != field {
			continue
		}
		if value, present := fact.Value.Get(); present {
			return value
		}
	}
	return ""
}
