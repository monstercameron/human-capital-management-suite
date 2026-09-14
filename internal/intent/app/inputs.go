package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/repair"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// Control identities the diagnostic intents pin.
//
// They are named constants rather than literals scattered through the
// resolvers because a stored finding cites them: a comparison that cannot say
// which freshness policy, approval policy and authority matrix it was judged
// under is not reproducible, and a reproducible diagnostic is the product.
const (
	// FreshnessPolicyVersion versions the observation-age policy every
	// comparison is judged under.
	FreshnessPolicyVersion = "observation.freshness.p1a/1.0.0"
	// FreshnessMaxAgeSeconds is how old an observation may be and still
	// support a decided mismatch. Thirty days is the pilot's budget for a
	// nightly incumbent read; a page older than that is reported STALE rather
	// than compared.
	FreshnessMaxAgeSeconds int64 = 30 * 24 * 60 * 60
	// FieldAuthorityPolicyVersion pins the authority-by-field decision a repair
	// plan's safety classification rests on.
	FieldAuthorityPolicyVersion = "people.source_authority/2026.1"
	// ApprovalPolicyVersion pins the approval policy a repair plan declares.
	ApprovalPolicyVersion = "repair.approval.p1a/1.0.0"
	// LocalSystem names this platform as a repair step's local target. The
	// external target is the connector's own source reference, so it is read
	// from the connection rather than restated here.
	LocalSystem = "hcmnext"
	// ObservationPageLimit bounds one observation page a comparison reads.
	ObservationPageLimit = 100
)

// FixtureInputs resolves domain inputs against internal/domains/fixtures and,
// for the cross-system diagnostics, against the configured incumbent
// connector.
//
// P1A ships no worker projection and no pay-band store: the eight read-only
// intents answer from the design-partner corpus. That is a deployment fact,
// not a test convenience, so the adapter lives beside the service and is what
// cmd/hcmnext wires in. Replacing it with a real projection is a change to
// this file and to the composition root, and to nothing else.
type FixtureInputs struct {
	// workers is the governed worker read this resolver pins its baseline
	// through. It starts as the corpus reader and is rebound by [NewCell] to
	// the cell's own composed WorkerFacts ([BindWorkers]), so the resolver
	// and the capability handlers read the same population through the same
	// port -- including the durable workers a user created, which the corpus
	// reader alone does not know about.
	workers people.WorkerFacts
	bands   rewards.PayBandCatalog
	// locate resolves a request's worker reference. It starts as the
	// corpus-only locator and is rebound by [NewCell] to the cell's own
	// ([BindWorkerLocator]), so this resolver and every other surface that
	// accepts a worker reference agree about what one names.
	locate WorkerLocator
	// The simulation and workspace preview must validate selected positions
	// against the same composed, tenant-scoped Position facts reader.
	positionReader position.PositionFacts
	// externalSource is the observing system the comparison intents read
	// their external side from. It is empty until the cell binds a connector,
	// and a diagnostic resolved without one refuses rather than comparing
	// against nothing.
	externalSource string
}

var _ DomainInputs = (*FixtureInputs)(nil)

// NewFixtureInputs loads the corpus.
func NewFixtureInputs() (*FixtureInputs, error) {
	workers, err := fixtures.NewMemoryWorkerFacts()
	if err != nil {
		return nil, fmt.Errorf("app: load worker corpus: %w", err)
	}
	bands, err := fixtures.NewMemoryBandCatalog()
	if err != nil {
		return nil, fmt.Errorf("app: load pay band corpus: %w", err)
	}
	demoBands, err := newDemoBandCatalog(bands)
	if err != nil {
		return nil, fmt.Errorf("app: load demo pay bands: %w", err)
	}
	return &FixtureInputs{workers: workers, bands: demoBands, locate: corpusWorkerLocator}, nil
}

// BindExternalSource names the observing system the comparison intents read.
func (f *FixtureInputs) BindExternalSource(ref string) { f.externalSource = ref }

// BindWorkers replaces the governed worker read this resolver pins its
// baseline through.
//
// [NewCell] calls it with the cell's own composed WorkerFacts, which is the
// corpus reader layered with the durable created population. Without it the
// resolver would keep its own private corpus reader and a created worker
// would be visible to the capability handlers but invisible to the very
// simulation that has to certify a promotion for them -- two read paths, and
// the one that matters answering "no such worker".
//
// A nil reader is ignored: a cell that composed no worker read at all keeps
// the corpus this resolver loaded for itself.
func (f *FixtureInputs) BindWorkers(workers people.WorkerFacts) {
	if workers != nil {
		f.workers = workers
	}
}

