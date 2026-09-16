package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/workerids"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// The workforce half of the journey engine: who a promotion can be proposed
// for, and how somebody becomes one of them.
//
// A journey needs a subject. Until this file the only subjects were the four
// workers compiled into internal/domains/fixtures, which made the vertical
// slice a demonstration of one hard-coded promotion rather than of the
// product. ListWorkers is the population -- the fixed corpus plus the tenant's
// own durable creations, said apart by Source -- and CreateWorker is how the
// second half of that population comes to exist.

// The derived halves of a created worker's record. None of them is asked for
// on the form: an identifier a user could choose is an identifier a user could
// collide, and evidence coordinates a user could set are evidence a user could
// forge.
const (
	// journeyWorkerPayBasis is the pay basis every created worker's baseline
	// is carried at. The corpus and the ported legacy scenarios both use it,
	// and internal/domains/rewards' annualization is defined against it.
	journeyWorkerPayBasis = "ANNUAL_SALARY"
	// journeyWorkerDefaultBonusTarget is the bonus target fraction a created
	// worker gets when the form names none. It is the ported legacy corpus's
	// own five percent, so a created worker and omar-reyes simulate against
	// comparable packages.
	journeyWorkerDefaultBonusTarget = "0.0500"
	// journeyWorkerFTE is the assignment allocation every created worker
	// carries. Part-time placement is a real thing this surface does not yet
	// model, and defaulting it silently to something other than full time
	// would be a fact nobody asserted.
	journeyWorkerFTE = "1.0000"
	// journeyWorkerNumberPrefix prefixes the derived worker number.
	journeyWorkerNumberPrefix = "W-J"
	// journeyWorkerLifecycleActive is the lifecycle and employment token a
	// created worker carries. It is the lower-case spelling
	// internal/domains/promotion compares against (its own lifecycleActive),
	// because a created worker the preflight would immediately block on an
	// inactive lifecycle is a worker this surface should not be able to make.
	journeyWorkerLifecycleActive = "active"
	// journeyWorkerType is the employment worker type a created worker
	// carries, in the corpus's own spelling.
	journeyWorkerType = "employee"
)

// Evidence decisions [journeyEngine.CreateWorker] records on the cell's own
// evidence sink, beside the capability invocations and the OBS-024 execution
// gate decisions. A workforce write that left no evidence would be the one
// governed write in this cell nobody could account for.
const (
	// EvidenceKindWorkerCreated records an admitted, committed creation.
	EvidenceKindWorkerCreated = "WORKER_CREATED"
	// EvidenceKindWorkerRefused records a creation the authority gate or the
	// input rules refused, with the reason reference that refused it.
	EvidenceKindWorkerRefused = "WORKER_REFUSED"
	// workforceCapabilityID is the capability identity both decisions are
	// recorded under. It is not a registered capability: this is a governed
	// write the journey performs directly, and recording it through the same
	// sink is what keeps one evidence chronology rather than two.
	workforceCapabilityID = "workforce.create_worker"
)

// Reason references [journeyEngine.CreateWorker] records on a refusal.
const (
	reasonWorkforceRoleRequired = "p1b.workforce_role_required"
	reasonWorkforceInput        = "workforce.invalid_input"
	reasonWorkforceUnavailable  = "workforce.storage_unavailable"
)

// ---------------------------------------------------------------------------
// ListWorkers
// ---------------------------------------------------------------------------

// ListWorkers implements [workspace.JourneyEngine].
//
// The two populations are concatenated rather than merged: created workers
// first, newest first (which is the order the store already returns), then the
// corpus in its own declared order. That ordering is the one a person expects
// -- what I just made is at the top -- and it is stable, because neither half
// is sorted by anything a concurrent write could change.
//
// A cell with no execution database has no created population at all, and
// reports the corpus alone rather than failing: the worker list is a read, and
// a read that refused because a write path is missing would make the corpus
// unusable on exactly the cells that only ever read.
func (e *journeyEngine) ListWorkers(ctx context.Context) ([]workspace.WorkerSummary, workspace.WorkforceOptions, error) {
	principal, err := journeyPrincipal(ctx)
	if err != nil {
		return nil, workspace.WorkforceOptions{}, err
	}
	options, err := workforceOptions()
	if err != nil {
		return nil, workspace.WorkforceOptions{}, err
	}

	created, err := e.listCreated(ctx, principal)
	if err != nil {
		return nil, workspace.WorkforceOptions{}, err
	}
	corpus, err := corpusWorkers()
	if err != nil {
		return nil, workspace.WorkforceOptions{}, err
	}
	return append(created, corpus...), options, nil
}

