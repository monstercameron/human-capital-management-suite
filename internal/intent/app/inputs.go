package app

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	promosnapshot "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/snapshot"
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

// CorpusInputs resolves domain inputs against the design-partner corpus in
// internal/domains/fixtures and, for the cross-system diagnostics, against
// the configured incumbent connector.
//
// P1A ships no worker projection and no pay-band store: the eight read-only
// intents answer from the corpus. That is a deployment fact, not a test
// convenience -- the corpus is the production source-data authority, so the
// adapter lives beside the service and is what cmd/hcmnext wires in.
// REV-006-01 renamed the former FixtureInputs to say exactly that: the name
// now marks the production resolver, and position-bound promotions resolve
// through the governed promotion snapshot and simulation pipeline, not
// through an ungoverned fixture path. Replacing the corpus with a real
// projection is a change to this file and to the composition root, and to
// nothing else.
type CorpusInputs struct {
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
	// positionReader is the Position read a promotion's target position is
	// re-checked against. It is nil until [NewCell] binds the cell's own
	// ([BindPositionReader]); a nil reader refuses a named position as not
	// found, never passes it through unproven.
	positionReader position.PositionFacts
	// externalSource is the observing system the comparison intents read
	// their external side from. It is empty until the cell binds a connector,
	// and a diagnostic resolved without one refuses rather than comparing
	// against nothing.
	externalSource string
	// pinnedManager answers the manager an intent's recorded proposal
	// revision already pinned (WF-RUN-034). Nil until the cell binds it.
	pinnedManager PinnedManagerReader
	// budgetPools answers the compensation-pool observation the governed
	// promotion snapshot reads. Nil until first use, when the corpus pool
	// loads; tests override it to prove exhaustion and absence.
	budgetPools *corpusBudgetFacts
}

var _ DomainInputs = (*CorpusInputs)(nil)

// NewCorpusInputs loads the corpus-backed production resolver.
func NewCorpusInputs() (*CorpusInputs, error) {
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
	return &CorpusInputs{workers: workers, bands: demoBands, locate: corpusWorkerLocator}, nil
}

