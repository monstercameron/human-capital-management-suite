package journey

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/workerids"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Dependencies is the single port JourneyService forwards to, plus the one
// timing knob WatchJourney's change feed is driven by.
type Dependencies struct {
	// Engine is the live Promotion journey engine
	// (internal/humanwork/workspace.JourneyEngine, implemented by
	// internal/intent/app). Nil is a valid composition: every method then
	// returns a typed UNAVAILABLE rather than panicking, so a process can
	// host this service before the engine is wired - the same rule
	// internal/transport/admin applies to each of its optional ports.
	Engine workspace.JourneyEngine
	// Preferences is the authenticated product-workspace preference store.
	// The handler derives tenant and principal from trusted context before
	// forwarding, so the wire contract cannot select another user's record.
	Preferences preferences.Store
	RoleAccess  roleaccess.Store
	WorkerIDs   workerids.Store
	// PollInterval is how often WatchJourney re-reads the engine looking for
	// a change. Zero or negative means [defaultWatchPollInterval].
	//
	// It is a composition knob rather than a wire field on purpose: the
	// client does not get to choose how hard this service reads the engine.
	// It exists so a test can drive the change path in milliseconds instead
	// of sleeping through the production interval, and so an operator with a
	// heavier engine can slow the feed down without a schema change.
	PollInterval time.Duration
	// CursorKey signs the resumable stream cursor WatchJourney's response
	// carries (WatchJourneyResponse.cursor) and verifies the resume cursor a
	// caller may present on reconnect (WatchJourneyRequest.resume_cursor),
	// via internal/transport/streaming.Signer (PROTO-007).
	//
	// A key shorter than streaming.MinKeySize, including nil, leaves cursor
	// issuance and resume disabled rather than refusing every watch: the
	// response's cursor field is left empty and any resume_cursor a caller
	// presents is silently ignored. That is what lets a process composed
	// before an operator provisions a signing key keep answering exactly as
	// it did before this field existed, and it is safe precisely because no
	// cursor was ever issued for that key's absence to be forged against.
	CursorKey []byte
	// CursorTTL bounds how long a cursor WatchJourney issues stays
	// presentable. Zero or negative means [defaultCursorTTL]. It has no
	// effect when CursorKey leaves cursor issuance disabled.
	CursorTTL time.Duration
	// Now supplies the current time for cursor issuance and validation. Nil
	// means [time.Now]. It exists so a test can pin cursor expiry the same
	// way [PollInterval] lets it pin the feed's cadence.
	Now func() time.Time
}

// pollInterval returns the effective watch poll interval.
func (d Dependencies) pollInterval() time.Duration {
	if d.PollInterval > 0 {
		return d.PollInterval
	}
	return defaultWatchPollInterval
}

// cursorTTL returns the effective cursor lifetime.
func (d Dependencies) cursorTTL() time.Duration {
	if d.CursorTTL > 0 {
		return d.CursorTTL
	}
	return defaultCursorTTL
}

// nowFunc returns the effective clock, never nil.
func (d Dependencies) nowFunc() func() time.Time {
	if d.Now != nil {
		return d.Now
	}
	return time.Now
}

// server adapts [Dependencies] to the generated
// journeyv1.JourneyServiceServer interface. Every method is a thin forward:
// [trustedContext] runs first, the request is converted to the port's plain
// Go types, the port is called, and its answer or its refusal is converted
// back. No method here decides an HCM business rule, and none of them reads
// a database table.
type server struct {
	journeyv1.UnimplementedJourneyServiceServer

	deps Dependencies
}

// Register adds hcmnext.journey.v1.JourneyService to srv. srv must already
// carry the shared trusted-request interceptor chain (normally
// internal/transport/grpcserver.UnaryInterceptor, installed when srv was
// constructed via internal/transport/grpcserver.NewServer): Register itself
// installs no interceptor and performs no admission of its own, exactly like
// a second internal/transport/grpcserver.RegisterXServer call would.
//
// This is the hook the orchestrator wires into the modular application's
// composition root once the shared *grpc.Server exists; it is deliberately
// not called from internal/transport/cell by this change.
func Register(srv *grpc.Server, deps Dependencies) {
	journeyv1.RegisterJourneyServiceServer(srv, &server{deps: deps})
}