// listCreated reads the tenant's own durable population, or nothing when this
// cell was composed without one.
func (e *journeyEngine) listCreated(ctx context.Context, principal *trust.Principal) ([]workspace.WorkerSummary, error) {
	if e.db == nil || e.svc.tenantUUID == nil {
		return nil, nil
	}
	tx, err := e.beginTenant(ctx, principal)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := (workforce.Store{}).List(ctx, tx, e.svc.tenantUUID(principal.Tenant()))
	if err != nil {
		return nil, fmt.Errorf("app: journey: list created workers: %w", err)
	}
	out := make([]workspace.WorkerSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, createdWorkerSummary(row))
	}
	return out, nil
}

// createdWorkerSummary projects one durable row onto the port's listing row.
func createdWorkerSummary(row workforce.WorkerRow) workspace.WorkerSummary {
	worker := workspace.WorkerSummary{
		WorkerRef:       row.WorkerKey,
		WorkerID:        row.WorkerID.String(),
		SubjectRevision: fmt.Sprintf("%s@%d", row.RevisionStream, row.RevisionSequence),
		LegalName:       row.LegalName,
		PreferredName:   row.PreferredName,
		WorkerNumber:    row.WorkerNumber,
		JobCode:         row.JobCode,
		JobTitle:        row.JobTitle,
		Grade:           row.Grade,
		OrgUnit:         row.OrgUnit,
		PositionID:      row.PositionID,
		Location:        row.Location,
		PayZone:         row.PayZone,
		BasePay:         row.BasePay,
		Currency:        row.Currency,
		BonusTarget:     row.BonusTarget,
		HireDate:        row.HireDate,
		ManagerRef:      row.ManagerRelationshipRef,
		ProfilePhotoURL: row.ProfilePhotoProxyRef,
		Source:          workspace.WorkerSourceCreated,
		CreatedAt:       row.RecordedAt,
	}
	// A board relationship is an accountable external authority, not a
	// missing employee. It therefore starts the employee ownership tree while
	// the opaque relationship reference remains available to the governed
	// domain. Other unresolved references stay unspecified so the authorized
	// transport projection can distinguish hidden managers from bad edges.
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(row.ManagerRelationshipRef)), "board:") {
		worker.ManagerDisposition = workspace.ManagerRelationshipRoot
	}
	return worker
}

// corpusWorkers projects the release's fixed population onto listing rows.
//
// Compensation is present only where the release declares a baseline for that
// exact worker. Jane's reference promotion and Omar's ported scenario each do;
// the other corpus workers remain blank. This distinction lets the promotion
// form state the current amount it is evaluating without copying one scenario's
// pay onto unrelated employees.
func corpusWorkers() ([]workspace.WorkerSummary, error) {
	profiles, err := fixtures.Workers()
	if err != nil {
		return nil, fmt.Errorf("app: journey: read the worker corpus: %w", err)
	}
	baselines, err := corpusCompensationBaselines()
	if err != nil {
		return nil, err
	}
	out := make([]workspace.WorkerSummary, 0, len(profiles))
	for _, p := range profiles {
		worker := workspace.WorkerSummary{
			WorkerRef:       p.Key,
			WorkerID:        p.ID,
			SubjectRevision: PromotionSubjectRevision(p.Key),
			LegalName:       p.LegalName,
			PreferredName:   p.PreferredName,
			WorkerNumber:    p.WorkerNumber,
			JobCode:         p.JobCode,
			Grade:           p.Grade,
			OrgUnit:         p.OrgUnit,
			PositionID:      p.PositionID,
			Location:        p.Location,
			PayZone:         p.PayZone,
			HireDate:        p.HireDate,
			Source:          workspace.WorkerSourceCorpus,
		}
		if baseline, ok := baselines[p.Key]; ok {
			worker.BasePay = baseline.currentBase
			worker.Currency = baseline.currency
			worker.BonusTarget = baseline.bonusTarget
		}
		out = append(out, worker)
	}
	return out, nil
}