// BindExternalSource names the observing system the comparison intents read.
func (f *CorpusInputs) BindExternalSource(ref string) { f.externalSource = ref }

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
func (f *CorpusInputs) BindWorkers(workers people.WorkerFacts) {
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
func (f *CorpusInputs) BindWorkerLocator(locate WorkerLocator) {
	if locate != nil {
		f.locate = locate
	}
}

// BindPositionReader gives the promotion preflight this resolver builds the
// cell's own Position read.
//
// PROMOUX-015: [NewCell] composed a position reader for the workspace form
// path only, so the governed propose path (JourneyService.ProposePromotion and
// the journey engine's Propose) built its preflight with no reader, and
// PROMOUX-004's selection check refused every picker-issued position as not
// found: no real position could be proposed. A nil reader is ignored.
func (f *CorpusInputs) BindPositionReader(reader position.PositionFacts) {
	if reader != nil {
		f.positionReader = reader
	}
}

// BindBands replaces the pay-band catalog this resolver evaluates against.
//
// [NewCorpusInputs] composes the compiled-in catalog, because a resolver
// built without a database has nothing else to read. [NewCell] calls this with
// the cell's own database-backed catalog (internal/data/bandfacts) so the
// simulation that has to certify a promotion prices it against the tenant's
// own stored bands rather than a process-local map. A nil catalog is ignored,
// so a cell that composed none keeps the one this type loaded for itself.
func (f *CorpusInputs) BindBands(bands rewards.PayBandCatalog) {
	if bands != nil {
		f.bands = bands
	}
}

// BindPinnedManager gives this resolver the approval-frozen manager read
// (WF-RUN-034). A promotion's manager is material: it is pinned in the
// proposal revision an approval binds, so every later re-simulation of that
// intent must reuse the pinned value rather than re-read the live reporting
// line. Without this, a manager change between proposal and decision would
// silently mint a different proposal digest and the decision would fail to
// find its own started run instead of taking the approval's INVALIDATED
// route. A nil reader is ignored, and a nil answer means nothing is pinned.
func (f *CorpusInputs) BindPinnedManager(read PinnedManagerReader) {
	if read != nil {
		f.pinnedManager = read
	}
}

// PinnedManagerReader answers the manager an intent's recorded proposal
// revision pinned, reporting false when the intent has recorded none.
type PinnedManagerReader func(ctx context.Context, tenant values.TenantId, intentID string) (string, bool, error)

// Bands exposes the pay-band catalog the domain handlers evaluate against.
func (f *CorpusInputs) Bands() rewards.PayBandCatalog { return f.bands }

// Workers exposes the governed worker read port.
func (f *CorpusInputs) Workers() people.WorkerFacts { return f.workers }

// Resolve implements [DomainInputs].
func (f *CorpusInputs) Resolve(ctx context.Context, req ResolveRequest) (DomainCall, error) {
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
func (f *CorpusInputs) worker(ctx context.Context, tenant values.TenantId, ref string) (values.EntityRef, bool) {
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
// resolves at all, and the revision every fact was read at. Extra fields
// extend the projection for callers that simulate over them (a position-bound
// promotion reads occupancy FTE); the baseline projection is unchanged.
func (f *CorpusInputs) read(ctx context.Context, tenant values.TenantId, worker values.EntityRef, asOf people.AsOf, extra ...people.FieldID) (people.FactSet, error) {
	fields := append(append([]people.FieldID(nil), promotion.RequiredWorkerFields()...), extra...)
	return f.workers.WorkerFactsAt(ctx, people.FactQuery{
		Tenant: tenant,
		Worker: worker,
		AsOf:   asOf,
		Fields: fields,
	})
}

// asOf builds the bitemporal coordinate from an effective date and an optional
// knowledge cut-off, defaulting the cut-off to the instance's creation time.
// The cut-off accepts a full RFC-3339 instant (what the journey declares for
// a created worker, whose record carries intraday precision) or a bare date
// (the corpus evaluation coordinate). A bare date keeps its long-standing
// midnight meaning; parsing an instant first is what keeps a worker created
// after midnight from reading as stale against its own day (REV-006-01).
func asOfFrom(inst intent.Instance, effective values.LocalDate, knownAtText string) (people.AsOf, error) {
	at := inst.CreatedAt
	if knownAtText != "" {
		if instant, err := time.Parse(time.RFC3339Nano, knownAtText); err == nil {
			at = values.NewInstant(instant)
		} else if parsed, err := values.ParseLocalDate(knownAtText); err != nil {
			return people.AsOf{}, fmt.Errorf("app: known_at: %w", err)
		} else {
			at = values.NewInstant(time.Date(int(parsed.Year()), parsed.Month(), int(parsed.Day()), 0, 0, 0, 0, time.UTC))
		}
	}
	known, err := values.NewKnownAt(at)
	if err != nil {
		return people.AsOf{}, fmt.Errorf("app: known_at: %w", err)
	}
	return people.AsOf{EffectiveOn: effective, KnownAt: known}, nil
}

// resolvePromotion decodes a promote_worker payload into the governed read, the
// domain preflight request and the kernel baseline.
func (f *CorpusInputs) resolvePromotion(ctx context.Context, req ResolveRequest, payload *structValue) (DomainCall, error) {
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
	// REV-006-01: a position-bound proposal is baselined through the
	// governed snapshot, which reads exactly
	// promosnapshot.WorkerFactFields. Those fields join the authorized Read
	// set so the snapshot's disclosure decision rules on every field it is
	// asked for; the Gate is unchanged.
	readFields := peopleFields(fieldSet)
	if strings.TrimSpace(target.PositionID) != "" {
		readFields = mergeFields(readFields, peopleFields(promosnapshot.WorkerFactFields()))
	}
	decision, err := authorizeRead(req.Principal, req.Purpose, authorizationRequest{
		Subject:       subject,
		EvaluatedAt:   inst.CreatedAt,
		Gate:          []authz.FieldID{authz.FieldBaseSalary, authz.FieldBonusTarget},
		Read:          readFields,
		Relationships: append(append([]authz.RelationshipFact{}, req.Relationships...), managerChainFacts(ctx, f.locate, req.Principal, subject)...),
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

	// REV-006-01: the occupancy simulation reads the governed FTE, so a
	// position-bound proposal projects it alongside the baseline fields.
	var extraFields []people.FieldID
	if strings.TrimSpace(target.PositionID) != "" {
		extraFields = []people.FieldID{people.FieldFTE}
	}
	facts, err := f.read(ctx, inst.Tenant, subject, asOf, extraFields...)
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
	// REV-006-01: a position-bound proposal builds its baseline through
	// the governed promotion snapshot, not the payload-pinned sides. The
	// snapshot binds current pay, the manager chain, the target position
	// revision and the budget pool observation to their digests; a refused
	// build (unknown position, unresolvable chain, missing or exhausted
	// pool) fails the resolve before any commit path can run.
	baseline := baselineFor(inst, facts, subject, target, present)
	var simulations *PromotionSimulations
	if strings.TrimSpace(target.PositionID) != "" {
		governed, snapErr := f.buildPromotionSnapshot(ctx, promotionSnapshotInput{
			Subject:   subject,
			Target:    promotionTargetPlacement{JobCode: target.JobCode, Grade: target.Grade, OrgUnit: target.OrgUnit, PositionID: target.PositionID, PayZone: target.PayZone},
			Proposed:  proposed,
			Effective: effective,
			AsOf:      asOf,
			Decision:  decision,
			Facts:     facts,
			Budget:    &promotionBudgetAuthority{Scope: budgetScopeOf(budget), Period: budgetPeriodOf(budget)},
		})
		if snapErr != nil {
			return DomainCall{}, snapErr
		}
		baseline = governed.Snapshot.BaselineSnapshot()
		// REV-006-01: the kernel preflights request-level required inputs
		// (employment_ref, effective_time, reason_ref, ...) by NAME, while
		// the snapshot baseline speaks snapshot input names. Both
		// statements are true -- the payload stated those inputs above and
		// the snapshot disclosed its own -- so the kernel baseline unions
		// them; without the union every position-bound proposal reads as
		// missing required data and nothing mints.
		baseline.PresentInputs = unionPresentInputs(baseline.PresentInputs, present)
		// The snapshot's per-input negative states stay in the snapshot,
		// bound by its digest, rather than moving into the kernel baseline.
		// Every substantive absence (manager chain, position revision,
		// pool) already refuses the build or the simulations before a
		// baseline exists; the only negatives that reach this point are
		// informational (a vacancy-after date nobody knows). Carrying those
		// into the kernel baseline would force a CREATE_OBLIGATION for
		// facts no workflow could establish, blocking every promotion the
		// sims just proved executable. The pre-snapshot baseline likewise
		// stated no negatives.
		baseline.NegativeStates = nil
		// planFor pins the commit fence to the governed worker read under
		// the subject's own key, which the snapshot baseline -- keyed by
		// input name -- does not carry. Without the pin every
		// position-bound proposal fails planning with no revision for its
		// subject.
		if facts.Exists {
			if baseline.Revisions == nil {
				baseline.Revisions = map[string]values.RevisionToken{}
			}
			baseline.Revisions[subject.String()] = facts.Watermark
		}
		simulated, simErr := runPromotionSims(governed, promotionSimInput{
			Target:   promotionTargetPlacement{JobCode: target.JobCode, Grade: target.Grade, OrgUnit: target.OrgUnit, PositionID: target.PositionID, PayZone: target.PayZone},
			AsOf:     asOf,
			Decision: decision,
			Facts:    facts,
		})
		if simErr != nil {
			return DomainCall{}, simErr
		}
		simulations = simulated
	}
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
		Baseline:        baseline,
		Simulations:     simulations,
		ManagerWorkerID: f.managerWorkerID(ctx, inst, workerRef),
	}, nil
}

// managerWorkerID resolves a created worker's recorded manager reference to
// the manager's own worker id. A corpus worker, or a manager reference no
// recorded worker answers to, has no manager worker and yields "".
func (f *CorpusInputs) managerWorkerID(ctx context.Context, inst intent.Instance, workerRef string) string {
	tenant := inst.Tenant
	// The manager an approved revision already pinned is approval-frozen
	// material: a re-simulation reuses it rather than re-reading the live
	// reporting line, so a manager change never silently re-mints the
	// proposal's digest under a running approval.
	if f.pinnedManager != nil {
		if pinned, ok, err := f.pinnedManager(ctx, tenant, inst.IntentID); err == nil && ok {
			return pinned
		}
	}
	if f.locate == nil {
		return ""
	}
	subject, ok, err := f.locate(ctx, tenant, workerRef)
	if err != nil || !ok || subject.Created == nil || strings.TrimSpace(subject.Created.ManagerRelationshipRef) == "" {
		return ""
	}
	manager, ok, err := f.locate(ctx, tenant, subject.Created.ManagerRelationshipRef)
	if err != nil || !ok || manager.Created == nil {
		return ""
	}
	return manager.Created.WorkerID.String()
}

// resolveExplain decodes an explain_worker_state payload.
func (f *CorpusInputs) resolveExplain(ctx context.Context, req ResolveRequest, payload *structValue) (DomainCall, error) {
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
		Relationships: append(append([]authz.RelationshipFact{}, req.Relationships...), managerChainFacts(ctx, f.locate, req.Principal, subject)...),
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
func (f *CorpusInputs) resolveCompensation(ctx context.Context, req ResolveRequest, payload *structValue) (DomainCall, error) {
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
		Relationships: append(append([]authz.RelationshipFact{}, req.Relationships...), managerChainFacts(ctx, f.locate, req.Principal, subject)...),
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
func (f *CorpusInputs) resolvePayBand(ctx context.Context, req ResolveRequest, payload *structValue) (DomainCall, error) {
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
		Relationships: append(append([]authz.RelationshipFact{}, req.Relationships...), managerChainFacts(ctx, f.locate, req.Principal, subject)...),
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

// unionPresentInputs merges two present-input lists into a sorted,
// deduplicated union for the kernel baseline. Order is normalized so the
// baseline – and everything digested from it – stays deterministic.
func unionPresentInputs(lists ...[]string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, list := range lists {
		for _, name := range list {
			if strings.TrimSpace(name) == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
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