// trustedContext reads the already-admitted principal and invocation from
// ctx - populated by the shared interceptor chain before any handler here
// runs - and fails closed when either is missing. It never inspects
// transport metadata or headers directly: by the time a handler runs, the
// only trusted context that exists is what transport.Admit already resolved.
//
// Unlike internal/transport/admin's requireOperator there is no role
// predicate. The journey page is an ordinary authenticated surface; which
// journeys this principal may read, and whether it may propose, execute or
// decide, is the engine's decision, reported as workspace.ErrDenied.
func trustedContext(ctx context.Context) (*trust.Principal, *transport.Invocation, *envelope.Error) {
	inv, ok := transport.InvocationFromContext(ctx)
	if !ok {
		return nil, nil, envelope.New(envelope.CodeUnauthenticated,
			"journey.no_trusted_context",
			"the request carries no trusted context")
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil {
		return nil, inv, envelope.New(envelope.CodeUnauthenticated,
			"journey.no_principal",
			"the request carries no authenticated principal").
			WithCorrelation(inv.RequestID())
	}
	return principal, inv, nil
}

// evidence is the authentication evidence reference every owned failure this
// package raises carries, so a client can say which credential the refusal
// was decided for without this package inventing a second evidence
// mechanism alongside internal/trust.Principal.EvidenceID.
func evidence(principal *trust.Principal) envelope.Evidence {
	if principal == nil {
		return envelope.Evidence{}
	}
	return envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"}
}

// ownedError projects a workspace.JourneyEngine failure onto the repository's
// owned error model, so the journey surface's wire shape is the same one
// every other service produces (*envelope.Error implements grpc-go's status
// interface, so returning it yields the projected status code plus the
// canonical hcmnext.common.v1.ErrorDetail).
//
// The mapping is total and closed:
//
//	workspace.ErrDenied              -> PERMISSION_DENIED
//	workspace.ErrJourneyUnknown      -> NOT_FOUND
//	workspace.ErrJourneyStage        -> FAILED_PRECONDITION
//	workspace.ErrJourneyInput        -> INVALID_ARGUMENT, naming the field
//	workspace.ErrJourneyUnavailable  -> UNAVAILABLE
//	workspace.ErrJourneyActiveConflict -> ALREADY_EXISTS (PROMOUX-002)
//	anything else                    -> INTERNAL, with the original error
//	                                    kept only as the nested diagnostic
//
// An error the port already raised as an *envelope.Error is passed through
// unchanged rather than being re-coded: the engine is entitled to speak the
// owned model directly, and re-wrapping would lose its violations.
//
// The unclassified case is deliberately the last one. envelope.Coerce would
// have turned it into a retryable UNAVAILABLE, which tells a client to try
// again at something that is not transient; CodeUnspecified projects to
// codes.Internal, and the raw text stays in the nested diagnostic that no
// transport surface is allowed to render.
func ownedError(err error, principal *trust.Principal, inv *transport.Invocation, op string) *envelope.Error {
	if err == nil {
		return nil
	}
	if owned, ok := envelope.As(err); ok {
		return owned
	}

	var out *envelope.Error
	switch {
	case errors.Is(err, workspace.ErrDenied):
		out = envelope.New(envelope.CodePermissionDenied,
			"journey."+op+".denied",
			"the action is not permitted for this principal")
	case errors.Is(err, workspace.ErrJourneyUnknown):
		out = envelope.New(envelope.CodeNotFound,
			"journey."+op+".unknown",
			"the resource does not exist or is not visible")
	case errors.Is(err, workspace.ErrJourneyStage):
		out = envelope.New(envelope.CodeFailedPrecondition,
			"journey."+op+".stage",
			"the action is not available at this stage")
	case errors.Is(err, workspace.ErrJourneyInput):
		field, reason, payRange := inputRefusal(err)
		out = envelope.New(envelope.CodeInvalidArgument,
			"journey."+op+".input",
			"the request input is not acceptable").
			WithViolation(field, "review this field and try again", reason).
			WithDiagnostic(err)
		// Only the two promotion proposal operations may publish a corrected
		// salary range. Other journey calls can return ErrJourneyInput but do
		// not establish a pay-disclosure context.
		if payRange != nil && (op == "propose" || op == "propose_promotion") {
			out.WithViolationMoneyRange(payRange.Minimum, payRange.Maximum)
		}
	case errors.Is(err, workspace.ErrJourneyUnavailable):
		out = envelope.New(envelope.CodeUnavailable,
			"journey."+op+".engine_unavailable",
			"the journey engine is not composed on this cell")
	case errors.Is(err, workspace.ErrJourneyActiveConflict):
		out = envelope.New(envelope.CodeAlreadyExists,
			"journey."+op+".active_conflict",
			"an active promotion already claims this worker and effective window")
	default:
		out = envelope.New(envelope.CodeUnspecified,
			"journey."+op+".failed",
			"the request could not be completed").WithDiagnostic(err)
	}
	if inv != nil {
		out = out.WithCorrelation(inv.RequestID())
	}
	if principal != nil {
		out = out.WithEvidence(evidence(principal))
	}
	return out
}

// inputRefusal accepts only the typed port contract. Legacy sentinel-only
// failures remain request-level, never guessed from potentially sensitive prose.
func inputRefusal(err error) (field, reason string, payRange *workspace.JourneyPayRange) {
	var typed *workspace.JourneyInputError
	if errors.As(err, &typed) && typed != nil {
		// A typed port error is still an internal error, not a licence to
		// publish arbitrary strings. Keep both wire coordinates closed so a
		// future caller cannot accidentally expose a worker value or policy
		// detail by putting it in FieldPath or ReasonRef.
		if knownInputFields[typed.FieldPath] && knownInputReasons[typed.ReasonRef] {
			if typed.FieldPath == "proposed_base" && typed.ReasonRef == "promotion.ladder.base_increase_out_of_range" && typed.PayRange != nil {
				payRange = typed.PayRange
			}
			return typed.FieldPath, typed.ReasonRef, payRange
		}
	}
	return "request", "journey.input.invalid", nil
}

var knownInputFields = map[string]bool{
	"(request)": true, "worker_ref": true, "subject_worker_ref": true,
	"expected_subject_revision": true, "client_request_id": true,
	"target_job_code": true, "desired_job_code": true, "target_grade": true, "desired_grade": true,
	"position_id": true, "desired_position_id": true, "proposed_base": true, "desired_base_pay": true,
	"desired_pay_currency": true, "effective_date": true, "reason": true, "business_reason": true,
	"worker_key": true, "worker": true, "legal_name": true, "preferred_name": true,
	"org_unit": true, "pay_zone": true, "location": true, "job_code": true, "grade": true,
	"base_pay": true, "currency": true, "bonus_target": true, "hire_date": true,
}

var knownInputReasons = map[string]bool{
	"journey.input.invalid":                       true,
	"promotion.base_pay.not_exact":                true,
	"promotion.ladder.base_increase_out_of_range": true,
}

// engine returns the configured port, or a typed UNAVAILABLE when the
// service was composed without one.
func (s *server) engine(principal *trust.Principal, inv *transport.Invocation, op string) (workspace.JourneyEngine, *envelope.Error) {
	if s.deps.Engine == nil {
		return nil, envelope.New(envelope.CodeUnavailable,
			"journey."+op+".engine_unconfigured",
			"the journey engine is not configured").
			WithCorrelation(inv.RequestID()).
			WithEvidence(evidence(principal))
	}
	return s.deps.Engine, nil
}

// ListJourneys forwards to workspace.JourneyEngine.ListJourneys. READ_ONLY.
func (s *server) ListJourneys(ctx context.Context, _ *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	// PROMOUX-015: the journey list carries every visible promotion's current
	// and proposed pay. It backs the Journeys and My Work pages, so a caller
	// who may view neither -- a self-service employee -- is refused rather
	// than handed the tenant's promotions over the RPC the pages would hide.
	if err := s.requireAnyFeatureView(ctx, principal, inv,
		featureAccessRequest{pageID: "journeys", featureID: "journey_list"},
		featureAccessRequest{pageID: "work", featureID: "assigned_queue"},
	); err != nil {
		return nil, err
	}
	eng, depErr := s.engine(principal, inv, "list")
	if depErr != nil {
		return nil, depErr
	}

	summaries, err := eng.ListJourneys(ctx)
	if err != nil {
		return nil, ownedError(err, principal, inv, "list")
	}
	diagAuthorized := s.diagnosticsAuthorized(ctx, principal)
	resp := &journeyv1.ListJourneysResponse{}
	for _, summary := range summaries {
		resp.Journeys = append(resp.Journeys, toJourney(summary, diagAuthorized))
	}
	return resp, nil
}

// ProposeJourney forwards to workspace.JourneyEngine.Propose, which runs the
// engine's own CreateIntent followed by SimulateIntent. Governed write.
func (s *server) ProposeJourney(ctx context.Context, req *journeyv1.ProposeJourneyRequest) (*journeyv1.ProposeJourneyResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	if err := s.requireFeatureAction(ctx, principal, inv, "journeys", "promotion_request", roleaccess.ActionCreate); err != nil {
		return nil, err
	}
	eng, depErr := s.engine(principal, inv, "propose")
	if depErr != nil {
		return nil, depErr
	}

	target := fromPlacement(req.GetTarget())
	summary, err := eng.Propose(ctx, workspace.ProposalInput{
		WorkerRef:        req.GetWorkerRef(),
		TargetJobCode:    target.JobCode,
		TargetGrade:      target.Grade,
		TargetPositionID: target.PositionID,
		ProposedBase:     req.GetProposedBase(),
		EffectiveDate:    req.GetEffectiveDate(),
		BusinessReason:   req.GetBusinessReason(),
	})
	if err != nil {
		return nil, ownedError(err, principal, inv, "propose")
	}
	return &journeyv1.ProposeJourneyResponse{Journey: toJourney(summary, s.diagnosticsAuthorized(ctx, principal))}, nil
}

// InspectJourney forwards to workspace.JourneyEngine.Inspect. READ_ONLY.
func (s *server) InspectJourney(ctx context.Context, req *journeyv1.InspectJourneyRequest) (*journeyv1.InspectJourneyResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	if err := s.requireAnyFeatureView(ctx, principal, inv,
		featureAccessRequest{pageID: "journeys", featureID: "journey_detail"},
		featureAccessRequest{pageID: "work", featureID: "assigned_queue"},
	); err != nil {
		return nil, err
	}
	eng, depErr := s.engine(principal, inv, "inspect")
	if depErr != nil {
		return nil, depErr
	}

	detail, err := eng.Inspect(ctx, req.GetIntentId())
	if err != nil {
		return nil, ownedError(err, principal, inv, "inspect")
	}
	return &journeyv1.InspectJourneyResponse{Detail: toDetail(detail, s.diagnosticsAuthorized(ctx, principal))}, nil
}

// ExecuteJourney forwards to workspace.JourneyEngine.Execute, which runs
// ExecuteIntent behind the P1B execution authority gate. Governed write.
func (s *server) ExecuteJourney(ctx context.Context, req *journeyv1.ExecuteJourneyRequest) (*journeyv1.ExecuteJourneyResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	if err := s.requireFeatureAction(ctx, principal, inv, "journeys", "journey_detail", roleaccess.ActionUpdate); err != nil {
		return nil, err
	}
	eng, depErr := s.engine(principal, inv, "execute")
	if depErr != nil {
		return nil, depErr
	}

	detail, err := eng.Execute(ctx, req.GetIntentId())
	if err != nil {
		return nil, ownedError(err, principal, inv, "execute")
	}
	return &journeyv1.ExecuteJourneyResponse{Detail: toDetail(detail, s.diagnosticsAuthorized(ctx, principal))}, nil
}

// DecideJourney forwards to workspace.JourneyEngine.Decide, which claims and
// completes the approval WorkItem as the routed approver and resumes the
// driver. Governed write.
func (s *server) DecideJourney(ctx context.Context, req *journeyv1.DecideJourneyRequest) (*journeyv1.DecideJourneyResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	if err := s.requireFeatureAction(ctx, principal, inv, "work", "approval_decision", roleaccess.ActionUpdate); err != nil {
		return nil, err
	}
	eng, depErr := s.engine(principal, inv, "decide")
	if depErr != nil {
		return nil, depErr
	}

	detail, err := eng.Decide(ctx, req.GetIntentId(), workspace.Decision{
		Approve: req.GetApprove(),
		Reason:  req.GetReason(),
	})
	if err != nil {
		return nil, ownedError(err, principal, inv, "decide")
	}
	return &journeyv1.DecideJourneyResponse{Detail: toDetail(detail, s.diagnosticsAuthorized(ctx, principal))}, nil
}

// AcknowledgeJourney forwards to workspace.JourneyEngine.Acknowledge, which
// receives the employee's verified acknowledgement as a correlated signal and
// resumes the driver from the matched receipt. Governed write. The page gate
// matches ExecuteJourney (advancing the journey, not deciding an approval);
// the engine itself enforces the attester rules (cell admission, no
// self-attestation by the initiator, open wait required).
func (s *server) AcknowledgeJourney(ctx context.Context, req *journeyv1.AcknowledgeJourneyRequest) (*journeyv1.AcknowledgeJourneyResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	if err := s.requireFeatureAction(ctx, principal, inv, "journeys", "journey_detail", roleaccess.ActionUpdate); err != nil {
		return nil, err
	}
	eng, depErr := s.engine(principal, inv, "acknowledge")
	if depErr != nil {
		return nil, depErr
	}

	detail, err := eng.Acknowledge(ctx, req.GetIntentId(), workspace.Acknowledgement{
		EvidenceRef: req.GetEvidenceRef(),
		Note:        req.GetNote(),
	})
	if err != nil {
		return nil, ownedError(err, principal, inv, "acknowledge")
	}
	return &journeyv1.AcknowledgeJourneyResponse{Detail: toDetail(detail, s.diagnosticsAuthorized(ctx, principal))}, nil
}

// EditProposal forwards to workspace.JourneyEngine.EditProposal, which is
// SupersedeIntent scoped to journeys. Governed write.
func (s *server) EditProposal(ctx context.Context, req *journeyv1.EditProposalRequest) (*journeyv1.EditProposalResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	if err := s.requireFeatureAction(ctx, principal, inv, "journeys", "promotion_request", roleaccess.ActionUpdate); err != nil {
		return nil, err
	}
	eng, depErr := s.engine(principal, inv, "edit_proposal")
	if depErr != nil {
		return nil, depErr
	}

	target := fromPlacement(req.GetTarget())
	successor, superseded, err := eng.EditProposal(ctx, req.GetIntentId(), req.GetExpectedInstanceVersion(),
		req.GetIdempotencyKey(), req.GetReason(), workspace.EditProposalInput{
			TargetJobCode:    target.JobCode,
			TargetGrade:      target.Grade,
			TargetPositionID: target.PositionID,
			ProposedBase:     req.GetProposedBase(),
			EffectiveDate:    req.GetEffectiveDate(),
			BusinessReason:   req.GetBusinessReason(),
		})
	if err != nil {
		return nil, ownedError(err, principal, inv, "edit_proposal")
	}
	return &journeyv1.EditProposalResponse{
		Journey:            toJourney(successor, s.diagnosticsAuthorized(ctx, principal)),
		SupersededIntentId: superseded,
	}, nil
}

// PreviewJourneyIntervention forwards to
// workspace.JourneyEngine.PreviewIntervention. READ_ONLY.
func (s *server) PreviewJourneyIntervention(ctx context.Context, req *journeyv1.PreviewJourneyInterventionRequest) (*journeyv1.PreviewJourneyInterventionResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	if err := s.requireFeatureAction(ctx, principal, inv, "journeys", "journey_detail", roleaccess.ActionView); err != nil {
		return nil, err
	}
	eng, depErr := s.engine(principal, inv, "preview_intervention")
	if depErr != nil {
		return nil, depErr
	}

	preview, err := eng.PreviewIntervention(ctx, req.GetIntentId(), fromInterventionKind(req.GetKind()))
	if err != nil {
		return nil, ownedError(err, principal, inv, "preview_intervention")
	}
	return toInterventionPreview(preview), nil
}

// RequestJourneyIntervention forwards to
// workspace.JourneyEngine.RequestIntervention, which is CancelIntent scoped
// to journeys. Governed write.
func (s *server) RequestJourneyIntervention(ctx context.Context, req *journeyv1.RequestJourneyInterventionRequest) (*journeyv1.RequestJourneyInterventionResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	if err := s.requireFeatureAction(ctx, principal, inv, "journeys", "journey_detail", roleaccess.ActionUpdate); err != nil {
		return nil, err
	}
	eng, depErr := s.engine(principal, inv, "request_intervention")
	if depErr != nil {
		return nil, depErr
	}

	result, err := eng.RequestIntervention(ctx, req.GetIntentId(), workspace.JourneyInterventionRequest{
		Kind:                    fromInterventionKind(req.GetKind()),
		ExpectedInstanceVersion: req.GetExpectedInstanceVersion(),
		IdempotencyKey:          req.GetIdempotencyKey(),
		Reason:                  req.GetReason(),
	})
	if err != nil {
		return nil, ownedError(err, principal, inv, "request_intervention")
	}
	return &journeyv1.RequestJourneyInterventionResponse{
		Journey:             toJourney(result.Journey, s.diagnosticsAuthorized(ctx, principal)),
		Outcome:             toInterventionOutcome(result.Outcome),
		RetainedEvidenceRef: result.RetainedEvidenceRef,
	}, nil
}

// ListWorkers forwards to workspace.JourneyEngine.ListWorkers. READ_ONLY.
func (s *server) ListWorkers(ctx context.Context, _ *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	if err := s.requireFeatureAction(ctx, principal, inv, "people", "directory", roleaccess.ActionView); err != nil {
		return nil, err
	}
	eng, depErr := s.engine(principal, inv, "list_workers")
	if depErr != nil {
		return nil, depErr
	}

	workers, options, err := eng.ListWorkers(ctx)
	if err != nil {
		return nil, ownedError(err, principal, inv, "list_workers")
	}
	workers, options, err = s.visibleWorkforce(ctx, principal, workers, options)
	if err != nil {
		return nil, preferenceError(err, principal, inv.RequestID(), "load_organization_visibility")
	}
	resp := &journeyv1.ListWorkersResponse{Options: toWorkforceOptions(options)}
	for _, w := range workers {
		resp.Workers = append(resp.Workers, toWorker(w))
	}
	return resp, nil
}

// CreateWorker forwards to workspace.JourneyEngine.CreateWorker, which records
// one employee as a durable, append-only fact behind the same P1B execution
// authority ExecuteJourney runs behind. Governed demo-authority write.
func (s *server) CreateWorker(ctx context.Context, req *journeyv1.CreateWorkerRequest) (*journeyv1.CreateWorkerResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	if err := s.requireFeatureAction(ctx, principal, inv, "people", "directory", roleaccess.ActionCreate); err != nil {
		return nil, err
	}
	eng, depErr := s.engine(principal, inv, "create_worker")
	if depErr != nil {
		return nil, depErr
	}

	worker, err := eng.CreateWorker(ctx, fromCreateWorkerRequest(req))
	if err != nil {
		return nil, ownedError(err, principal, inv, "create_worker")
	}
	return &journeyv1.CreateWorkerResponse{Worker: toWorker(worker)}, nil
}