func corpusCompensationBaselines() (map[string]journeyBaselineFacts, error) {
	set, err := fixtures.LegacyScenarios()
	if err != nil {
		return nil, fmt.Errorf("app: journey: read declared compensation baselines: %w", err)
	}
	if len(set.Scenarios) == 0 {
		return nil, fmt.Errorf("app: journey: the promotion corpus declares no scenario")
	}
	legacy := set.Scenarios[0]
	return map[string]journeyBaselineFacts{
		"jane-doe": {
			currentBase: fixtures.JanePromotionBase,
			currency:    "USD",
			bonusTarget: fixtures.JanePromotionBonus,
		},
		set.Worker: {
			currentBase: legacy.CurrentAmount,
			currency:    legacy.CurrentCurrency,
			bonusTarget: legacy.BonusTarget,
		},
	}, nil
}

// workforceOptions derives the closed set of placements a created worker may
// be given.
//
// Job codes, grades, pay zones and the currency come from the pay-band
// catalog, because a placement no band covers is a placement the rewards
// engine can only answer "no band found" for. Org units and positions come
// from the corpus population, because those are organizational facts this cell
// observes rather than prices it evaluates -- there is no catalog of them, and
// offering the ones that demonstrably exist is better than offering a free
// text field that looks like a choice.
func workforceOptions() (workspace.WorkforceOptions, error) {
	scopes, err := fixtures.BandScopes()
	if err != nil {
		return workspace.WorkforceOptions{}, fmt.Errorf("app: journey: read the pay band catalog: %w", err)
	}
	profiles, err := fixtures.Workers()
	if err != nil {
		return workspace.WorkforceOptions{}, fmt.Errorf("app: journey: read the worker corpus: %w", err)
	}
	paths, err := publishedPromotionPaths()
	if err != nil {
		return workspace.WorkforceOptions{}, err
	}

	jobCodes, grades, payZones, currencies := newStringSet(), newStringSet(), newStringSet(), newStringSet()
	for _, s := range scopes {
		jobCodes.add(s.JobCode)
		grades.add(s.Grade)
		payZones.add(s.PayZone)
		currencies.add(s.Currency)
	}
	orgUnits, positions := newStringSet(), newStringSet()
	for _, p := range profiles {
		orgUnits.add(p.OrgUnit)
		positions.add(p.PositionID)
	}

	options := workspace.WorkforceOptions{
		JobCodes:  jobCodes.sorted(),
		Grades:    grades.sorted(),
		OrgUnits:  orgUnits.sorted(),
		PayZones:  payZones.sorted(),
		Positions: positions.sorted(),
	}
	for _, scope := range scopes {
		options.Placements = append(options.Placements, workspace.WorkforcePlacementOption{
			JobCode: scope.JobCode, Grade: scope.Grade,
			PayZone: scope.PayZone, Currency: scope.Currency,
		})
	}
	// PROMOUX-015: the published paths are the one list Propose's ladder gate
	// also reads (publishedPromotionPaths), so a listed edge is an accepted one.
	for _, path := range paths {
		options.PromotionPaths = append(options.PromotionPaths, path.Option)
	}
	// The catalog is single-currency in this release. If it ever is not, a
	// created worker's currency stops being derivable and becomes a choice,
	// which is a port change rather than a silent pick of the first one.
	if declared := currencies.sorted(); len(declared) == 1 {
		options.Currency = declared[0]
	}
	appendDemoWorkforcePlacements(&options)
	return options, nil
}

