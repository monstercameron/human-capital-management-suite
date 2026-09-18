package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/workerids"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator/workflowcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// The Promotion journey engine: the server side of the workspace's journey
// surface (internal/humanwork/workspace's JourneyEngine port).
//
// Everything here runs through the engine's own governed operations. Propose
// is CreateIntent plus SimulateIntent, Execute is [IntentService.ExecuteIntent]
// behind the P1B execution-authority gate, Decide is the real
// internal/humanwork/workitem claim/start/complete plus the caller-driven
// driver's Resume, and Inspect reads only what those wrote: the stored intent,
// a read-only re-simulation, the durable workflow instance, its node
// executions, its WorkItems and their transitions, and the one ledger fact the
// END node recorded. There is no second, page-shaped path to any of it.

// DefaultJourneyApprover is who the approval WorkItem is routed to, and
// therefore the principal the journey engine records the decision as, when a
// composition names no [CellConfig.ExecutionApprover] of its own. It is the
// same default internal/platform/execution's own work-item factory uses; the
// two must agree, because the routed assignment is what authorizes the
// decision.
const DefaultJourneyApprover = "principal:promotion-approver"

// journeyListPageSize bounds the one page of intents [journeyEngine.ListJourneys]
// reads. The journey list is a demo surface over one tenant's promotions, not
// a paginated report.
const journeyListPageSize int32 = 200

// journeyEngine implements [workspace.JourneyEngine] over one composed cell.
type journeyEngine struct {
	svc *IntentService
	// db is the pool the durable execution record is read through and the
	// approval WorkItem is claimed and completed in. Every statement runs
	// inside a tenant-scoped transaction.
	db dbport.Beginner
	// approver is the principal the approval WorkItem is routed to.
	approver string
	now      func() time.Time
	// locate is the cell's own worker-reference resolver. The journey shares
	// it with the domain-input resolver and the workspace read surface, so a
	// reference the list shows is a reference the proposal resolves.
	locate    WorkerLocator
	workerIDs workerids.Store
	// recorder receives the workflow-engine operations the journey runs
	// outside the execution driver (the approval step completion).
	recorder WorkflowRecorder
	// authority re-resolves a promotion approval's authority at decision
	// time (WF-STEP-003). Nil refuses every promotion-class decision.
	authority ApprovalAuthoritySource
	// positions and positionReader are UXLIVE-011's vacancy projection: the
	// directory this cell can read and the Position domain's own facts port
	// that decides which of those rows is real, authorized and still open.
	// Both nil on a cell with no execution database, which yields no
	// vacancy options at all -- never a free-text fallback.
	positions      PositionDirectorySource
	positionReader position.PositionFacts
	// repair and repairAuthority are UXLIVE-006's governed repair
	// projection: the door a REPAIR_REQUIRED journey names as its next step,
	// and the resolver that says whether this viewer currently holds the
	// grant to open it. Both nil on a cell composed without an execution
	// database, which the preview reports as "this deployment cannot
	// repair" -- never as "no repair is needed".
	repair          *workflowcontrol.RepairController
	repairAuthority workflowcontrol.AuthorityResolver
}

var _ workspace.JourneyEngine = (*journeyEngine)(nil)