// BindWorkerLocator replaces the worker-reference resolver this resolver uses.
//
// [NewCell] calls it with the cell's own locator, which knows the tenant's
// created population as well as the corpus. Without it a promotion proposed
// for a created worker would carry a reference this resolver refuses, and the
// simulation that has to certify the promotion would never run.
//
// A nil locator is ignored, so a caller that never composed one keeps the
// corpus resolution this type loaded for itself.
func (f *FixtureInputs) BindWorkerLocator(locate WorkerLocator) {
	if locate != nil {
		f.locate = locate
	}
}

// BindPositionReader gives promotion simulation the position reader already
// used by the workspace preview. A nil reader keeps named positions blocked.
func (f *FixtureInputs) BindPositionReader(reader position.PositionFacts) {
	f.positionReader = reader
}

// Bands exposes the pay-band catalog the domain handlers evaluate against.
func (f *FixtureInputs) Bands() rewards.PayBandCatalog { return f.bands }

// Workers exposes the governed worker read port.
func (f *FixtureInputs) Workers() people.WorkerFacts { return f.workers }

// Resolve implements [DomainInputs].
func (f *FixtureInputs) Resolve(ctx context.Context, req ResolveRequest) (DomainCall, error) {
	inst, def := req.Instance, req.Definition
	payload, err := decodeStruct(inst.Request.WireBytes)
	if err != nil {
		return DomainCall{}, err
	}

	switch def.Ref.TypeID {
	case promotion.IntentType:
		return f.resolvePromotion(ctx, req, payload)
	case people.ExplainWorkerStateIntentType:
		return f.resolveExplain(ctx, req, payload)
	case rewards.SimulateCompensationIntentType:
		return f.resolveCompensation(ctx, req, payload)
	case rewards.EvaluatePayBandIntentType:
		return f.resolvePayBand(ctx, req, payload)
	case dataops.DetectDriftIntentType:
		return f.resolveDrift(ctx, req, payload)
	case repair.CreateRepairPlanIntentType, repair.SimulateRepairIntentType:
		return f.resolveRepair(ctx, req, payload)
	case intelligence.ExplainTransactionIntentType:
		return f.resolveTransaction(req, payload)
	default:
		return DomainCall{}, fmt.Errorf("app: %s has no P1A domain binding in this cell", def.Ref)
	}
}

// worker resolves a request's worker reference through the cell's own
// locator, accepting a corpus key, a created worker's key, or an entity id.
//
// A lookup failure is reported as "does not resolve" rather than propagated:
// this returns the same two-valued answer it always did, and every call site
// turns a false into a refusal naming the reference. The locator itself logs
// nothing and invents nothing, so the only lost information is the difference
// between "not there" and "could not tell", which no caller here distinguishes.
func (f *FixtureInputs) worker(ctx context.Context, tenant values.TenantId, ref string) (values.EntityRef, bool) {
	locate := f.locate
	if locate == nil {
		locate = corpusWorkerLocator
	}
	location, ok, err := locate(ctx, tenant, ref)
	if err != nil || !ok {
		return values.EntityRef{}, false
	}
	return location.Ref, true
}

// read performs the corpus read that pins the baseline: whether the worker
// resolves at all, and the revision every fact was read at.
func (f *FixtureInputs) read(ctx context.Context, tenant values.TenantId, worker values.EntityRef, asOf people.AsOf) (people.FactSet, error) {
	return f.workers.WorkerFactsAt(ctx, people.FactQuery{
		Tenant: tenant,
		Worker: worker,
		AsOf:   asOf,
		Fields: promotion.RequiredWorkerFields(),
	})
}

// asOf builds the bitemporal coordinate from an effective date and an optional
// knowledge cut-off, defaulting the cut-off to the instance's creation time.
func asOfFrom(inst intent.Instance, effective values.LocalDate, knownAtText string) (people.AsOf, error) {
	at := inst.CreatedAt
	if knownAtText != "" {
		parsed, err := values.ParseLocalDate(knownAtText)
		if err != nil {
			return people.AsOf{}, fmt.Errorf("app: known_at: %w", err)
		}
		at = values.NewInstant(time.Date(int(parsed.Year()), parsed.Month(), int(parsed.Day()), 0, 0, 0, 0, time.UTC))
	}
	known, err := values.NewKnownAt(at)
	if err != nil {
		return people.AsOf{}, fmt.Errorf("app: known_at: %w", err)
	}
	return people.AsOf{EffectiveOn: effective, KnownAt: known}, nil
}