// appendDemoWorkforcePlacements adds the placements and the job codes and
// grades the seeded demo company's own career-ladder edges lead to; the edges
// themselves are published by [publishedPromotionPaths].
//
// PROMOUX-001: before this, workforceOptions only ever published the fixed
// four-worker conformance corpus's job architecture (internal/domains/fixtures,
// job-architecture.json). None of that catalog's job codes match any of the
// demo company's own staffing roles (internal/data/demoworkforce), so every
// demoworkforce-seeded worker resolved zero governed promotion choices for a
// purely structural reason -- a catalog gap, not a real eligibility
// verdict -- and looked identical to a worker the rules had actually
// evaluated and refused. Publishing the demo company's own ladder here (the
// same workspace.PromotionPathOption/WorkforcePlacementOption contract the
// fixed corpus already speaks) gives both populations one governed
// job-architecture model, which is what a client's eligibility read can
// finally tell apart.
//
// This is intentionally unscoped by tenant, matching corpusWorkers' own
// unconditional append just above: this release's journey engine already
// publishes the fixed four-worker corpus to every tenant that reaches it, so
// adding one more fixed, non-secret demo catalog does not introduce a new
// category of cross-tenant leakage. A tenant-scoped job architecture is a
// larger, separate change.
func appendDemoWorkforcePlacements(options *workspace.WorkforceOptions) {
	edges := demoworkforce.PromotionPaths()
	if len(edges) == 0 {
		return
	}
	zones := demoworkforce.PayZones()
	jobCodes, grades := newStringSet(), newStringSet()
	seenPlacement := map[string]bool{}
	for _, edge := range edges {
		jobCodes.add(edge.SourceJobCode)
		jobCodes.add(edge.TargetJobCode)
		grades.add(edge.SourceGrade)
		grades.add(edge.TargetGrade)
		for _, zone := range zones {
			key := edge.TargetJobCode + "|" + edge.TargetGrade + "|" + zone
			if seenPlacement[key] {
				continue
			}
			seenPlacement[key] = true
			options.Placements = append(options.Placements, workspace.WorkforcePlacementOption{
				JobCode: edge.TargetJobCode, Grade: edge.TargetGrade, PayZone: zone, Currency: "USD",
			})
		}
	}
	for _, code := range jobCodes.sorted() {
		if !containsString(options.JobCodes, code) {
			options.JobCodes = append(options.JobCodes, code)
		}
	}
	sort.Strings(options.JobCodes)
	for _, grade := range grades.sorted() {
		if !containsString(options.Grades, grade) {
			options.Grades = append(options.Grades, grade)
		}
	}
	sort.Strings(options.Grades)
}