// newJourneyEngine composes the engine [NewCell] hands the workspace.
func newJourneyEngine(
	svc *IntentService, db dbport.Beginner, approver string, now func() time.Time, locate WorkerLocator, stores ...workerids.Store,
) *journeyEngine {
	if approver == "" {
		approver = DefaultJourneyApprover
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if locate == nil {
		locate = corpusWorkerLocator
	}
	var workerIDStore workerids.Store
	if len(stores) > 0 {
		workerIDStore = stores[0]
	}
	engine := &journeyEngine{svc: svc, db: db, approver: approver, now: now, locate: locate, workerIDs: workerIDStore}
	if svc != nil {
		svc.bindProposalDecisioner(engine)
	}
	return engine
}

// ---------------------------------------------------------------------------
// Refusal projection
// ---------------------------------------------------------------------------

// journeyError projects one owned envelope refusal onto the journey port's own
// sentinels, so the page can answer without importing the envelope model.
//
// The mapping is by the reason reference the service already owns, never by
// message text: a permission denial is [workspace.ErrDenied], a missing intent
// is ErrJourneyUnknown, "this cell cannot execute at all" is
// ErrJourneyUnavailable, and "this intent has nothing executable right now" is
// ErrJourneyStage. Anything else travels as the owned error.
func journeyError(err error) error {
	if err == nil {
		return nil
	}
	var owned *envelope.Error
	if !errors.As(err, &owned) {
		return err
	}
	switch owned.Code() {
	case envelope.CodePermissionDenied:
		return fmt.Errorf("%w: %s", workspace.ErrDenied, owned.Error())
	case envelope.CodeNotFound:
		return fmt.Errorf("%w: %s", workspace.ErrJourneyUnknown, owned.Error())
	}
	switch owned.ReasonRef() {
	case reasonNoExecutablePlan, reasonProposalDecisionRejected, reasonProposalDecisionExpired, reasonProposalDecisionInvalidated,
		reasonProposalDecisionConflict, reasonProposalDecisionStage, reasonStaleRevision, reasonPromotionActive:
		return fmt.Errorf("%w: %s", workspace.ErrJourneyStage, owned.Error())
	case reasonExecutionUnavailable, reasonNoGovernedWrite, reasonProposalDecisionRoute:
		return fmt.Errorf("%w: %s", workspace.ErrJourneyUnavailable, owned.Error())
	case reasonProposalDecisionSeparation:
		return fmt.Errorf("%w: %s", workspace.ErrDenied, owned.Error())
	case reasonPromotionActiveConflict:
		return fmt.Errorf("%w: %s", workspace.ErrJourneyActiveConflict, owned.Error())
	}
	return fmt.Errorf("app: journey: %w", err)
}

// journeyInputError names the field the manager has to fix.
func journeyInputError(field, detail string) error {
	return journeyInputErrorReason(field, "journey.input.invalid", detail)
}

func journeyInputErrorReason(field, reason, detail string) error {
	return &workspace.JourneyInputError{FieldPath: field, ReasonRef: reason, Detail: detail}
}

// journeyPrincipal reads the verified principal the whole call runs on behalf
// of. It is the same admission the workspace's read surface uses.
func journeyPrincipal(ctx context.Context) (*trust.Principal, error) {
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil {
		return nil, fmt.Errorf("%w: no verified principal", workspace.ErrDenied)
	}
	return principal, nil
}

// ---------------------------------------------------------------------------
// ListJourneys
// ---------------------------------------------------------------------------

// ListJourneys implements [workspace.JourneyEngine].
//
// The list is one bounded page of the caller's own tenant's intents, filtered
// to promote_worker and rendered by its latest durable change. An executed
// journey is resolved through proposal_revision -> work_item ->
// workflow_instance and the terminal ledger event; it is never re-simulated
// to manufacture history from today's rules. Only a never-executed draft is
// re-simulated so the live work list can report whether it remains executable.
//
// This split follows the storage contract: mutable runtime rows answer where
// current execution is, while append-oriented proposal and ledger rows explain
// what historical transaction actually closed.
func (e *journeyEngine) ListJourneys(ctx context.Context) ([]workspace.JourneySummary, error) {
	principal, err := journeyPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	var all []*intentsv1.IntentInstance
	cursor := ""
	for {
		listed, listErr := e.svc.ListIntents(ctx, &intentsv1.ListIntentsRequest{Page: &commonv1.PageRequest{PageSize: journeyListPageSize, Cursor: cursor}})
		if listErr != nil {
			return nil, journeyError(listErr)
		}
		all = append(all, listed.GetIntents()...)
		cursor = listed.GetPage().GetNextCursor()
		if cursor == "" {
			break
		}
	}

	tx, txErr := e.beginTenant(ctx, principal)
	if txErr != nil {
		return nil, txErr
	}
	defer func() { _ = tx.Rollback(ctx) }()

	out := make([]workspace.JourneySummary, 0, len(all))
	// UXAUDIT-017: one clock reading and one bounded name resolver for the
	// whole page; the work item summary itself reuses record.items below.
	now := e.now()
	assigneeName := e.assigneeNameResolver(ctx, principal.Tenant())
	for _, msg := range all {
		if msg.GetDefinition().GetIntentTypeId() != promotion.IntentType {
			continue
		}
		summary, sumErr := journeySummaryFromProto(msg)
		if sumErr != nil {
			// A promotion intent whose payload this build cannot read is a
			// row the page must still be able to show, so it is listed at its
			// stored identity with no derived placement rather than failing
			// the whole list.
			unreadable := workspace.JourneySummary{
				IntentID: msg.GetIntentId(), CorrelationID: msg.GetCorrelationId(),
				Stage: workspace.JourneyStageBlocked,
			}
			unreadable.Viewer = journeyViewerProjection(unreadable.Stage,
				isJourneyInitiator(msg.GetInitiator().GetPrincipalId(), principal.Subject()), nil)
			out = append(out, unreadable)
			continue
		}
		materialDigest, record, executed, recErr := e.readExecutedRecordForIntent(ctx, tx, principal, summary.IntentID)
		if recErr != nil {
			return nil, recErr
		}
		if executed {
			summary.MaterialDigest = materialDigest
			summary.Stage = deriveJourneyStage("stored-proposal", record)
		} else {
			artifact, _ := e.resimulate(ctx, msg.GetIntentId())
			summary.ProposalRevisionID = artifact.GetProposalRevisionId()
			summary.MaterialDigest = artifact.GetMaterialProposalDigest().GetDigest()
			record, recErr = e.readRecord(ctx, tx, principal, summary.MaterialDigest)
			if recErr != nil {
				return nil, recErr
			}
			summary.Stage = deriveJourneyStage(summary.ProposalRevisionID, record)
		}
		if record.instance != nil {
			summary.InstanceID = record.instance.InstanceID.String()
			summary.InstanceVersion = record.instance.InstanceVersion
		}
		if item, open := openJourneyWorkItem(record.items); open {
			summary.Approver = item.Assignment.ChosenOwner
		}
		applyDurableJourneyTime(&summary, record)
		summary.CurrentWorkItem = journeyWorkItemSummary(record.items, principal.Subject(), principal.OrganizationScopeID(), now, assigneeName)
		// PROMOUX-012: the viewer's relationship and the next transition, from
		// the stored initiator and the membership just disclosed above.
		summary.Viewer = journeyViewerProjection(summary.Stage,
			isJourneyInitiator(msg.GetInitiator().GetPrincipalId(), principal.Subject()), summary.CurrentWorkItem)
		out = append(out, summary)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].IntentID > out[j].IntentID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func (e *journeyEngine) ListJourneysPage(ctx context.Context, req workspace.JourneyListRequest) (workspace.JourneyListPage, error) {
	all, err := e.ListJourneys(ctx)
	if err != nil {
		return workspace.JourneyListPage{}, err
	}
	query := strings.ToLower(strings.TrimSpace(req.Query))
	worker := strings.TrimSpace(req.WorkerRef)
	filtered := all[:0]
	for _, item := range all {
		if worker != "" && item.Worker.String() != worker {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(item.WorkerName+" "+item.BusinessReason+" "+item.IntentID), query) {
			continue
		}
		if outcome := strings.ToLower(strings.TrimSpace(req.Outcome)); outcome != "" && strings.ToLower(string(item.Stage)) != outcome {
			continue
		}
		if year := strings.TrimSpace(req.Year); year != "" && !strings.HasPrefix(item.EffectiveDate, year) {
			continue
		}
		filtered = append(filtered, item)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		var left, right string
		switch strings.ToLower(req.Sort) {
		case "person":
			left, right = strings.ToLower(filtered[i].WorkerName), strings.ToLower(filtered[j].WorkerName)
		case "change":
			left, right = strings.ToLower(filtered[i].Target.JobCode), strings.ToLower(filtered[j].Target.JobCode)
		default:
			left, right = filtered[i].UpdatedAt.UTC().Format(time.RFC3339Nano), filtered[j].UpdatedAt.UTC().Format(time.RFC3339Nano)
		}
		if left == right {
			return filtered[i].IntentID < filtered[j].IntentID
		}
		if strings.ToLower(req.Direction) == "desc" {
			return left > right
		}
		return left < right
	})
	size := req.PageSize
	if size <= 0 {
		size = 50
	}
	if size > 100 {
		size = 100
	}
	start := 0
	if req.Page > 1 {
		start = (req.Page - 1) * size
	}
	fingerprint := historyQueryFingerprint(req, size)
	if req.Cursor != "" {
		raw, decErr := base64.RawURLEncoding.DecodeString(req.Cursor)
		if decErr != nil {
			return workspace.JourneyListPage{}, fmt.Errorf("journey: invalid history cursor: %w", decErr)
		}
		var cursor historyCursor
		decErr = json.Unmarshal(raw, &cursor)
		if decErr != nil || cursor.Fingerprint != fingerprint || cursor.Offset < 0 {
			return workspace.JourneyListPage{}, fmt.Errorf("journey: invalid history cursor")
		}
		start = cursor.Offset
	}
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + size
	if end > len(filtered) {
		end = len(filtered)
	}
	next := ""
	if end < len(filtered) {
		nextBytes, _ := json.Marshal(historyCursor{Fingerprint: fingerprint, Offset: end})
		next = base64.RawURLEncoding.EncodeToString(nextBytes)
	}
	return workspace.JourneyListPage{Journeys: append([]workspace.JourneySummary(nil), filtered[start:end]...), NextCursor: next, TotalCount: len(filtered)}, nil
}

type historyCursor struct {
	Fingerprint string `json:"fingerprint"`
	Offset      int    `json:"offset"`
}

func historyQueryFingerprint(req workspace.JourneyListRequest, size int) string {
	canonical := strings.Join([]string{strings.TrimSpace(req.WorkerRef), strings.ToLower(strings.TrimSpace(req.Query)), strings.ToLower(strings.TrimSpace(req.Outcome)), strings.TrimSpace(req.Year), strings.ToLower(strings.TrimSpace(req.Sort)), strings.ToLower(strings.TrimSpace(req.Direction)), strconv.Itoa(size)}, "\x00")
	sum := sha256.Sum256([]byte(canonical))
	return fmt.Sprintf("%x", sum[:])
}

// applyDurableJourneyTime makes the summary's updated coordinate follow the
// latest durable business transition, including an in-flight approval. The
// intent's simulation timestamp remains the floor for an unexecuted draft.
func applyDurableJourneyTime(summary *workspace.JourneySummary, record journeyRecord) {
	if summary == nil {
		return
	}
	latest := summary.UpdatedAt
	advance := func(at time.Time) {
		if at.After(latest) {
			latest = at.UTC()
		}
	}
	if record.instance != nil {
		advance(record.instance.CreatedAt)
		if record.instance.StartedAt != nil {
			advance(*record.instance.StartedAt)
		}
		if record.instance.CompletedAt != nil {
			advance(*record.instance.CompletedAt)
		}
	}
	for _, node := range record.nodes {
		advance(node.RecordedAt)
		if node.StartedAt != nil {
			advance(*node.StartedAt)
		}
		if node.CompletedAt != nil {
			advance(*node.CompletedAt)
		}
	}
	for _, item := range record.items {
		advance(item.CreatedAt)
		if item.ClaimedAt != nil {
			advance(*item.ClaimedAt)
		}
		if item.CompletedAt != nil {
			advance(*item.CompletedAt)
		}
	}
	for _, transition := range record.transitions {
		advance(transition.At)
	}
	if record.ledger != nil {
		advance(record.ledger.RecordedAt)
	}
	summary.UpdatedAt = latest
}

// resimulate re-runs the read-only simulation for one intent and returns the
// artifact. A refusal is reported as an empty artifact rather than an error:
// a journey whose simulation no longer produces an executable plan is a
// BLOCKED row on the list, not a failed page.
func (e *journeyEngine) resimulate(ctx context.Context, intentID string) (*intentsv1.SimulationArtifact, error) {
	simulated, err := e.resimulateDetailedWithRelationships(ctx, intentID, nil)
	if err != nil {
		return nil, err
	}
	return simulated.Artifact, nil
}

func (e *journeyEngine) resimulateDetailed(ctx context.Context, intentID string) (simulationResult, error) {
	return e.resimulateDetailedWithRelationships(ctx, intentID, nil)
}

func (e *journeyEngine) resimulateWithRelationships(
	ctx context.Context, intentID string, relationships []authz.RelationshipFact,
) (*intentsv1.SimulationArtifact, error) {
	simulated, err := e.resimulateDetailedWithRelationships(ctx, intentID, relationships)
	if err != nil {
		return nil, err
	}
	return simulated.Artifact, nil
}

func (e *journeyEngine) resimulateDetailedWithRelationships(
	ctx context.Context, intentID string, relationships []authz.RelationshipFact,
) (simulationResult, error) {
	principal, inv, err := caller(ctx)
	if err != nil {
		return simulationResult{}, journeyError(err)
	}
	inst, _, ownedErr := e.svc.loadInstance(ctx, principal.Tenant().String(), intentID)
	if ownedErr != nil {
		return simulationResult{}, journeyError(ownedErr)
	}
	def, resolveErr := e.svc.defs.Resolve(inst.Definition)
	if resolveErr != nil {
		return simulationResult{}, journeyError(resolveErr)
	}
	simulated, simErr := e.svc.simulateDetailedWithRelationships(ctx, principal, purposeOf(principal, inv), inst, def, relationships)
	if simErr != nil {
		return simulationResult{}, journeyError(simErr)
	}
	return simulated, nil
}

// ---------------------------------------------------------------------------
// Propose
// ---------------------------------------------------------------------------

// Propose implements [workspace.JourneyEngine].
//
// The manager supplies the target placement, the proposed base, the effective
// date and the business reason, and nothing else. The worker's current
// placement comes from the governed worker read (the same
// explain_worker_state capability invocation the workspace's read surface
// runs, through the same gateway, under the same authorization decision), and
// the declared compensation baseline, the annualization and the budget
// authority come from the corpus internal/domains/promotion itself certifies
// -- never from the form.
func (e *journeyEngine) Propose(ctx context.Context, in workspace.ProposalInput) (workspace.JourneySummary, error) {
	principal, err := journeyPrincipal(ctx)
	if err != nil {
		return workspace.JourneySummary{}, err
	}
	if err := validateProposalInput(in); err != nil {
		return workspace.JourneySummary{}, err
	}
	// The reference is resolved against both populations -- the fixed corpus
	// and this tenant's own created workers -- because a journey has to be
	// proposable for an employee somebody just made, not only for the four
	// this release ships with.
	subject, resolved, err := e.locate(ctx, principal.Tenant(), in.WorkerRef)
	if err != nil {
		return workspace.JourneySummary{}, err
	}
	if !resolved {
		return workspace.JourneySummary{}, journeyInputError("worker_ref", "no such worker in this workforce")
	}
	worker := subject.Ref

	baseline, err := journeyBaseline(in, subject)
	if err != nil {
		trace.SpanFromContext(ctx).AddEvent("promotion.baseline.unavailable")
		return workspace.JourneySummary{}, err
	}
	if subject.Created != nil {
		trace.SpanFromContext(ctx).AddEvent("promotion.baseline.durable_worker")
	} else {
		trace.SpanFromContext(ctx).AddEvent("promotion.baseline.declared_reference")
	}
	// WF-RUN-034: a worker whose earlier promotion has committed is proposed
	// from the pay the aggregates now record, not the pay journey_worker
	// froze when they were created. The placement half of the same fact
	// reaches currentPlacement through the governed read's own overlay.
	if subject.Created != nil {
		committed, found, committedErr := e.committedPay(ctx, principal, subject.Created.WorkerID.String())
		if committedErr != nil {
			return workspace.JourneySummary{}, committedErr
		}
		if found {
			baseline.currentBase, baseline.currency = committed.BasePay, committed.Currency
		}
	}
	current, err := e.currentPlacement(ctx, principal, worker, baseline.effective)
	if err != nil {
		return workspace.JourneySummary{}, err
	}
	if err := validatePublishedPromotionPath(current, in, baseline); err != nil {
		return workspace.JourneySummary{}, err
	}
	// A proposal that names no position is given the catalog vacancy when
	// the tenant's catalog records one; without a catalog the proposal
	// stays position-less and the run closes BLOCKED exactly as before.
	if strings.TrimSpace(in.TargetPositionID) == "" {
		selected, selectErr := e.selectTargetPosition(ctx, principal, current.orgUnit, strings.TrimSpace(in.TargetJobCode), strings.TrimSpace(in.TargetGrade), baseline.effective)
		if selectErr != nil {
			return workspace.JourneySummary{}, selectErr
		}
		in.TargetPositionID = selected
	}

	def, ownedErr := e.svc.defs.Resolve(intent.Ref{TypeID: promotion.IntentType, Version: 1})
	if ownedErr != nil {
		return workspace.JourneySummary{}, journeyError(envelope.New(envelope.CodeNotFound, reasonDefinitionUnknown,
			"the resource does not exist or is not visible").WithDiagnostic(ownedErr))
	}
	payload, err := journeyRequestPayload(in, subject.Key, current, baseline)
	if err != nil {
		return workspace.JourneySummary{}, err
	}
	key, keyErr := e.svc.ids()
	if keyErr != nil {
		return workspace.JourneySummary{}, fmt.Errorf("app: journey: mint an idempotency key: %w", keyErr)
	}
	proposeIdempotencyKey := "journey:propose:" + key

	// PROMOUX-002: admitted before CreateIntent is ever reached, so a
	// conflicting caller's refusal leaves no intent, proposal revision, work
	// item or ledger row behind. See admitPromotionWindow's own doc for the
	// guarantee this relies on. PROMOUX-017: the same helper abandons the
	// reservation this call opened when CreateIntent refuses it, so a
	// refused proposal leaves no orphan window either.
	var created *intentsv1.CreateIntentResponse
	if _, err := e.createGuardedIntent(ctx, principal, worker.String(), baseline.effectiveText, proposeIdempotencyKey,
		func() (string, error) {
			res, err := e.svc.CreateIntent(ctx, &intentsv1.CreateIntentRequest{
				IdempotencyKey: proposeIdempotencyKey,
				Definition:     &intentsv1.DefinitionReference{IntentTypeId: def.Ref.TypeID, Version: def.Ref.Version},
				// The initiator is server-derived. On the RPC surfaces the transport's
				// own trusted-field pass fills it from the verified credential
				// (internal/transport.ApplyTrustedContext); an in-process caller has
				// no such pass, so this fills it from the same principal rather than
				// leaving CreateIntent to refuse a request with no initiator at all.
				Initiator: &intentsv1.PrincipalReference{
					PrincipalId:          principal.Subject(),
					Kind:                 journeyInitiatorKind(principal.SubjectKind()),
					IdentityAssuranceRef: principal.EvidenceID(),
				},
				// The POSITION subject is declared only when the form actually named
				// one: an empty SubjectId is a structurally invalid reference, not
				// an unnamed position (PROMOUX-004 made target_position_id
				// optional; see validateProposalInput and
				// internal/intent/definitions.definitions.go's target_position_ref).
				Subjects: journeySubjects(worker.Id, in.TargetPositionID),
				Request: &intentsv1.TypedPayload{
					Schema: &intentsv1.SchemaReference{
						SchemaId:         def.InputSchema.SchemaID,
						Version:          def.InputSchema.Version,
						ProtobufFullName: def.InputSchema.ProtobufFullName,
					},
					ProtobufWireBytes: payload,
				},
				ExecutionMode: intentsv1.ExecutionMode_EXECUTION_MODE_SIMULATE,
			})
			if err != nil {
				return "", err
			}
			created = res
			return res.GetIntent().GetIntentId(), nil
		}); err != nil {
		return workspace.JourneySummary{}, err
	}

	summary, sumErr := journeySummaryFromProto(created.GetIntent())
	if sumErr != nil {
		return workspace.JourneySummary{}, sumErr
	}
	simulated, simErr := e.resimulateDetailed(ctx, summary.IntentID)
	if simErr != nil {
		return workspace.JourneySummary{}, simErr
	}
	artifact := simulated.Artifact
	// Same durable candidates the typed ProposePromotion records: the page
	// form and the typed contract are one capability, so the snapshot,
	// proposal revision and simulation result persist identically.
	stored, convErr := protomap.InstanceFromProto(created.GetIntent())
	if convErr != nil {
		return workspace.JourneySummary{}, convErr
	}
	if persistErr := e.recordProposalCandidates(ctx, principal, stored, simulated); persistErr != nil {
		return workspace.JourneySummary{}, persistErr
	}
	summary.ProposalRevisionID = artifact.GetProposalRevisionId()
	summary.MaterialDigest = artifact.GetMaterialProposalDigest().GetDigest()
	summary.Stage = workspace.JourneyStageProposed
	if summary.ProposalRevisionID == "" {
		summary.Stage = workspace.JourneyStageBlocked
	}
	summary.Viewer = journeyViewerProjection(summary.Stage,
		isJourneyInitiator(created.GetIntent().GetInitiator().GetPrincipalId(), principal.Subject()), nil)
	return summary, nil
}

// displayPositionID renders a stored position reference as the position it
// names. The proposal binds the picker-issued revision reference durably,
// which is right: it pins the exact revision the decision was made against.
// It is not, however, a thing a reader can read -- printed whole it is a
// 299-character token that carried the comparison table's Change column off
// the screen (UXLIVE-003). A value the decoder does not understand is
// returned unchanged, because the page's job is to show what the intent
// carries, not to hide it.
func displayPositionID(stored string) string {
	trimmed := strings.TrimSpace(stored)
	if trimmed == "" {
		return trimmed
	}
	selected, _, err := position.RevisionRef(trimmed).Decode()
	if err != nil {
		return trimmed
	}
	return selected.Id
}

// journeySubjects declares the intent's subjects: the worker always, and a
// POSITION subject only when the form named one. An empty SubjectId is a
// structurally invalid reference (CreateIntent refuses it), not "no
// position" -- so a job/grade-only promotion, which checkPlacement already
// accepts on its own, declares only the worker subject.
func journeySubjects(workerID, targetPositionID string) []*intentsv1.SubjectReference {
	subjects := []*intentsv1.SubjectReference{
		{SubjectKind: "EMPLOYMENT", SubjectId: workerID, AuthorityDomain: "PEOPLE"},
	}
	if strings.TrimSpace(targetPositionID) != "" {
		// The proposal payload binds the exact picker-issued revision, while
		// the intent's material subject names the underlying position entity.
		// RevisionRef's wire separator is not canonical subject-id text.
		positionID := targetPositionID
		if selected, _, err := position.RevisionRef(targetPositionID).Decode(); err == nil {
			positionID = selected.Id
		}
		subjects = append(subjects, &intentsv1.SubjectReference{
			SubjectKind: "POSITION", SubjectId: positionID, AuthorityDomain: "POSITION",
		})
	}
	return subjects
}

// validateProposalInput refuses a form the engine cannot turn into a governed
// promotion request, naming the field that has to change.
//
// target_position_id is deliberately not in this required list.
// PROMOUX-004 makes a non-empty value here mean something specific -- a
// reference the real Position domain checks -- and no picker exists yet on
// this form to issue one. A field that was mandatory but never validated is
// a worse contract than one that is optional and validated.
func validateProposalInput(in workspace.ProposalInput) error {
	for _, field := range []struct{ name, value string }{
		{"worker_ref", in.WorkerRef},
		{"target_job_code", in.TargetJobCode},
		{"target_grade", in.TargetGrade},
		{"proposed_base", in.ProposedBase},
		{"effective_date", in.EffectiveDate},
		{"business_reason", in.BusinessReason},
	} {
		if strings.TrimSpace(field.value) == "" {
			return journeyInputError(field.name, "is required")
		}
	}
	if _, err := values.ParseLocalDate(strings.TrimSpace(in.EffectiveDate)); err != nil {
		return journeyInputError("effective_date", "is not an ISO-8601 date (YYYY-MM-DD)")
	}
	return nil
}

// validatePublishedPromotionPath is the server-side ladder gate. Client
// filtering is guidance, never authority: every caller, including the direct
// intent-only RPC, must prove that the current and target profiles form a
// published edge and that the proposed base follows that edge's exact rule.
// PROMOUX-015: the edges are [publishedPromotionPaths], the same list
// ListWorkers publishes.
func validatePublishedPromotionPath(current journeyCurrent, in workspace.ProposalInput, baseline journeyBaselineFacts) error {
	paths, err := publishedPromotionPaths()
	if err != nil {
		return err
	}
	for _, path := range paths {
		if !path.matches(current.jobCode, current.grade, strings.TrimSpace(in.TargetJobCode), strings.TrimSpace(in.TargetGrade)) {
			continue
		}
		if path.bounds != nil {
			return validatePublishedBaseIncrease(in.ProposedBase, baseline, path.bounds.Path.MinimumBaseIncrease, path.bounds.Path.MaximumBaseIncrease)
		}
		if current.orgUnit == "" {
			// Catalog-level eligibility checks have no worker organization;
			// enforce the edge's published UPWARD rule without inventing one.
			currentPay, payErr := values.NewMoney(baseline.currentBase, baseline.currency, fixtures.MoneyScale, fixtures.MoneyRounding)
			if payErr != nil {
				return payErr
			}
			proposedPay, payErr := values.NewMoney(strings.TrimSpace(in.ProposedBase), baseline.currency, fixtures.MoneyScale, values.RoundingExactRequired)
			if payErr != nil {
				return journeyInputError("proposed_base", "must be an exact monetary amount")
			}
			if proposedPay.Amount().Cmp(currentPay.Amount()) < 0 {
				return journeyInputError("proposed_base", "an upward ladder edge does not accept a base below the current base")
			}
			return nil
		}
		break // Worker proposals are checked against their demo ladder bounds below.
	}
	// The workforce options publish the demo company's deterministic ladder in
	// addition to the fixed conformance corpus. Admission must read that same
	// source, or the UI offers a next role the server always rejects.
	for _, edge := range demoworkforce.PromotionPaths() {
		if edge.OrgUnit != current.orgUnit || edge.SourceJobCode != current.jobCode ||
			edge.SourceGrade != current.grade || edge.TargetJobCode != strings.TrimSpace(in.TargetJobCode) ||
			edge.TargetGrade != strings.TrimSpace(in.TargetGrade) {
			continue
		}
		minimum, err := values.NewPercentage(edge.MinimumBaseIncrease, fixtures.PercentScale, values.RoundingExactRequired)
		if err != nil {
			return fmt.Errorf("app: journey: invalid demo ladder minimum: %w", err)
		}
		maximum, err := values.NewPercentage(edge.MaximumBaseIncrease, fixtures.PercentScale, values.RoundingExactRequired)
		if err != nil {
			return fmt.Errorf("app: journey: invalid demo ladder maximum: %w", err)
		}
		return validatePublishedBaseIncrease(in.ProposedBase, baseline, minimum, maximum)
	}
	return journeyInputError("target_job_code", "the target job and grade are not a published next step from the worker's current profile")
}

// validatePublishedBaseIncrease compares exact decimal money differences with
// exact fractional thresholds. Dividing and rounding a ratio first can admit
// a cent just outside the published bound; the threshold multiplication keeps
// the admission predicate identical at every penny boundary.
func validatePublishedBaseIncrease(proposedBase string, baseline journeyBaselineFacts, minimum, maximum values.Percentage) error {
	currentPay, err := values.NewMoney(baseline.currentBase, baseline.currency, fixtures.MoneyScale, fixtures.MoneyRounding)
	if err != nil {
		return fmt.Errorf("app: journey: parse current base for ladder rule: %w", err)
	}
	proposedPay, err := values.NewMoney(strings.TrimSpace(proposedBase), baseline.currency, fixtures.MoneyScale, values.RoundingExactRequired)
	if err != nil {
		return journeyInputErrorReason("proposed_base", "promotion.base_pay.not_exact", "must be an exact monetary amount in the worker's currency")
	}
	minimumPay, maximumPay, err := promotion.BasePayBounds(currentPay, minimum, maximum)
	if err != nil {
		return fmt.Errorf("app: journey: calculate exact ladder bounds: %w", err)
	}
	if proposedPay.Amount().Cmp(minimumPay.Amount()) < 0 || proposedPay.Amount().Cmp(maximumPay.Amount()) > 0 {
		return &workspace.JourneyInputError{
			FieldPath: "proposed_base", ReasonRef: "promotion.ladder.base_increase_out_of_range",
			Detail:   "the published ladder edge requires a base increase between " + minimum.String() + " and " + maximum.String() + " (decimal fractions)",
			PayRange: &workspace.JourneyPayRange{Minimum: minimumPay, Maximum: maximumPay},
		}
	}
	return nil
}

// journeyBaseline is the declared, corpus-sourced half of a promotion request:
// everything the manager does not supply and the governed worker read does not
// master.
//
// P1A masters no compensation projection -- internal/domains/rewards is handed
// a baseline, it does not fetch one -- so the current pay side, the bonus
// target and the budget authority come from the same ported legacy corpus
// internal/humanwork/workspace's own read surface seeds from
// (workspace.DefaultQuery). Deriving them here rather than accepting them off
// the form is what keeps "the engine reads the baseline, the form proposes the
// change" true on this surface too.
type journeyBaselineFacts struct {
	currentBase    string
	currency       string
	bonusTarget    string
	revisionStream string
	revisionSeq    string
	budgetAvailabe string
	evaluationDate string
	// knownAt is the knowledge cut-off the governed worker read is taken at,
	// as an ISO-8601 date. It is empty for a corpus worker, whose knowledge
	// coordinate is the ported scenario's own evaluation date; a created
	// worker carries the instant its record was known at instead, because
	// that is when this cell actually learned the facts being read.
	knownAt       string
	effective     values.LocalDate
	effectiveText string
}

// knownAtDate is the knowledge cut-off the request payload declares: the
// created worker's own, or the corpus evaluation date.
func (b journeyBaselineFacts) knownAtDate() string {
	if b.knownAt != "" {
		return b.knownAt
	}
	return b.evaluationDate
}

// The legacy scenario describes Omar only; Jane has separate declared reference
// simulation inputs. Neither baseline may be borrowed by any other worker.
// When the resolved worker carries its own durable record, the
// pay side of the baseline is read from that record instead -- its base pay,
// its currency, its bonus target and the instant it was known at.
//
// The budget authority still comes from the corpus either way. It is a
// finance-side fact about the org unit, not about the worker, and this cell
// masters no budget of its own to read it from.
func journeyBaseline(in workspace.ProposalInput, subject WorkerLocation) (journeyBaselineFacts, error) {
	set, err := fixtures.LegacyScenarios()
	if err != nil {
		return journeyBaselineFacts{}, fmt.Errorf("app: journey: read the promotion corpus: %w", err)
	}
	if len(set.Scenarios) == 0 {
		return journeyBaselineFacts{}, fmt.Errorf("app: journey: the promotion corpus declares no scenario")
	}
	s := set.Scenarios[0]
	effectiveText := strings.TrimSpace(in.EffectiveDate)
	effective, parseErr := values.ParseLocalDate(effectiveText)
	if parseErr != nil {
		return journeyBaselineFacts{}, journeyInputError("effective_date", parseErr.Error())
	}
	facts := journeyBaselineFacts{
		currentBase:    s.CurrentAmount,
		currency:       s.CurrentCurrency,
		bonusTarget:    s.BonusTarget,
		budgetAvailabe: set.BudgetAvailable,
		evaluationDate: s.EvaluationAt,
		effective:      effective,
		effectiveText:  effectiveText,
	}
	if created := subject.Created; created != nil {
		facts.currentBase = created.BasePay
		facts.currency = created.Currency
		facts.bonusTarget = created.BonusTarget
		facts.revisionStream = created.RevisionStream
		facts.revisionSeq = fmt.Sprint(created.RevisionSequence)
		facts.knownAt = created.KnownAt.UTC().Format(time.DateOnly)
	} else if subject.Key == "jane-doe" {
		facts.currentBase = fixtures.JanePromotionBase
		facts.currency = "USD"
		facts.bonusTarget = fixtures.JanePromotionBonus
		facts.evaluationDate = "2026-09-03"
	} else if subject.Key != set.Worker {
		return journeyBaselineFacts{}, journeyInputError("worker_ref", "this worker has no declared compensation baseline; connect a compensation record before proposing a promotion")
	}
	return facts, nil
}

// journeyCurrent is the worker's placement as the governed read discloses it.
type journeyCurrent struct {
	worker     values.EntityRef
	name       string
	jobCode    string
	grade      string
	orgUnit    string
	positionID string
	payZone    string
}

// currentPlacement runs the governed worker read and projects exactly the
// placement fields a promotion request needs onto plain strings.
//
// It reads through the same capability gateway invocation the workspace's own
// read surface uses ([invokeExplain]), so the placement a proposal is built
// from is authorized, effect-class checked and evidenced like every other
// answer this cell gives.
func (e *journeyEngine) currentPlacement(
	ctx context.Context, principal *trust.Principal, worker values.EntityRef, effective values.LocalDate,
) (journeyCurrent, error) {
	asOf, err := workspaceAsOf(effective, values.NewInstant(e.now()))
	if err != nil {
		return journeyCurrent{}, err
	}
	fields := workspaceFields(promotion.RequiredWorkerFields())
	purpose := principal.DefaultPurpose()
	decision, authErr := authorizeRead(principal, purpose, authorizationRequest{
		Subject:       worker,
		EvaluatedAt:   values.NewInstant(e.now()),
		Read:          peopleFields(fields),
		Relationships: managerChainFacts(ctx, e.locate, principal, worker),
	})
	if authErr != nil {
		return journeyCurrent{}, workspaceDenial(authErr)
	}
	explanation, _, ownedErr := invokeExplain(ctx, e.svc, principal, purpose, people.ExplainWorkerStateRequest{
		Tenant:        worker.Tenant,
		Worker:        worker,
		AsOf:          asOf,
		Fields:        fields,
		Authorization: peopleDecision(decision, fields),
	})
	if ownedErr != nil {
		return journeyCurrent{}, ownedErr
	}
	switch {
	case explanation.Disclosure == people.DisclosureWithheld:
		return journeyCurrent{}, fmt.Errorf("%w: %s", workspace.ErrDenied, explanation.WithheldReason)
	case explanation.Presence == people.SubjectAbsent:
		return journeyCurrent{}, journeyInputError("worker_ref", "the governed read discloses no such worker")
	}

	disclosed := map[people.FieldID]string{}
	for _, fact := range explanation.AuthorizedFields() {
		if v, ok := fact.Value.Get(); ok {
			disclosed[fact.Field] = v
		}
	}
	current := journeyCurrent{
		worker:     worker,
		name:       disclosed[people.FieldPreferredName],
		jobCode:    disclosed[people.FieldJobCode],
		grade:      disclosed[people.FieldGrade],
		orgUnit:    disclosed[people.FieldOrgUnit],
		positionID: disclosed[people.FieldPositionID],
		payZone:    disclosed[people.FieldPayZone],
	}
	if current.name == "" {
		current.name = disclosed[people.FieldLegalName]
	}
	if current.orgUnit == "" || current.payZone == "" {
		return journeyCurrent{}, journeyInputError("worker_ref",
			"the governed worker read discloses no organizational placement to promote from")
	}
	return current, nil
}

// journeyRequestPayload builds the promote_worker request payload: the same
// google.protobuf.Struct shape [decodeStruct] and the domain-input resolver
// already read, assembled from the governed read, the corpus baseline and the
// manager's own four fields.
//
// It is built as JSON and decoded through the one approved Protobuf seam
// ([protomap.Struct]) rather than assembled from Protobuf value types
// directly, because internal/intent/app is not on LIB-003's allowed-import
// roots for google.golang.org/protobuf.
func journeyRequestPayload(
	in workspace.ProposalInput, workerKey string, current journeyCurrent, baseline journeyBaselineFacts,
	extras ...promotionRequestExtras,
) ([]byte, error) {
	var extra promotionRequestExtras
	if len(extras) > 0 {
		extra = extras[0]
	}
	stream := baseline.revisionStream
	sequence := baseline.revisionSeq
	if stream == "" {
		stream = promotionRevisionStream(workerKey)
		sequence = promotionRevisionSequence
	}
	side := func(base string) map[string]any {
		return map[string]any{
			"base":              base,
			"currency":          baseline.currency,
			"pay_basis":         "ANNUAL_SALARY",
			"bonus_target":      baseline.bonusTarget,
			"effective_date":    baseline.effectiveText,
			"revision_stream":   stream,
			"revision_sequence": sequence,
		}
	}
	fields := map[string]any{
		// worker_ref is the canonical key the journey's own worker list shows,
		// not whatever spelling the form used: a request that named a worker
		// by a raw entity id would list under that id forever.
		"worker_ref":  workerKey,
		"worker_name": current.name,
		"known_at":    baseline.knownAtDate(),
		// current_placement is a display fact, not an input: the resolver
		// reads the worker's real placement itself through the governed read
		// and never looks at this object (it decodes only the named paths it
		// needs). It is recorded in the request so a journey list can show
		// what the promotion moves the worker away from without re-running one
		// governed read per row.
		"current_placement": map[string]any{
			"job_code":    current.jobCode,
			"grade":       current.grade,
			"org_unit":    current.orgUnit,
			"position_id": current.positionID,
			"pay_zone":    current.payZone,
		},
		"effective_date":  baseline.effectiveText,
		"evaluation_date": baseline.evaluationDate,
		"business_reason": strings.TrimSpace(in.BusinessReason),
		"target": map[string]any{
			"job_code":    strings.TrimSpace(in.TargetJobCode),
			"grade":       strings.TrimSpace(in.TargetGrade),
			"org_unit":    current.orgUnit,
			"position_id": strings.TrimSpace(in.TargetPositionID),
			"pay_zone":    current.payZone,
		},
		"current":  side(baseline.currentBase),
		"proposed": side(strings.TrimSpace(in.ProposedBase)),
		"budget": map[string]any{
			"available_amount": baseline.budgetAvailabe,
			"currency":         baseline.currency,
			"owner_system":     "finance.incumbent.erp",
			"policy_ref":       "finance.budget_authority/2026.1",
			"scope":            "cost-center:" + current.orgUnit,
			"period":           "FY2026",
			"baseline_version": "finance.budget.baseline/2026.09",
			"observation_id":   "obs_budget_" + journeySanitize(current.orgUnit),
		},
	}
	// The intent-only contract (PROMO-007) can say three things the page form
	// cannot, and each is written only when it was actually said: a desired
	// organizational unit (empty means "where the subject is today", which is
	// what the governed read already supplied above), a desired manager, and
	// the subject revision the proposal was formed against. They are appended
	// rather than defaulted so that the payload of a request that said nothing
	// about them is byte-for-byte the payload the page form has always
	// produced, and its digest therefore unchanged.
	if target, ok := fields["target"].(map[string]any); ok {
		if extra.targetOrgUnit != "" {
			target["org_unit"] = extra.targetOrgUnit
		}
		if extra.managerRef != "" {
			target["manager_ref"] = extra.managerRef
		}
	}
	if extra.subjectRevision != "" {
		fields["expected_subject_revision"] = extra.subjectRevision
	}

	raw, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("app: journey: encode the promotion request: %w", err)
	}
	var payload protomap.Struct
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("app: journey: encode the promotion request payload: %w", err)
	}
	wire, err := protomap.MarshalDeterministic(&payload)
	if err != nil {
		return nil, fmt.Errorf("app: journey: marshal the promotion request payload: %w", err)
	}
	return wire, nil
}