// resolvePromotion decodes a promote_worker payload into the governed read, the
// domain preflight request and the kernel baseline.
func (f *FixtureInputs) resolvePromotion(ctx context.Context, req ResolveRequest, payload *structValue) (DomainCall, error) {
	inst := req.Instance
	workerRef, err := str(payload, "worker_ref")
	if err != nil {
		return DomainCall{}, err
	}
	subject, ok := f.worker(ctx, inst.Tenant, workerRef)
	if !ok {
		return DomainCall{}, fmt.Errorf("app: worker_ref %q is not a resolvable worker reference", workerRef)
	}
	target, err := targetPlacement(payload)
	if err != nil {
		return DomainCall{}, err
	}
	effective, err := localDate(payload, "effective_date")
	if err != nil {
		return DomainCall{}, err
	}
	evaluation, err := localDate(payload, "evaluation_date")
	if err != nil {
		return DomainCall{}, err
	}
	current, err := compensationSnapshot(payload, "current")
	if err != nil {
		return DomainCall{}, err
	}
	proposed, err := compensationSnapshot(payload, "proposed")
	if err != nil {
		return DomainCall{}, err
	}
	budget, err := budgetObservation(payload)
	if err != nil {
		return DomainCall{}, err
	}
	reason, err := str(payload, "business_reason")
	if err != nil {
		return DomainCall{}, err
	}
	asOf, err := asOfFrom(inst, effective, optionalStr(payload, "known_at"))
	if err != nil {
		return DomainCall{}, err
	}

	fieldSet := promotion.RequiredWorkerFields()
	// A promotion reads worker state and moves pay, so the decision covers
	// both: the worker-state projection it discloses, and the compensation
	// fields it cannot be answered without. A caller authorized to see the
	// assignment but not the pay is refused here rather than handed a
	// simulation with the money silently missing.
	decision, err := authorizeRead(req.Principal, req.Purpose, authorizationRequest{
		Subject:       subject,
		EvaluatedAt:   inst.CreatedAt,
		Gate:          []authz.FieldID{authz.FieldBaseSalary, authz.FieldBonusTarget},
		Read:          peopleFields(fieldSet),
		Relationships: req.Relationships,
	})
	if err != nil {
		return DomainCall{}, err
	}
	explain := people.ExplainWorkerStateRequest{
		Tenant:        inst.Tenant,
		Worker:        subject,
		AsOf:          asOf,
		Fields:        fieldSet,
		Authorization: peopleDecision(decision, fieldSet),
	}

	facts, err := f.read(ctx, inst.Tenant, subject, asOf)
	if err != nil {
		return DomainCall{}, fmt.Errorf("app: governed worker read: %w", err)
	}

	present := []string{"employment_ref", "effective_time", "reason_ref"}
	if target.PositionID != "" {
		present = append(present, "target_position_ref")
	}
	if proposed.Base.IsValue() {
		present = append(present, "proposed_base_pay")
	}
	baseline := baselineFor(inst, facts, subject, target, present)
	if target.PositionID != "" && f.positionReader != nil {
		selected, _, decodeErr := position.RevisionRef(target.PositionID).Decode()
		if decodeErr == nil && selected.Tenant == inst.Tenant {
			knownAt, knownErr := values.NewKnownAt(values.NewInstant(time.Date(int(evaluation.Year()), evaluation.Month(), int(evaluation.Day()), 0, 0, 0, 0, time.UTC)))
			if knownErr != nil {
				return DomainCall{}, fmt.Errorf("app: target position known-at: %w", knownErr)
			}
			revision, exists, readErr := f.positionReader.PositionRevisionAt(ctx, position.PositionQuery{
				Tenant: inst.Tenant, Position: selected, AsOf: position.AsOf{EffectiveOn: effective, KnownAt: knownAt},
			})
			if readErr != nil {
				return DomainCall{}, fmt.Errorf("app: governed target position read: %w", readErr)
			}
			if exists {
				for _, declared := range inst.Subjects {
					if declared.Kind == "POSITION" && declared.SubjectID == selected.Id {
						baseline.KnownSubjects = append(baseline.KnownSubjects, declared)
						baseline.Revisions[selected.String()] = revision.Revision
					}
				}
			}
		}
	}

	return DomainCall{
		Explain: &explain,
		Promotion: &promotion.PreflightRequest{
			Tenant:         inst.Tenant,
			Subject:        subject,
			Target:         target,
			Current:        current,
			Proposed:       proposed,
			EffectiveDate:  effective,
			EvaluationDate: evaluation,
			BusinessReason: reason,
			Budget:         budget,
			Policy:         promotion.DefaultPolicy(),
			Annualization:  rewards.DefaultAnnualization(),
			PositionReader: f.positionReader,
		},
		Baseline: baseline,
	}, nil
}

