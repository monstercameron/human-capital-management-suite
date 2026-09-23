package journey

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
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
	// Invalidations is the committed-transition hub
	// WatchPromotionInvalidations subscribes to (REV-091-03). Nil answers
	// that stream UNAVAILABLE.
	Invalidations InvalidationSource
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
	// PreviousCursorKey is the retired stream-cursor signing key, accepted
	// for resume verification only while a rotation is in progress. New
	// cursors are always minted under CursorKey.
	PreviousCursorKey []byte
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

	// roleMu guards roleCache, the server's short-TTL durable role cache
	// (RBAC-RT-002). It lives on the server value that owns it, never in
	// a package-level registry.
	roleMu    sync.Mutex
	roleCache *roleaccess.Resolver
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
	"body": true, "idempotency_key": true,
}

var knownInputReasons = map[string]bool{
	"journey.input.invalid":                       true,
	"promotion.base_pay.not_exact":                true,
	"promotion.ladder.base_increase_out_of_range": true,
	workspace.JourneyNoteReasonEmpty:              true,
	workspace.JourneyNoteReasonTooLong:            true,
	workspace.JourneyNoteReasonInvalidText:        true,
	workspace.JourneyNoteReasonKeyInvalid:         true,
	workspace.JourneyNoteReasonKeyReused:          true,
	workspace.JourneyNoteReasonLimit:              true,
	workspace.JourneyReasonNotProse:               true,
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

const (
	// fallbackJourneyPageSize and fallbackJourneyPageSizeMax bound the
	// local slice listJourneyPage takes for an engine that predates the
	// paging port. They mirror the live engine's own default and cap so a
	// test double pages like production; the engine itself stays the only
	// implementation that filters and sorts.
	fallbackJourneyPageSize    = 50
	fallbackJourneyPageSizeMax = 100
	// historyCursorRefusal matches the live engine's invalid-cursor
	// refusal, which carries no typed sentinel this package could match
	// with errors.Is (internal/intent/app owns that message). Matching it
	// here projects a forged or stale page cursor as INVALID_ARGUMENT
	// naming page.cursor instead of an INTERNAL error.
	historyCursorRefusal = "invalid history cursor"
)

// listJourneyPage resolves one ListJourneys page: the engine's page when it
// implements workspace.HistoryEngine, a local slice of
// workspace.JourneyEngine.ListJourneys when it does not.
//
// terminalOnly answers only journeys the engine's own viewer projection
// reports closed. The engine's resume cursor names an offset in its
// unfiltered query, so a cursor and terminal_only together name no page at
// all and are refused; terminal pages navigate by page number. The fallback
// path mints no cursors for the same reason: an opaque cursor it cannot
// honor is refused rather than silently answered as the first page.
func (s *server) listJourneyPage(ctx context.Context, eng workspace.JourneyEngine, principal *trust.Principal, inv *transport.Invocation, req *journeyv1.ListJourneysRequest) ([]workspace.JourneySummary, string, int, *envelope.Error) {
	if hist, ok := eng.(workspace.HistoryEngine); ok {
		return s.engineJourneyPage(ctx, hist, principal, inv, req)
	}
	summaries, err := eng.ListJourneys(ctx)
	if err != nil {
		return nil, "", 0, ownedError(err, principal, inv, "list")
	}
	if req.GetTerminalOnly() {
		summaries = closedJourneys(summaries)
	}
	if req.GetPage().GetCursor() != "" {
		return nil, "", 0, pageCursorRefused(inv, principal)
	}
	size, number := fallbackPageBounds(req)
	start := 0
	if number > 1 {
		start = (number - 1) * size
	}
	if start > len(summaries) {
		start = len(summaries)
	}
	end := start + size
	if end > len(summaries) {
		end = len(summaries)
	}
	return summaries[start:end], "", len(summaries), nil
}

// engineJourneyPage maps one wire list request onto the engine's paging
// port and returns its page unchanged.
func (s *server) engineJourneyPage(ctx context.Context, hist workspace.HistoryEngine, principal *trust.Principal, inv *transport.Invocation, req *journeyv1.ListJourneysRequest) ([]workspace.JourneySummary, string, int, *envelope.Error) {
	if req.GetTerminalOnly() {
		return s.terminalJourneyPage(ctx, hist, principal, inv, req)
	}
	page, err := hist.ListJourneysPage(ctx, journeyListQuery(req))
	if err != nil {
		return nil, "", 0, listPageError(err, principal, inv)
	}
	return page.Journeys, page.NextCursor, page.TotalCount, nil
}

// terminalJourneyPage drains the engine's filtered query and answers the
// requested slice of its closed journeys. Draining reuses the engine's own
// filtering and ordering; the closed selection reads the engine's viewer
// projection, never a stage list restated here.
func (s *server) terminalJourneyPage(ctx context.Context, hist workspace.HistoryEngine, principal *trust.Principal, inv *transport.Invocation, req *journeyv1.ListJourneysRequest) ([]workspace.JourneySummary, string, int, *envelope.Error) {
	if req.GetPage().GetCursor() != "" {
		return nil, "", 0, pageCursorRefused(inv, principal)
	}
	query := journeyListQuery(req)
	var closed []workspace.JourneySummary
	for {
		page, err := hist.ListJourneysPage(ctx, query)
		if err != nil {
			return nil, "", 0, listPageError(err, principal, inv)
		}
		closed = append(closed, closedJourneys(page.Journeys)...)
		if page.NextCursor == "" {
			break
		}
		query.Cursor = page.NextCursor
	}
	size, number := fallbackPageBounds(req)
	start := 0
	if number > 1 {
		start = (number - 1) * size
	}
	if start > len(closed) {
		start = len(closed)
	}
	end := start + size
	if end > len(closed) {
		end = len(closed)
	}
	return closed[start:end], "", len(closed), nil
}

// journeyListQuery maps the wire filters onto the engine's query. The page
// size travels as requested: zero means the engine's default, exactly as a
// client that never heard of paging would send.
func journeyListQuery(req *journeyv1.ListJourneysRequest) workspace.JourneyListRequest {
	return workspace.JourneyListRequest{
		PageSize:  int(req.GetPage().GetPageSize()),
		Page:      int(req.GetPageNumber()),
		Cursor:    req.GetPage().GetCursor(),
		WorkerRef: req.GetWorkerRef(),
		Query:     req.GetQuery(),
		Outcome:   req.GetOutcome(),
		Year:      req.GetYear(),
		Sort:      req.GetSort(),
		Direction: req.GetDirection(),
	}
}

// fallbackPageBounds mirrors the live engine's page-size default and cap
// for paths that slice locally (the pre-paging fallback and the terminal
// selection), so every path pages alike.
func fallbackPageBounds(req *journeyv1.ListJourneysRequest) (size, number int) {
	size = int(req.GetPage().GetPageSize())
	if size <= 0 {
		size = fallbackJourneyPageSize
	}
	if size > fallbackJourneyPageSizeMax {
		size = fallbackJourneyPageSizeMax
	}
	number = int(req.GetPageNumber())
	if number < 1 {
		number = 1
	}
	return size, number
}

// closedJourneys keeps the journeys the engine's viewer projection reports
// closed. Closed is the engine's terminal verdict for this viewer; the
// transport restates no stage list of its own.
func closedJourneys(summaries []workspace.JourneySummary) []workspace.JourneySummary {
	kept := summaries[:0:0]
	for _, summary := range summaries {
		if summary.Viewer.Closed {
			kept = append(kept, summary)
		}
	}
	return kept
}

// listPageError projects an engine paging failure: a refused cursor is the
// caller's field to fix, anything else is the engine's own error.
func listPageError(err error, principal *trust.Principal, inv *transport.Invocation) *envelope.Error {
	if err != nil && strings.Contains(err.Error(), historyCursorRefusal) {
		return pageCursorRefused(inv, principal)
	}
	return ownedError(err, principal, inv, "list")
}

// pageCursorRefused is the one answer for a page cursor the server cannot
// honor: an opaque engine cursor presented where terminal filtering or a
// non-paging engine makes it meaningless, or a cursor the engine refused.
func pageCursorRefused(inv *transport.Invocation, principal *trust.Principal) *envelope.Error {
	out := envelope.New(envelope.CodeInvalidArgument,
		"journey.list.input",
		"the request input is not acceptable").
		WithViolation("page.cursor", "review this field and try again", "journey.input.invalid")
	if inv != nil {
		out = out.WithCorrelation(inv.RequestID())
	}
	if principal != nil {
		out = out.WithEvidence(evidence(principal))
	}
	return out
}

// ListJourneys pages through workspace.HistoryEngine.ListJourneysPage when
// the engine implements it, and slices workspace.JourneyEngine.ListJourneys
// locally when it does not. Either way the wire page request — size, cursor,
// number and filters — is honored, and the response carries the page, its
// resume cursor and the total. READ_ONLY.
func (s *server) ListJourneys(ctx context.Context, req *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
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

	summaries, nextCursor, total, err := s.listJourneyPage(ctx, eng, principal, inv, req)
	if err != nil {
		return nil, err
	}
	diagAuthorized := s.diagnosticsAuthorized(ctx, principal)
	resp := &journeyv1.ListJourneysResponse{
		Page:       &commonv1.PageResponse{NextCursor: nextCursor},
		TotalCount: int32(total),
	}
	for _, summary := range summaries {
		resp.Journeys = append(resp.Journeys, toJourney(summary, diagAuthorized))
	}
	// UXLIVE-027: the one authorized population summary, over exactly the
	// journeys above, so no page recounts or re-filters the collection.
	resp.Population = toPopulation(workspace.SummarizeJourneys(summaries, s.deps.nowFunc()()))
	if reader, ok := eng.(workspace.WorkflowNotificationReader); ok {
		notices, err := reader.WorkflowNotifications(ctx, summaries)
		if err != nil {
			resp.NotificationsUnavailable = true
			return resp, nil
		}
		for _, notice := range notices {
			resp.Notifications = append(resp.Notifications, &journeyv1.WorkflowNotification{
				NotificationId: notice.ID, JourneyId: notice.JourneyID, WorkItemId: notice.WorkItemID,
				WorkerName: notice.WorkerName, Purpose: notice.Purpose, Status: notice.Status,
				CreatedAt: timestamppb.New(notice.CreatedAt), Read: notice.Read,
			})
		}
	}
	// WF-NOTIFY-001: status notices for requests the caller started. They share the
	// message shape; a status notice has no work item.
	if reader, ok := eng.(workspace.WorkflowStatusReader); ok && !resp.NotificationsUnavailable {
		notices, err := reader.WorkflowStatusNotifications(ctx, summaries)
		if err != nil {
			// One inbox serves both kinds; a partial list would read as complete.
			resp.Notifications, resp.NotificationsUnavailable = nil, true
			return resp, nil
		}
		for _, notice := range notices {
			resp.Notifications = append(resp.Notifications, &journeyv1.WorkflowNotification{
				NotificationId: notice.ID, JourneyId: notice.JourneyID, WorkerName: notice.WorkerName,
				Purpose: notice.Purpose, Status: notice.Status, CreatedAt: timestamppb.New(notice.CreatedAt), Read: notice.Read,
			})
		}
		sort.SliceStable(resp.Notifications, func(i, j int) bool {
			return resp.Notifications[i].GetCreatedAt().AsTime().After(resp.Notifications[j].GetCreatedAt().AsTime())
		})
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
	// The journeys/work page grants admit business reviewers; the one
	// diagnostics disclosure rule (RBAC-RT-005) additionally admits oversight
	// readers such as the auditor, who holds no product page grant. Both
	// checks are read-only snapshot loads before the engine reads, so no
	// side effect precedes authorization, and the engine still refuses
	// subjects its own policy does not disclose to the caller while masking
	// pay for redacted-only grants.
	if err := s.requireAnyFeatureView(ctx, principal, inv,
		featureAccessRequest{pageID: "journeys", featureID: "journey_detail"},
		featureAccessRequest{pageID: "work", featureID: "assigned_queue"},
	); err != nil {
		if !s.diagnosticsAuthorized(ctx, principal) {
			return nil, err
		}
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
// AddJourneyNote forwards to workspace.JourneyNoteEngine.AddNote. Append-only
// experience write. The page gate is the detail gate: whoever may read a
// journey's detail may leave a note on it, and the engine re-admits the
// caller to that one journey before recording anything.
func (s *server) AddJourneyNote(ctx context.Context, req *journeyv1.AddJourneyNoteRequest) (*journeyv1.AddJourneyNoteResponse, error) {
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
	eng, depErr := s.engine(principal, inv, "add_note")
	if depErr != nil {
		return nil, depErr
	}
	notes, ok := eng.(workspace.JourneyNoteEngine)
	if !ok {
		return nil, ownedError(fmt.Errorf("%w: this cell records no journey notes", workspace.ErrJourneyUnavailable), principal, inv, "add_note")
	}
	note, detail, err := notes.AddNote(ctx, req.GetIntentId(), workspace.JourneyNoteInput{
		Body: req.GetBody(), IdempotencyKey: req.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, ownedError(err, principal, inv, "add_note")
	}
	return &journeyv1.AddJourneyNoteResponse{
		Note:   toJourneyNote(note),
		Detail: toDetail(detail, s.diagnosticsAuthorized(ctx, principal)),
	}, nil
}

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
	// The complete listing stays available as the reporting-line source for
	// per-subject field disclosure; visibleWorkforce only filters rows.
	visible, options, err := s.visibleWorkforce(ctx, principal, workers, options)
	if err != nil {
		return nil, preferenceError(err, principal, inv.RequestID(), "load_organization_visibility")
	}
	return &journeyv1.ListWorkersResponse{
		Options: toWorkforceOptions(options),
		Workers: s.authorizeWorkers(ctx, principal, workers, visible),
	}, nil
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
	return &journeyv1.CreateWorkerResponse{Worker: s.authorizeWorker(ctx, principal, worker)}, nil
}