// journeySanitize reduces a reference to the characters a revision stream and
// an observation identifier accept, the same way the workspace's own query
// does.
func journeySanitize(ref string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(ref)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	if b.Len() == 0 {
		return "worker"
	}
	return b.String()
}

// journeyInitiatorKind maps the authenticated actor kind onto the wire
// initiator kind, the same way internal/transport's own trusted-field pass
// does for an RPC caller.
func journeyInitiatorKind(k trust.SubjectKind) intentsv1.InitiatorKind {
	switch k {
	case trust.SubjectKindHuman:
		return intentsv1.InitiatorKind_INITIATOR_KIND_HUMAN
	case trust.SubjectKindService:
		return intentsv1.InitiatorKind_INITIATOR_KIND_SERVICE
	case trust.SubjectKindAgent:
		return intentsv1.InitiatorKind_INITIATOR_KIND_AGENT
	case trust.SubjectKindIntegration:
		return intentsv1.InitiatorKind_INITIATOR_KIND_INTEGRATION
	default:
		return intentsv1.InitiatorKind_INITIATOR_KIND_UNSPECIFIED
	}
}

// ---------------------------------------------------------------------------
// Summary projection
// ---------------------------------------------------------------------------

// journeySummaryFromProto derives one journey summary from the stored intent
// alone: identity, correlation, the worker, both sides of the placement
// change, both pay figures, the effective date and the business reason. The
// stage and the execution identifiers are added by the caller, from durable
// execution state.
func journeySummaryFromProto(msg *intentsv1.IntentInstance) (workspace.JourneySummary, error) {
	if msg == nil {
		return workspace.JourneySummary{}, fmt.Errorf("app: journey: no intent")
	}
	tenant := values.TenantId(msg.GetTenantId())
	if err := tenant.Validate(); err != nil {
		return workspace.JourneySummary{}, fmt.Errorf("app: journey: invalid stored tenant: %w", err)
	}
	payload, err := decodeStruct(msg.GetRequest().GetProtobufWireBytes())
	if err != nil {
		return workspace.JourneySummary{}, fmt.Errorf("app: journey: %w", err)
	}
	summary := workspace.JourneySummary{
		IntentID:       msg.GetIntentId(),
		CorrelationID:  msg.GetCorrelationId(),
		EffectiveDate:  optionalStr(payload, "effective_date"),
		BusinessReason: optionalStr(payload, "business_reason"),
	}
	if worker, ok := workspaceWorker(optionalStr(payload, "worker_ref")); ok {
		// The corpus lookup can supply an ID, never the authority tenant. A
		// sandbox or imported tenant must retain the intent's stored scope.
		worker.Tenant = tenant
		summary.Worker = worker
	} else {
		// A created worker's key is not a well-formed entity identifier, so
		// the corpus resolver cannot render it. The subject loop below fills
		// the authoritative id in from the intent's own EMPLOYMENT subject;
		// this only supplies the tenant and kind so the reference is
		// well-formed once it does.
		summary.Worker = values.EntityRef{Tenant: tenant, Kind: people.KindWorker}
	}
	summary.WorkerName = optionalStr(payload, "worker_name")
	if summary.WorkerName == "" {
		summary.WorkerName = optionalStr(payload, "worker_ref")
	}
	for _, subj := range msg.GetSubjects() {
		if subj.GetSubjectKind() == "EMPLOYMENT" && subj.GetSubjectId() != "" {
			summary.Worker.Id = subj.GetSubjectId()
		}
	}
	if target, targetErr := fieldsOf(payload, "target"); targetErr == nil {
		summary.Target = workspace.JourneyPlacement{
			JobCode:    optionalStr(target, "job_code"),
			Grade:      optionalStr(target, "grade"),
			PositionID: displayPositionID(optionalStr(target, "position_id")),
			OrgUnit:    optionalStr(target, "org_unit"),
			PayZone:    optionalStr(target, "pay_zone"),
		}
		// The current placement's organizational half is the target's: a
		// promotion moves job and grade, not the org unit or the pay zone the
		// governed read supplied for both sides.
		summary.Current.OrgUnit = summary.Target.OrgUnit
		summary.Current.PayZone = summary.Target.PayZone
	}
	if placement, placementErr := fieldsOf(payload, "current_placement"); placementErr == nil {
		summary.Current = workspace.JourneyPlacement{
			JobCode:    optionalStr(placement, "job_code"),
			Grade:      optionalStr(placement, "grade"),
			PositionID: displayPositionID(optionalStr(placement, "position_id")),
			OrgUnit:    optionalStr(placement, "org_unit"),
			PayZone:    optionalStr(placement, "pay_zone"),
		}
	}
	if currentPay, payErr := fieldsOf(payload, "current"); payErr == nil {
		summary.CurrentBase = optionalStr(currentPay, "base")
		summary.Currency = optionalStr(currentPay, "currency")
	}
	if proposedPay, payErr := fieldsOf(payload, "proposed"); payErr == nil {
		summary.ProposedBase = optionalStr(proposedPay, "base")
		if summary.Currency == "" {
			summary.Currency = optionalStr(proposedPay, "currency")
		}
	}
	if ts := msg.GetCreatedAt(); ts != nil {
		summary.CreatedAt = ts.AsTime().UTC()
	}
	if ts := msg.GetLastTransitionAt(); ts != nil {
		summary.UpdatedAt = ts.AsTime().UTC()
	}
	summary.GovernanceVersion = msg.GetInstanceVersion()
	return summary, nil
}