// resolveExplain decodes an explain_worker_state payload.
func (f *FixtureInputs) resolveExplain(ctx context.Context, req ResolveRequest, payload *structValue) (DomainCall, error) {
	inst := req.Instance
	workerRef, err := str(payload, "worker_ref")
	if err != nil {
		return DomainCall{}, err
	}
	subject, ok := f.worker(ctx, inst.Tenant, workerRef)
	if !ok {
		return DomainCall{}, fmt.Errorf("app: worker_ref %q is not a resolvable worker reference", workerRef)
	}
	effective, err := localDate(payload, "effective_on")
	if err != nil {
		return DomainCall{}, err
	}
	asOf, err := asOfFrom(inst, effective, optionalStr(payload, "known_at"))
	if err != nil {
		return DomainCall{}, err
	}
	fieldSet := people.AllFields()
	decision, err := authorizeRead(req.Principal, req.Purpose, authorizationRequest{
		Subject:       subject,
		EvaluatedAt:   inst.CreatedAt,
		Read:          peopleFields(fieldSet),
		Relationships: req.Relationships,
	})
	if err != nil {
		return DomainCall{}, err
	}
	facts, err := f.read(ctx, inst.Tenant, subject, asOf)
	if err != nil {
		return DomainCall{}, fmt.Errorf("app: governed worker read: %w", err)
	}
	return DomainCall{
		Explain: &people.ExplainWorkerStateRequest{
			Tenant:        inst.Tenant,
			Worker:        subject,
			AsOf:          asOf,
			Fields:        fieldSet,
			Authorization: peopleDecision(decision, fieldSet),
		},
		Baseline: baselineFor(inst, facts, subject, promotion.TargetPlacement{},
			[]string{"worker_ref", "as_of"}),
	}, nil
}

// resolveCompensation decodes a simulate_compensation payload.
func (f *FixtureInputs) resolveCompensation(ctx context.Context, req ResolveRequest, payload *structValue) (DomainCall, error) {
	inst := req.Instance
	workerRef, err := str(payload, "worker_ref")
	if err != nil {
		return DomainCall{}, err
	}
	subject, ok := f.worker(ctx, inst.Tenant, workerRef)
	if !ok {
		return DomainCall{}, fmt.Errorf("app: worker_ref %q is not a resolvable worker reference", workerRef)
	}
	effective, err := localDate(payload, "effective_date")
	if err != nil {
		return DomainCall{}, err
	}
	// A compensation simulation is entirely about pay, so every field it
	// reads gates it: there is no partial answer worth returning when the
	// caller may not see compensation under this purpose.
	if _, authErr := authorizeRead(req.Principal, req.Purpose, authorizationRequest{
		Subject:       subject,
		EvaluatedAt:   inst.CreatedAt,
		Gate:          []authz.FieldID{authz.FieldBaseSalary, authz.FieldBonusTarget},
		Relationships: req.Relationships,
	}); authErr != nil {
		return DomainCall{}, authErr
	}
	current, err := compensationSnapshot(payload, "current")
	if err != nil {
		return DomainCall{}, err
	}
	proposed, err := compensationSnapshot(payload, "proposed")
	if err != nil {
		return DomainCall{}, err
	}
	in := rewards.SimulateCompensationInput{
		Tenant:        inst.Tenant,
		Subject:       subject,
		Current:       current,
		Proposed:      proposed,
		Annualization: rewards.DefaultAnnualization(),
		EffectiveDate: effective,
	}
	if band, bandErr := fieldsOf(payload, "band"); bandErr == nil {
		q, qErr := bandQuery(band, inst.Tenant, effective)
		if qErr != nil {
			return DomainCall{}, qErr
		}
		in.Band = &q
	}
	asOf, err := asOfFrom(inst, effective, optionalStr(payload, "known_at"))
	if err != nil {
		return DomainCall{}, err
	}
	facts, err := f.read(ctx, inst.Tenant, subject, asOf)
	if err != nil {
		return DomainCall{}, fmt.Errorf("app: governed worker read: %w", err)
	}
	return DomainCall{
		Compensation: &in,
		Baseline: baselineFor(inst, facts, subject, promotion.TargetPlacement{},
			[]string{"employment_ref", "proposed_amount", "effective_time"}),
	}, nil
}