func containsString(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

// bandCovers reports whether the catalog carries a band for exactly this
// placement and currency. It is the same lookup rewards.PayBandCatalog would
// perform at simulation time, asked early so the refusal names the field
// instead of the simulation naming a missing band.
func bandCovers(jobCode, grade, payZone, currency string) (bool, error) {
	scopes, err := fixtures.BandScopes()
	if err != nil {
		return false, fmt.Errorf("app: journey: read the pay band catalog: %w", err)
	}
	for _, s := range scopes {
		if s.JobCode == jobCode && s.Grade == grade && s.PayZone == payZone && s.Currency == currency {
			return true, nil
		}
	}
	return false, nil
}

// stringSet collects distinct option values without importing a set library
// for six lines of work.
type stringSet map[string]struct{}

func newStringSet() stringSet { return stringSet{} }

func (s stringSet) add(v string) {
	if v != "" {
		s[v] = struct{}{}
	}
}

func (s stringSet) sorted() []string {
	out := make([]string, 0, len(s))
	for v := range s {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// CreateWorker
// ---------------------------------------------------------------------------

// CreateWorker implements [workspace.JourneyEngine].
//
// It is gated by [IntentService.authorizeExecution], the same P1B execution
// authority [journeyEngine.Decide] resumes a parked instance under. That is
// deliberate and it is the conservative reading: adding an employee is a
// workforce write against the demo authority, and a surface that could create
// the subject of a promotion without the authority to execute promotions would
// be a way around the gate rather than a step before it. The gate is checked
// against the promote_worker definition because that is the intent type this
// authority admits and the one every created worker exists to be the subject
// of.
//
// The decision is recorded on the cell's own evidence sink either way, under
// [workforceCapabilityID], so an admitted creation and a refused one both
// appear in the one chronology Cell.Evidence reads back.
func (e *journeyEngine) CreateWorker(ctx context.Context, in workspace.WorkerInput) (workspace.WorkerSummary, error) {
	principal, err := journeyPrincipal(ctx)
	if err != nil {
		return workspace.WorkerSummary{}, err
	}
	def, ownedErr := e.svc.defs.Resolve(intent.Ref{TypeID: promotion.IntentType, Version: 1})
	if ownedErr != nil {
		return workspace.WorkerSummary{}, fmt.Errorf(
			"%w: this cell publishes no promotion definition to create workers for", workspace.ErrJourneyUnavailable)
	}
	if gateErr := e.svc.authorizeExecution(principal, def); gateErr != nil {
		e.recordWorkforceEvidence(ctx, principal, EvidenceKindWorkerRefused, workforceSubject(in), reasonWorkforceRoleRequired)
		return workspace.WorkerSummary{}, journeyError(gateErr)
	}
	if e.db == nil || e.svc.tenantUUID == nil {
		e.recordWorkforceEvidence(ctx, principal, EvidenceKindWorkerRefused, workforceSubject(in), reasonWorkforceUnavailable)
		return workspace.WorkerSummary{}, fmt.Errorf(
			"%w: this cell was composed with no execution database", workspace.ErrJourneyUnavailable)
	}

	options, err := workforceOptions()
	if err != nil {
		return workspace.WorkerSummary{}, err
	}

	// The reservation and the row it names share one transaction now
	// (PROMOUX-001 follow-up): worker_id_reservation is append-only, so a
	// reservation made in its own transaction and then orphaned by a
	// failure in a later, separate transaction (the shape newWorkerRowContext
	// + insertWorker used to have) could never be released or reused --
	// every retry burned another number while creating nothing. Opening the
	// transaction here and threading it through both steps means a failure
	// anywhere rolls back the reservation with it.
	tx, err := e.beginTenant(ctx, principal)
	if err != nil {
		return workspace.WorkerSummary{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row, err := e.newWorkerRowContext(ctx, tx, principal, in, options)
	if err != nil {
		e.recordWorkforceEvidence(ctx, principal, EvidenceKindWorkerRefused, workforceSubject(in), reasonWorkforceInput)
		return workspace.WorkerSummary{}, err
	}

	stored, err := e.insertWorker(ctx, tx, row)
	if err != nil {
		e.recordWorkforceEvidence(ctx, principal, EvidenceKindWorkerRefused, row.WorkerKey, reasonWorkforceInput)
		return workspace.WorkerSummary{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return workspace.WorkerSummary{}, fmt.Errorf("app: journey: commit the worker: %w", err)
	}
	e.recordWorkforceEvidence(ctx, principal, EvidenceKindWorkerCreated, stored.WorkerKey, "")
	return createdWorkerSummary(stored), nil
}

// insertWorker inserts one worker inside tx, which the caller owns and will
// itself commit or roll back, and projects a duplicate key onto the port's
// input refusal.
//
// A duplicate is [workspace.ErrJourneyInput] and not a stage refusal: the name
// the key was derived from is a field on the form, so it is a field the person
// can change. The message names worker_key rather than the name field because
// the key is what actually collided, and two different people can legitimately
// share a name.
func (e *journeyEngine) insertWorker(ctx context.Context, tx dbport.Tx, row workforce.WorkerRow) (workforce.WorkerRow, error) {
	stored, err := (workforce.Store{}).Create(ctx, tx, row)
	if err != nil {
		switch {
		case isWorkforceDuplicate(err):
			return workforce.WorkerRow{}, journeyInputError("worker_key",
				"an employee with that reference already exists in this tenant")
		case isWorkforceInvalidRow(err):
			return workforce.WorkerRow{}, journeyInputError("worker", err.Error())
		}
		return workforce.WorkerRow{}, fmt.Errorf("app: journey: create the worker: %w", err)
	}
	return stored, nil
}

// newWorkerRow validates the form and derives everything the form does not
// supply: the identity, the key, the revision-stream position, the bitemporal
// coordinates and the employment/assignment identifiers.
func (e *journeyEngine) newWorkerRow(
	principal *trust.Principal, in workspace.WorkerInput, options workspace.WorkforceOptions,
) (workforce.WorkerRow, error) {
	return e.newWorkerRowContext(context.Background(), nil, principal, in, options)
}

// txReserver is the additional capability a workerids.Store may offer:
// reserving a worker number inside a transaction the caller already owns,
// rather than in one Reserve commits by itself. It is declared here, not
// widened onto workerids.Store, because every other caller of that
// interface (the Worker IDs admin settings page) reserves nothing that a
// worker row needs to land atomically with -- only worker creation does.
// *workeridstore.Store implements it; a test double that implements only
// workerids.Store falls back to the older, non-atomic Reserve path below.
type txReserver interface {
	ReserveTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, organization, actor string, fc workerids.FormatContext) (string, error)
}

// newWorkerRowContext derives everything a create-worker request needs,
// including the reserved worker number. When tx is non-nil and e.workerIDs
// supports it, the reservation is made inside tx -- the same transaction
// CreateWorker will use to insert the row -- so a subsequent insert failure
// rolls the reservation back with it instead of permanently burning a
// number nothing was ever created for. tx is nil for the test-only
// newWorkerRow wrapper, which always uses the older, self-committing
// Reserve (e.workerIDs is nil in every existing internal/intent/app test).
func (e *journeyEngine) newWorkerRowContext(
	ctx context.Context, tx dbport.Tx, principal *trust.Principal, in workspace.WorkerInput, options workspace.WorkforceOptions,
) (workforce.WorkerRow, error) {
	legal := strings.TrimSpace(in.LegalName)
	if legal == "" {
		return workforce.WorkerRow{}, journeyInputError("legal_name", "is required")
	}
	preferred := strings.TrimSpace(in.PreferredName)
	if preferred == "" {
		// A person always has a name to be addressed by. Deriving the first
		// part of the legal name is the same convention the corpus follows
		// ("Omar Reyes" is addressed as "Omar"), and it is better than
		// showing an employee with a blank name because a form was short.
		preferred = strings.Fields(legal)[0]
	}

	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = options.Currency
	}
	if currency == "" {
		return workforce.WorkerRow{}, journeyInputError("currency", "is required and this cell declares no default")
	}

	placement, err := validatePlacement(in, currency)
	if err != nil {
		return workforce.WorkerRow{}, err
	}

	base, err := fixtures.Money(strings.TrimSpace(in.BasePay), currency)
	if err != nil {
		return workforce.WorkerRow{}, journeyInputError("base_pay", "is not a decimal amount")
	}
	if base.Amount().Sign() <= 0 {
		return workforce.WorkerRow{}, journeyInputError("base_pay", "must be greater than zero")
	}

	bonusText := strings.TrimSpace(in.BonusTarget)
	if bonusText == "" {
		bonusText = journeyWorkerDefaultBonusTarget
	}
	bonus, err := fixtures.Percent(bonusText)
	if err != nil {
		return workforce.WorkerRow{}, journeyInputError("bonus_target", "is not a decimal fraction")
	}
	if bonus.Fraction().Sign() < 0 {
		return workforce.WorkerRow{}, journeyInputError("bonus_target", "must not be negative")
	}

	hireText := strings.TrimSpace(in.HireDate)
	if hireText == "" {
		return workforce.WorkerRow{}, journeyInputError("hire_date", "is required")
	}
	hire, err := values.ParseLocalDate(hireText)
	if err != nil {
		return workforce.WorkerRow{}, journeyInputError("hire_date", "is not an ISO-8601 date (YYYY-MM-DD)")
	}

	workerID, err := e.mintWorkerID()
	if err != nil {
		return workforce.WorkerRow{}, err
	}
	now := e.now().UTC()
	short := shortID(workerID)
	workerNumber := journeyWorkerNumberPrefix + strings.ToUpper(short)
	if e.workerIDs != nil {
		fc := workerids.FormatContext{At: now, UnitCode: placement.OrgUnit}
		reserver, txCapable := e.workerIDs.(txReserver)
		switch {
		case tx != nil && txCapable && e.svc.tenantUUID != nil:
			workerNumber, err = reserver.ReserveTx(ctx, tx, e.svc.tenantUUID(principal.Tenant()), principal.OrganizationScopeID(), principal.Subject(), fc)
		default:
			workerNumber, err = e.workerIDs.Reserve(ctx, principal.Tenant(), principal.OrganizationScopeID(), principal.Subject(), fc)
		}
		if err != nil {
			return workforce.WorkerRow{}, fmt.Errorf("app: journey: reserve worker number: %w", err)
		}
	}
	manager := strings.TrimSpace(in.ManagerRef)
	if manager == "" {
		manager = "rel_mgr_" + short
	}

	return workforce.WorkerRow{
		TenantID:               e.svc.tenantUUID(principal.Tenant()),
		WorkerID:               workerID,
		WorkerKey:              journeyWorkerKey(preferred, legal, short),
		LegalName:              legal,
		PreferredName:          preferred,
		WorkerNumber:           workerNumber,
		WorkerType:             journeyWorkerType,
		LifecycleStatus:        journeyWorkerLifecycleActive,
		EmploymentID:           "emp_" + short,
		AssignmentID:           "asg_" + short,
		JobCode:                placement.JobCode,
		Grade:                  placement.Grade,
		OrgUnit:                placement.OrgUnit,
		PositionID:             placement.PositionID,
		Location:               placement.Location,
		PayZone:                placement.PayZone,
		FTE:                    journeyWorkerFTE,
		ManagerRelationshipRef: manager,
		HireDate:               hire.String(),
		EffectiveFrom:          hire.String(),
		BasePay:                base.Amount().String(),
		Currency:               currency,
		PayBasis:               journeyWorkerPayBasis,
		BonusTarget:            bonus.Fraction().String(),
		RevisionStream:         "people.worker." + workerID.String(),
		RevisionSequence:       1,
		KnownAt:                now,
		RecordedAt:             now,
		CreatedBy:              principal.Subject(),
		Source:                 workforce.SourceCreated,
	}, nil
}

// workerPlacement is the validated organizational half of a create form.
type workerPlacement struct {
	JobCode    string
	Grade      string
	OrgUnit    string
	PositionID string
	Location   string
	PayZone    string
}

// validatePlacement refuses a placement the simulation could not evaluate,
// naming the field that has to change.
//
// The band check is the RED clause of this surface: creating a worker on a job
// code, grade and zone no band covers produces a worker whose every promotion
// is refused at simulation time for a reason that has nothing to do with the
// promotion. Refusing it here turns a confusing later failure into an
// immediate, specific one.
func validatePlacement(in workspace.WorkerInput, currency string) (workerPlacement, error) {
	p := workerPlacement{
		JobCode:    strings.TrimSpace(in.JobCode),
		Grade:      strings.TrimSpace(in.Grade),
		OrgUnit:    strings.TrimSpace(in.OrgUnit),
		PositionID: strings.TrimSpace(in.PositionID),
		Location:   strings.TrimSpace(in.Location),
		PayZone:    strings.TrimSpace(in.PayZone),
	}
	for _, field := range []struct{ name, value string }{
		{"job_code", p.JobCode},
		{"grade", p.Grade},
		{"org_unit", p.OrgUnit},
		{"position_id", p.PositionID},
		{"pay_zone", p.PayZone},
	} {
		if field.value == "" {
			return workerPlacement{}, journeyInputError(field.name, "is required")
		}
	}
	if p.Location == "" {
		// Location is a display fact no rule reads; the org unit is the
		// closest thing to a place this record actually knows.
		p.Location = p.OrgUnit
	}

	covered, err := bandCovers(p.JobCode, p.Grade, p.PayZone, currency)
	if err != nil {
		return workerPlacement{}, err
	}
	if !covered {
		return workerPlacement{}, journeyInputError("job_code", fmt.Sprintf(
			"no pay band covers %s/%s/%s in %s, so no promotion could be simulated for this worker",
			p.JobCode, p.Grade, p.PayZone, currency))
	}
	return p, nil
}

// mintWorkerID mints the created worker's entity identity through the cell's
// own id source, so a deterministic composition mints deterministic workers.
func (e *journeyEngine) mintWorkerID() (uuid.UUID, error) {
	minted, err := e.svc.ids()
	if err != nil {
		return uuid.Nil, fmt.Errorf("app: journey: mint a worker id: %w", err)
	}
	parsed, err := uuid.Parse(minted)
	if err != nil {
		return uuid.Nil, fmt.Errorf("app: journey: the cell's id source minted %q, which is not a uuid: %w", minted, err)
	}
	return parsed, nil
}

// shortID is the disambiguating suffix a created worker's key, number and
// employment/assignment identifiers are built from.
func shortID(id uuid.UUID) string { return id.String()[:8] }

// journeyWorkerKey derives the stable reference the journey lists and accepts.
//
// It is the name slug plus a short form of the entity id, never the name
// alone: two people may share a name, the key is unique per tenant, and a
// creation that collided on a name would be a person the surface refused to
// admit exists. A name with nothing sluggable in it (only punctuation, say)
// falls back to the legal name and then to the fallback token, so the key
// always has a readable head rather than a bare separator.
func journeyWorkerKey(preferred, legal, short string) string {
	name := journeyWorkerSlug(preferred)
	if name == "" {
		name = journeyWorkerSlug(legal)
	}
	if name == "" {
		name = "worker"
	}
	return name + "-" + short
}

// journeyWorkerSlug is [journeySanitize] with its own fallback token and any
// leading or trailing separators removed, or "" when nothing readable is
// left.
func journeyWorkerSlug(name string) string {
	slug := strings.Trim(journeySanitize(name), "-")
	if slug == "worker" && strings.TrimSpace(name) == "" {
		return ""
	}
	return slug
}

// workforceSubject is the evidence subject reference for a creation that never
// got as far as having a key.
func workforceSubject(in workspace.WorkerInput) string {
	if name := strings.TrimSpace(in.PreferredName); name != "" {
		return journeySanitize(name)
	}
	if name := strings.TrimSpace(in.LegalName); name != "" {
		return journeySanitize(name)
	}
	return "worker"
}

// recordWorkforceEvidence writes one workforce decision to the cell's evidence
// sink.
//
// The sink's own failure is swallowed on purpose, and only here: this is a
// best-effort chronology entry beside a decision that has already been made,
// and turning "the evidence sink is full" into "you may not create an
// employee" would make the audit trail an availability dependency of the
// action it audits. [IntentService.ExecuteIntent] treats its own gate evidence
// as load-bearing precisely because it is written before the decision; this
// one is written after.
func (e *journeyEngine) recordWorkforceEvidence(ctx context.Context, principal *trust.Principal, kind, subject, reason string) {
	if e.svc.evidence == nil || principal == nil {
		return
	}
	_, _ = e.svc.evidence.RecordInvocation(ctx, capability.InvocationEvidence{
		CapabilityID:      workforceCapabilityID,
		CapabilityVersion: 1,
		SubjectRef:        subject,
		Tenant:            principal.Tenant().String(),
		Decision:          kind,
		ReasonCode:        reason,
		OccurredAt:        e.workforceInstant(),
	})
}

// workforceInstant is the instant a workforce evidence entry is stamped with.
// It is the engine's own clock rather than the service's trusted one so that a
// composition pinning [CellConfig.Now] gets a pinned evidence chronology too.
func (e *journeyEngine) workforceInstant() time.Time { return e.now().UTC() }

// isWorkforceDuplicate and isWorkforceInvalidRow classify the store's two
// typed refusals, which are the only storage failures this surface can turn
// into something the person on the form can act on.
func isWorkforceDuplicate(err error) bool  { return errors.Is(err, workforce.ErrDuplicate) }
func isWorkforceInvalidRow(err error) bool { return errors.Is(err, workforce.ErrInvalidRow) }