// resolvePayBand decodes an evaluate_pay_band_position payload.
func (f *FixtureInputs) resolvePayBand(ctx context.Context, req ResolveRequest, payload *structValue) (DomainCall, error) {
	inst := req.Instance
	workerRef, err := str(payload, "worker_ref")
	if err != nil {
		return DomainCall{}, err
	}
	subject, ok := f.worker(ctx, inst.Tenant, workerRef)
	if !ok {
		return DomainCall{}, fmt.Errorf("app: worker_ref %q is not a resolvable worker reference", workerRef)
	}
	asOfDate, err := localDate(payload, "as_of")
	if err != nil {
		return DomainCall{}, err
	}
	// Where a worker's pay sits in a band is a compensation disclosure, and
	// it is gated as one.
	if _, authErr := authorizeRead(req.Principal, req.Purpose, authorizationRequest{
		Subject:       subject,
		EvaluatedAt:   inst.CreatedAt,
		Gate:          []authz.FieldID{authz.FieldBaseSalary},
		Relationships: req.Relationships,
	}); authErr != nil {
		return DomainCall{}, authErr
	}
	q, err := bandQuery(payload, inst.Tenant, asOfDate)
	if err != nil {
		return DomainCall{}, err
	}
	amountText, err := str(payload, "amount")
	if err != nil {
		return DomainCall{}, err
	}
	amount, err := fixtures.Money(amountText, q.Currency)
	if err != nil {
		return DomainCall{}, fmt.Errorf("app: amount: %w", err)
	}
	coordinate, err := asOfFrom(inst, asOfDate, optionalStr(payload, "known_at"))
	if err != nil {
		return DomainCall{}, err
	}
	facts, err := f.read(ctx, inst.Tenant, subject, coordinate)
	if err != nil {
		return DomainCall{}, fmt.Errorf("app: governed worker read: %w", err)
	}
	return DomainCall{
		PayBand: &PayBandInputs{Query: q, Amount: amount},
		Baseline: baselineFor(inst, facts, subject, promotion.TargetPlacement{},
			[]string{"employment_ref", "pay_band_ref", "amount"}),
	}, nil
}

// baselineFor builds the kernel's input snapshot from a governed corpus read.
//
// Subject resolution is real: a subject the corpus cannot resolve is simply
// absent from KnownSubjects, and the kernel then reports UNKNOWN_SUBJECT. The
// pinned revision is the read watermark, so an unpinned read is reported as a
// stale baseline rather than silently accepted.
func baselineFor(
	inst intent.Instance,
	facts people.FactSet,
	subject values.EntityRef,
	target promotion.TargetPlacement,
	present []string,
) intent.BaselineSnapshot {
	snapshot := intent.BaselineSnapshot{
		SnapshotID:    "snapshot:" + inst.IntentID,
		ObservedAt:    inst.CreatedAt,
		Revisions:     map[string]values.RevisionToken{},
		PresentInputs: present,
	}
	if facts.Exists {
		snapshot.Revisions[subject.String()] = facts.Watermark
		for _, s := range inst.Subjects {
			if resolvesAgainst(s, subject, target) {
				snapshot.KnownSubjects = append(snapshot.KnownSubjects, s)
			}
		}
	}
	return snapshot
}

// resolvesAgainst reports whether one declared subject is resolved by the
// governed read plus the requested target placement.
func resolvesAgainst(s intent.SubjectReference, worker values.EntityRef, target promotion.TargetPlacement) bool {
	switch strings.ToUpper(s.Kind) {
	case "PERSON", "EMPLOYMENT", "ASSIGNMENT", "WORKER", "COMPENSATION":
		return s.SubjectID == worker.Id || s.SubjectID == worker.String()
	case "POSITION":
		// Promotion resolves the selected position from PositionFacts above;
		// a request string alone is never evidence that a subject exists.
		return false
	case "ORGANIZATION":
		return target.OrgUnit != "" && s.SubjectID == target.OrgUnit
	case "PAY_BAND":
		return true
	default:
		return false
	}
}
