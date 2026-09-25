// Package workorder adapts the generated WorkOrderService contract to the
// authenticated application service. It contains wire conversion only.
package workorder

import (
	"context"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	workorderv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workorder/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/application/workorderservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectmemberstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workorderstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	domain "github.com/monstercameron/human-capital-management-suite/internal/domains/workorder"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderbilling"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderreport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	workflowworkorder "github.com/monstercameron/human-capital-management-suite/internal/workflow/workorder"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"net/http"
)

const defaultPageSize = 50

const ProcedurePrefix = "/hcmnext.workorder.v1.WorkOrderService/"
const (
	CreateWorkOrderProcedure        = ProcedurePrefix + "CreateWorkOrder"
	GetWorkOrderProcedure           = ProcedurePrefix + "GetWorkOrder"
	ListWorkOrdersProcedure         = ProcedurePrefix + "ListWorkOrders"
	SubmitInitiatorRequestProcedure = ProcedurePrefix + "SubmitInitiatorRequest"
	DecideInitiatorRequestProcedure = ProcedurePrefix + "DecideInitiatorRequest"
	AddWorkOrderNoteProcedure       = ProcedurePrefix + "AddWorkOrderNote"
	RequestPhaseTransitionProcedure = ProcedurePrefix + "RequestPhaseTransition"
	RecordWorkEntryProcedure        = ProcedurePrefix + "RecordWorkEntry"
	RecordProgressEntryProcedure    = ProcedurePrefix + "RecordProgressEntry"
	RecordSpendEntryProcedure       = ProcedurePrefix + "RecordSpendEntry"
	RequestWorkOrderReportProcedure = ProcedurePrefix + "RequestWorkOrderReport"
	RequestBillingDraftProcedure    = ProcedurePrefix + "RequestBillingDraft"
)

// Service is the narrow application port used by this transport.
type Service interface {
	Create(context.Context, *trust.Principal, workorderservice.CreateRequest) (domain.Snapshot, error)
	Get(context.Context, *trust.Principal, workorderservice.ScopedRequest) (domain.Snapshot, error)
	SubmitRequest(context.Context, *trust.Principal, workorderservice.InitiatorRequest) (domain.Snapshot, error)
	DecideRequest(context.Context, *trust.Principal, workorderservice.DecisionRequest) (domain.Snapshot, error)
	AddNote(context.Context, *trust.Principal, workorderservice.NoteRequest) (domain.Snapshot, error)
	Transition(context.Context, *trust.Principal, workorderservice.TransitionRequest) (domain.Snapshot, error)
	Assign(context.Context, *trust.Principal, workorderservice.AssignmentRequest) (domain.Snapshot, error)
	RecordProgress(context.Context, *trust.Principal, workorderservice.ProgressRequest) (domain.Snapshot, error)
	RecordSpend(context.Context, *trust.Principal, workorderservice.SpendRequest) (domain.Snapshot, error)
	List(context.Context, *trust.Principal, workorderservice.ListRequest) (workorderservice.ListResult, error)
	RecordWorkEntry(context.Context, *trust.Principal, workorderservice.WorkEntryRequest) (workorderservice.WorkEntryResult, error)
	RequestReport(context.Context, *trust.Principal, workorderservice.ReportRequest) (workorderservice.ReportResult, error)
	RequestBillingDraft(context.Context, *trust.Principal, workorderservice.BillingDraftRequest) (workorderservice.BillingDraftResult, error)
}

type Dependencies struct{ Service Service }

type server struct {
	workorderv1.UnimplementedWorkOrderServiceServer
	service Service
}

func Register(srv *grpc.Server, deps Dependencies) {
	workorderv1.RegisterWorkOrderServiceServer(srv, &server{service: deps.Service})
}

func admitted(ctx context.Context) (*trust.Principal, error) {
	inv, ok := transport.InvocationFromContext(ctx)
	if !ok || inv == nil {
		return nil, envelope.New(envelope.CodeUnauthenticated, "workorder.no_trusted_context", "request has no trusted context")
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.Subject() == "" || p.Tenant().String() == "" {
		return nil, envelope.New(envelope.CodeUnauthenticated, "workorder.no_principal", "request has no authenticated principal")
	}
	if bound := inv.Principal(); bound != nil && bound != p {
		return nil, envelope.New(envelope.CodeUnauthenticated, "workorder.principal_mismatch", "request has no authenticated principal")
	}
	return p, nil
}

func scopeContext(scope *commonv1.ScopeContext, p *trust.Principal) (string, error) {
	if scope == nil {
		return "", envelope.New(envelope.CodeInvalidArgument, "workorder.scope_required", "work order scope is required")
	}
	if p == nil || (scope.GetTenantId() != "" && scope.GetTenantId() != p.Tenant().String()) {
		return "", envelope.New(envelope.CodeNotFound, "workorder.scope_mismatch", "work order was not found")
	}
	// organization_scope_id is a scope label, not a project id. The application
	// service resolves the work order's project under tenant scope and performs
	// current authorization against that verified project.
	return "", nil
}

func (s *server) available() error {
	if s == nil || s.service == nil {
		return envelope.New(envelope.CodeUnavailable, "workorder.service.unavailable", "work order operation is unavailable")
	}
	return nil
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := envelope.As(err); ok {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return status.FromContextError(err).Err()
	}
	switch {
	case errors.Is(err, workorderservice.ErrInvalidPrincipal):
		return envelope.New(envelope.CodeUnauthenticated, "workorder.invalid_principal", "request has no authenticated principal")
	case errors.Is(err, workorderservice.ErrInvalidRequest), errors.Is(err, domain.ErrInvalid):
		return envelope.New(envelope.CodeInvalidArgument, "workorder.invalid_argument", "work order request is invalid")
	case errors.Is(err, workorderservice.ErrUnavailable):
		return envelope.New(envelope.CodeUnavailable, "workorder.service.unavailable", "work order operation is unavailable")
	case errors.Is(err, workorderservice.ErrArtifactUnavailable):
		return envelope.New(envelope.CodeUnavailable, "workorder.artifact.unavailable", "work order report or billing service is unavailable")
	case errors.Is(err, workorderservice.ErrInvalidPage):
		return envelope.New(envelope.CodeInvalidArgument, "workorder.page.invalid", "work order page request is invalid")
	case errors.Is(err, workorderservice.ErrInvalidCursor):
		return envelope.New(envelope.CodeAborted, "workorder.cursor.invalid", "work order page cursor is invalid or stale")
	case errors.Is(err, workorderaccess.ErrUnauthorized), errors.Is(err, workorderaccess.ErrSelfApproval), errors.Is(err, projectaccess.ErrUnauthorized):
		return envelope.New(envelope.CodePermissionDenied, "workorder.permission_denied", "work order operation is not authorized")
	case errors.Is(err, workorderservice.ErrScopeMismatch), errors.Is(err, workorderstore.ErrNotFound), errors.Is(err, projectmemberstore.ErrNotFound), errors.Is(err, projectaccess.ErrMemberNotFound):
		return envelope.New(envelope.CodeNotFound, "workorder.not_found", "work order was not found")
	case errors.Is(err, workorderservice.ErrWorkerIneligible):
		return envelope.New(envelope.CodeFailedPrecondition, "workorder.worker_ineligible", "worker is not eligible for this work order")
	case errors.Is(err, domain.ErrNoteVisibilityUnsupported):
		return envelope.New(envelope.CodeFailedPrecondition, "workorder.note_visibility_unavailable", "requested note visibility is unavailable")
	case errors.Is(err, workflowworkorder.ErrTemplateMismatch), errors.Is(err, workflowworkorder.ErrPhaseTransition), errors.Is(err, workflowworkorder.ErrPhaseBinding):
		return envelope.New(envelope.CodeFailedPrecondition, "workorder.workflow.rejected", "work order phase transition was rejected")
	case errors.Is(err, workflowworkorder.ErrStaleGateFacts):
		return envelope.New(envelope.CodeAborted, "workorder.workflow.gate_stale", "work order gate facts are stale")
	case errors.Is(err, domain.ErrNotFound):
		return envelope.New(envelope.CodeNotFound, "workorder.not_found", "work order resource was not found")
	case errors.Is(err, domain.ErrStale), errors.Is(err, workorderstore.ErrRevisionConflict):
		return envelope.New(envelope.CodeAborted, "workorder.revision_conflict", "work order revision has changed")
	case errors.Is(err, domain.ErrConflict), errors.Is(err, workorderstore.ErrIdempotencyConflict):
		return envelope.New(envelope.CodeAlreadyExists, "workorder.idempotency_conflict", "idempotency key was used for another request")
	case errors.Is(err, domain.ErrTransition):
		return envelope.New(envelope.CodeFailedPrecondition, "workorder.transition_rejected", "work order transition was rejected")
	default:
		return envelope.New(envelope.CodeUnspecified, "workorder.internal_error", "work order operation failed")
	}
}

func pageSize(page *commonv1.PageRequest) (int, error) {
	if page == nil || page.GetPageSize() == 0 {
		return defaultPageSize, nil
	}
	n := int(page.GetPageSize())
	if n < 1 || n > 100 {
		return 0, envelope.New(envelope.CodeInvalidArgument, "workorder.page_size.invalid", "page size must be between 1 and 100")
	}
	return n, nil
}

func workOrderMessage(s domain.Snapshot) *workorderv1.WorkOrder {
	out := &workorderv1.WorkOrder{Id: s.ID, ProjectId: s.ProjectID, Title: s.Title, Scope: s.Scope, TemplateId: s.TemplateID, TemplateVersion: s.TemplateVersion, PhaseId: string(s.Phase), Revision: s.Revision, InitiatorId: s.InitiatorID, SupervisorId: s.SupervisorID}
	switch s.Phase {
	case domain.PhaseDraft:
		out.Status = workorderv1.WorkOrderStatus_WORK_ORDER_STATUS_DRAFT
	case domain.PhaseCancelled:
		out.Status = workorderv1.WorkOrderStatus_WORK_ORDER_STATUS_CANCELLED
	case domain.PhaseAccepted:
		out.Status = workorderv1.WorkOrderStatus_WORK_ORDER_STATUS_ACCEPTED
	case domain.PhaseClosed:
		out.Status = workorderv1.WorkOrderStatus_WORK_ORDER_STATUS_CLOSED
	default:
		out.Status = workorderv1.WorkOrderStatus_WORK_ORDER_STATUS_ACTIVE
	}
	if !s.CreatedAt.IsZero() {
		out.CreatedAt = timestamppb.New(s.CreatedAt)
	} else if len(s.Journal) > 0 {
		out.CreatedAt = timestamppb.New(s.Journal[0].At)
	}
	if !s.UpdatedAt.IsZero() {
		out.UpdatedAt = timestamppb.New(s.UpdatedAt)
	} else if len(s.Journal) > 0 {
		out.UpdatedAt = timestamppb.New(s.Journal[len(s.Journal)-1].At)
	}
	return out
}

func decimalValue(d *commonv1.Decimal) (values.Decimal, error) {
	if d == nil || d.GetScale() < 0 || d.GetScale() > values.MaxScale {
		return values.Decimal{}, values.ErrDecimalUnset
	}
	mag := new(big.Int).SetBytes(d.GetUnscaledMagnitude())
	if mag.Sign() == 0 && d.GetSign() != commonv1.DecimalSign_DECIMAL_SIGN_ZERO {
		return values.Decimal{}, values.ErrDecimalUnset
	}
	if mag.Sign() != 0 && d.GetSign() == commonv1.DecimalSign_DECIMAL_SIGN_ZERO {
		return values.Decimal{}, values.ErrDecimalUnset
	}
	negative := d.GetSign() == commonv1.DecimalSign_DECIMAL_SIGN_NEGATIVE
	if !negative && d.GetSign() != commonv1.DecimalSign_DECIMAL_SIGN_POSITIVE && d.GetSign() != commonv1.DecimalSign_DECIMAL_SIGN_ZERO {
		return values.Decimal{}, values.ErrDecimalUnset
	}
	text := mag.String()
	if d.GetScale() > 0 {
		if len(text) <= int(d.GetScale()) {
			text = strings.Repeat("0", int(d.GetScale())-len(text)+1) + text
		}
		text = text[:len(text)-int(d.GetScale())] + "." + text[len(text)-int(d.GetScale()):]
	}
	if negative {
		text = "-" + text
	}
	return values.NewDecimal(text, d.GetScale(), values.RoundingExactRequired)
}

func workOrderScope(scope *commonv1.ScopeContext, p *trust.Principal, orderID, key string) (workorderservice.ScopedRequest, error) {
	projectID, err := scopeContext(scope, p)
	if err != nil {
		return workorderservice.ScopedRequest{}, err
	}
	return workorderservice.ScopedRequest{ProjectID: projectID, WorkOrderID: orderID, IdempotencyKey: key}, nil
}

func (s *server) CreateWorkOrder(ctx context.Context, req *workorderv1.CreateWorkOrderRequest) (*workorderv1.CreateWorkOrderResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.available(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "workorder.invalid_argument", "work order request is invalid")
	}
	if _, err = scopeContext(req.GetScopeContext(), p); err != nil {
		return nil, err
	}
	snap, err := s.service.Create(ctx, p, workorderservice.CreateRequest{ProjectID: req.GetProjectId(), Title: req.GetTitle(), Scope: req.GetScope(), TemplateID: req.GetTemplateId(), TemplateVersion: req.GetTemplateVersion(), SupervisorID: req.GetSupervisorId(), LinkedTaskIDs: append([]string(nil), req.GetLinkedTaskIds()...), IdempotencyKey: req.GetIdempotencyKey()})
	if err != nil {
		return nil, mapError(err)
	}
	return &workorderv1.CreateWorkOrderResponse{WorkOrder: workOrderMessage(snap)}, nil
}

func (s *server) GetWorkOrder(ctx context.Context, req *workorderv1.GetWorkOrderRequest) (*workorderv1.GetWorkOrderResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.available(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "workorder.invalid_argument", "work order request is invalid")
	}
	scope, err := workOrderScope(req.GetScopeContext(), p, req.GetWorkOrderId(), "")
	if err != nil {
		return nil, err
	}
	snap, err := s.service.Get(ctx, p, scope)
	if err != nil {
		return nil, mapError(err)
	}
	return &workorderv1.GetWorkOrderResponse{WorkOrder: workOrderMessage(snap)}, nil
}

func (s *server) SubmitInitiatorRequest(ctx context.Context, req *workorderv1.SubmitInitiatorRequestRequest) (*workorderv1.SubmitInitiatorRequestResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.available(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, invalid()
	}
	scope, err := workOrderScope(req.GetScopeContext(), p, req.GetWorkOrderId(), req.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	in := domain.InitiatorRequestInput{}
	switch detail := req.GetDetails().(type) {
	case *workorderv1.SubmitInitiatorRequestRequest_Budget:
		b := detail.Budget
		if b == nil {
			return nil, invalid()
		}
		amount, e := moneyValue(b.GetRequestedLimit())
		if e != nil {
			return nil, invalid()
		}
		in.Kind = domain.RequestBudget
		in.Amount = amount
		in.Currency = b.GetRequestedLimit().GetCurrencyCode()
		in.CostCategory = b.GetCostCategory()
		in.FundingSource = b.GetFundingSource()
		in.Rationale = b.GetRationale()
		in.Subject = "Budget request"
		in.BaselineRevision = strconv.FormatUint(req.GetExpectedRevision(), 10)
		in.EvidenceRef = firstEvidence(b.GetEvidence())
		in.EvidenceRefs = evidenceRefs(b.GetEvidence())
	case *workorderv1.SubmitInitiatorRequestRequest_Approval:
		a := detail.Approval
		if a == nil {
			return nil, invalid()
		}
		in.Kind = domain.RequestApproval
		in.Subject = a.GetSubject()
		in.Rationale = a.GetRationale()
		in.Policy = "approval"
		in.ApproverClass = a.GetApproverClass()
		in.ProposedAction = a.GetProposedAction()
		in.DueWindow = timestampText(a.GetDueAt())
		in.EvidenceRef = firstEvidence(a.GetEvidence())
		in.EvidenceRefs = evidenceRefs(a.GetEvidence())
	case *workorderv1.SubmitInitiatorRequestRequest_Resource:
		r := detail.Resource
		if r == nil {
			return nil, invalid()
		}
		q, e := decimalValue(r.GetQuantity())
		if e != nil {
			return nil, invalid()
		}
		in.Kind = requestKind(req.GetKind())
		in.RoleOrItem = r.GetRoleOrItem()
		in.Quantity = q
		in.Unit = r.GetUnit()
		in.NeededUntil = timestampText(r.GetNeededUntil())
		if cost := r.GetEstimatedCost(); cost != nil {
			in.EstimatedCost, e = moneyValue(cost)
			if e != nil {
				return nil, invalid()
			}
			in.EstimatedCostSpecified = true
			in.EstimatedCostCurrency = cost.GetCurrencyCode()
		}
		in.Location = r.GetLocation()
		in.Rationale = r.GetRationale()
		in.DueWindow = timestampText(r.GetNeededFrom())
		in.EvidenceRef = firstEvidence(r.GetEvidence())
		in.EvidenceRefs = evidenceRefs(r.GetEvidence())
		in.Subject = "Resource request"
	case *workorderv1.SubmitInitiatorRequestRequest_Inspection:
		r := detail.Inspection
		if r == nil {
			return nil, invalid()
		}
		in.Kind = domain.RequestInspection
		in.Subject = r.GetInspectionScope()
		in.Rationale = r.GetRationale()
		in.Policy = r.GetEvidencePolicyRef()
		in.DueWindow = timestampText(r.GetDueAt())
		in.EvidenceRef = firstEvidence(r.GetEvidence())
		in.EvidenceRefs = evidenceRefs(r.GetEvidence())
	case *workorderv1.SubmitInitiatorRequestRequest_ChangeOrder:
		r := detail.ChangeOrder
		if r == nil {
			return nil, invalid()
		}
		in.Kind = domain.RequestChangeOrder
		in.Subject = "Change order"
		in.ScopeDelta = r.GetScopeDelta()
		in.BaselineRevision = strconv.FormatUint(r.GetBaselineRevision(), 10)
		var e error
		in.QuantityDelta, e = decimalValue(r.GetQuantityDelta())
		if e != nil {
			return nil, invalid()
		}
		in.QuantityDeltaUnit = r.GetUnit()
		in.ScheduleDelta = strconv.FormatInt(r.GetScheduleDeltaSeconds(), 10)
		if price := r.GetPriceDelta(); price != nil {
			in.PriceDelta, e = moneyValue(price)
			if e != nil {
				return nil, invalid()
			}
			in.PriceDeltaSpecified = true
			in.PriceDeltaCurrency = price.GetCurrencyCode()
		}
		in.Rationale = r.GetRationale()
		in.EvidenceRef = firstEvidence(r.GetEvidence())
		in.EvidenceRefs = evidenceRefs(r.GetEvidence())
	case *workorderv1.SubmitInitiatorRequestRequest_BillingReview:
		r := detail.BillingReview
		if r == nil {
			return nil, invalid()
		}
		in.Kind = domain.RequestBillingReview
		in.Subject = "Billing review"
		in.BillingPeriod = timestampText(r.GetPeriodStart()) + "/" + timestampText(r.GetPeriodEnd())
		in.PricingVersion = r.GetContractVersion()
		in.BaselineRevision = strconv.FormatUint(r.GetSourceCutoffRevision(), 10)
		in.Rationale = r.GetRationale()
	case *workorderv1.SubmitInitiatorRequestRequest_Document:
		r := detail.Document
		if r == nil {
			return nil, invalid()
		}
		in.Kind = domain.RequestDocument
		in.Subject = r.GetDocumentKind()
		in.Policy = r.GetEvidencePolicyRef()
		in.Rationale = r.GetRationale()
		in.DueWindow = timestampText(r.GetDueAt())
		in.EvidenceRef = firstEvidence(r.GetEvidence())
		in.EvidenceRefs = evidenceRefs(r.GetEvidence())
	default:
		return nil, invalid()
	}
	wireKind := requestKind(req.GetKind())
	if err := validateDetailsKind(wireKind, in.Kind); err != nil {
		return nil, invalid()
	}
	in.Kind = wireKind
	in.ExpectedRevision = req.GetExpectedRevision()
	form, err := copyFormValues(req.GetFormValues())
	if err != nil {
		return nil, invalid()
	}
	in.FormValues = form
	snap, err := s.service.SubmitRequest(ctx, p, workorderservice.InitiatorRequest{ScopedRequest: scope, ExpectedRevision: req.GetExpectedRevision(), Input: in})
	if err != nil {
		return nil, mapError(err)
	}
	return &workorderv1.SubmitInitiatorRequestResponse{WorkOrder: workOrderMessage(snap), Request: latestRequest(snap)}, nil
}

func (s *server) DecideInitiatorRequest(ctx context.Context, req *workorderv1.DecideInitiatorRequestRequest) (*workorderv1.DecideInitiatorRequestResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.available(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, invalid()
	}
	scope, err := workOrderScope(req.GetScopeContext(), p, req.GetWorkOrderId(), req.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	decision := domain.RequestRejected
	switch req.GetDecision() {
	case workorderv1.DecisionKind_DECISION_KIND_APPROVE:
		decision = domain.RequestApproved
	case workorderv1.DecisionKind_DECISION_KIND_REJECT:
		decision = domain.RequestRejected
	case workorderv1.DecisionKind_DECISION_KIND_RETURN_FOR_REVISION:
		decision = domain.RequestReturned
	default:
		return nil, invalid()
	}
	snap, err := s.service.DecideRequest(ctx, p, workorderservice.DecisionRequest{ScopedRequest: scope, ExpectedRevision: req.GetExpectedRevision(), Input: domain.RequestDecisionInput{RequestID: req.GetRequestId(), ExpectedRevision: req.GetExpectedRevision(), Decision: decision, Reason: req.GetReason(), IdempotencyKey: req.GetIdempotencyKey()}})
	if err != nil {
		return nil, mapError(err)
	}
	return &workorderv1.DecideInitiatorRequestResponse{WorkOrder: workOrderMessage(snap), Request: findRequest(snap, req.GetRequestId())}, nil
}

func (s *server) AddWorkOrderNote(ctx context.Context, req *workorderv1.AddWorkOrderNoteRequest) (*workorderv1.AddWorkOrderNoteResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.available(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, invalid()
	}
	scope, err := workOrderScope(req.GetScopeContext(), p, req.GetWorkOrderId(), req.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	visibility, err := noteVisibility(req.GetVisibility())
	if err != nil {
		return nil, err
	}
	snap, err := s.service.AddNote(ctx, p, workorderservice.NoteRequest{ScopedRequest: scope, ExpectedRevision: req.GetExpectedRevision(), Input: domain.NoteInput{Body: req.GetBody(), Visibility: visibility, Classification: req.GetClassification(), Anchor: req.GetPhaseId() + "/" + req.GetLineId(), AttachmentRefs: evidenceRefs(req.GetAttachments()), ExpectedRevision: req.GetExpectedRevision(), IdempotencyKey: req.GetIdempotencyKey()}})
	if err != nil {
		return nil, mapError(err)
	}
	return &workorderv1.AddWorkOrderNoteResponse{WorkOrder: workOrderMessage(snap), Note: latestNote(snap)}, nil
}

func noteVisibility(visibility workorderv1.NoteVisibility) (string, error) {
	switch visibility {
	case workorderv1.NoteVisibility_NOTE_VISIBILITY_PARTICIPANTS:
		return "PARTICIPANTS", nil
	case workorderv1.NoteVisibility_NOTE_VISIBILITY_SUPERVISORS, workorderv1.NoteVisibility_NOTE_VISIBILITY_FINANCE:
		return "", mapError(domain.ErrNoteVisibilityUnsupported)
	default:
		return "", invalid()
	}
}

func (s *server) RequestPhaseTransition(ctx context.Context, req *workorderv1.RequestPhaseTransitionRequest) (*workorderv1.RequestPhaseTransitionResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.available(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, invalid()
	}
	scope, err := workOrderScope(req.GetScopeContext(), p, req.GetWorkOrderId(), req.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	snap, err := s.service.Transition(ctx, p, workorderservice.TransitionRequest{ScopedRequest: scope, ExpectedRevision: req.GetExpectedRevision(), Input: domain.TransitionInput{Target: domain.Phase(req.GetTargetPhaseId()), Reason: req.GetReason(), EvidenceRefs: evidenceRefs(req.GetEvidence()), ExpectedRevision: req.GetExpectedRevision(), IdempotencyKey: req.GetIdempotencyKey()}})
	if err != nil {
		return nil, mapError(err)
	}
	return &workorderv1.RequestPhaseTransitionResponse{WorkOrder: workOrderMessage(snap), Result: &workorderv1.PhaseTransitionResult{Applied: true}}, nil
}

func (s *server) RecordProgressEntry(ctx context.Context, req *workorderv1.RecordProgressEntryRequest) (*workorderv1.RecordProgressEntryResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.available(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, invalid()
	}
	scope, err := workOrderScope(req.GetScopeContext(), p, req.GetWorkOrderId(), req.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	qty, err := decimalValue(req.GetCompletedQuantity())
	if err != nil {
		return nil, invalid()
	}
	snap, err := s.service.RecordProgress(ctx, p, workorderservice.ProgressRequest{ScopedRequest: scope, ExpectedRevision: req.GetExpectedRevision(), Input: domain.ProgressInput{LineID: req.GetLineId(), Quantity: qty, Unit: req.GetUnit(), EvidenceRef: firstEvidence(req.GetEvidence()), EvidenceRefs: evidenceRefs(req.GetEvidence()), ExpectedRevision: req.GetExpectedRevision(), IdempotencyKey: req.GetIdempotencyKey()}})
	if err != nil {
		return nil, mapError(err)
	}
	return &workorderv1.RecordProgressEntryResponse{WorkOrder: workOrderMessage(snap), Entry: latestProgress(snap)}, nil
}

func (s *server) RecordSpendEntry(ctx context.Context, req *workorderv1.RecordSpendEntryRequest) (*workorderv1.RecordSpendEntryResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.available(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, invalid()
	}
	scope, err := workOrderScope(req.GetScopeContext(), p, req.GetWorkOrderId(), req.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	amount, err := moneyValue(req.GetAmount())
	if err != nil {
		return nil, invalid()
	}
	category := workEntryKind(req.GetCategory())
	if category == "" {
		return nil, invalid()
	}
	disposition, err := spendDisposition(req.GetDisposition())
	if err != nil {
		return nil, invalid()
	}
	quantity := values.Decimal{}
	if req.GetQuantity() != nil {
		quantity, err = decimalValue(req.GetQuantity())
		if err != nil {
			return nil, invalid()
		}
	}
	snap, err := s.service.RecordSpend(ctx, p, workorderservice.SpendRequest{ScopedRequest: scope, ExpectedRevision: req.GetExpectedRevision(), Input: domain.SpendInput{Category: category, Disposition: disposition, Amount: amount, Currency: req.GetAmount().GetCurrencyCode(), Quantity: quantity, QuantitySpecified: req.GetQuantity() != nil, Unit: req.GetUnit(), SourceRef: req.GetSourceDocumentRef(), ExpectedRevision: req.GetExpectedRevision(), IdempotencyKey: req.GetIdempotencyKey()}})
	if err != nil {
		return nil, mapError(err)
	}
	return &workorderv1.RecordSpendEntryResponse{WorkOrder: workOrderMessage(snap), Entry: latestSpend(snap)}, nil
}

func invalid() error {
	return envelope.New(envelope.CodeInvalidArgument, "workorder.invalid_argument", "work order request is invalid")
}
func moneyValue(m *commonv1.Money) (values.Decimal, error) {
	if m == nil {
		return values.Decimal{}, values.ErrDecimalUnset
	}
	return decimalValue(m.GetAmount())
}
func requestKind(k workorderv1.InitiatorRequestKind) domain.RequestKind {
	switch k {
	case workorderv1.InitiatorRequestKind_INITIATOR_REQUEST_KIND_BUDGET:
		return domain.RequestBudget
	case workorderv1.InitiatorRequestKind_INITIATOR_REQUEST_KIND_APPROVAL:
		return domain.RequestApproval
	case workorderv1.InitiatorRequestKind_INITIATOR_REQUEST_KIND_CREW:
		return domain.RequestCrew
	case workorderv1.InitiatorRequestKind_INITIATOR_REQUEST_KIND_MATERIAL:
		return domain.RequestMaterial
	case workorderv1.InitiatorRequestKind_INITIATOR_REQUEST_KIND_EQUIPMENT:
		return domain.RequestEquipment
	case workorderv1.InitiatorRequestKind_INITIATOR_REQUEST_KIND_INSPECTION:
		return domain.RequestInspection
	case workorderv1.InitiatorRequestKind_INITIATOR_REQUEST_KIND_DOCUMENT:
		return domain.RequestDocument
	case workorderv1.InitiatorRequestKind_INITIATOR_REQUEST_KIND_CHANGE_ORDER:
		return domain.RequestChangeOrder
	case workorderv1.InitiatorRequestKind_INITIATOR_REQUEST_KIND_BILLING_REVIEW:
		return domain.RequestBillingReview
	}
	return ""
}
func validateDetailsKind(wire, detail domain.RequestKind) error {
	if wire == "" || detail == "" || wire != detail {
		return errors.New("initiator request kind does not match typed details")
	}
	switch wire {
	case domain.RequestBudget, domain.RequestApproval, domain.RequestCrew, domain.RequestMaterial, domain.RequestEquipment, domain.RequestInspection, domain.RequestDocument, domain.RequestChangeOrder, domain.RequestBillingReview:
		return nil
	}
	return errors.New("unsupported initiator request kind")
}
func copyFormValues(values map[string]string) (map[string]string, error) {
	if len(values) > 64 {
		return nil, errors.New("too many form values")
	}
	out := make(map[string]string, len(values))
	total := 0
	for key, value := range values {
		if key == "" || len(key) > 128 || len(value) > 4096 {
			return nil, errors.New("form value exceeds size limit")
		}
		total += len(key) + len(value)
		if total > 65536 {
			return nil, errors.New("form values exceed aggregate limit")
		}
		out[key] = value
	}
	return out, nil
}
func timestampText(t *timestamppb.Timestamp) string {
	if t == nil || !t.IsValid() {
		return ""
	}
	return t.AsTime().UTC().Format(time.RFC3339)
}
func evidenceRefs(es []*commonv1.EvidenceRef) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		if e != nil && e.GetEvidenceId() != "" {
			out = append(out, e.GetEvidenceId())
		}
	}
	return out
}
func evidenceMessages(refs []string) []*commonv1.EvidenceRef {
	out := make([]*commonv1.EvidenceRef, 0, len(refs))
	for _, ref := range refs {
		if ref != "" {
			out = append(out, &commonv1.EvidenceRef{EvidenceId: ref})
		}
	}
	return out
}
func timestampValue(text string) *timestamppb.Timestamp {
	if text == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return nil
	}
	return timestamppb.New(t)
}
func firstEvidence(es []*commonv1.EvidenceRef) string {
	r := evidenceRefs(es)
	if len(r) > 0 {
		return r[0]
	}
	return ""
}
func latestRequest(s domain.Snapshot) *workorderv1.InitiatorRequest {
	if len(s.Requests) == 0 {
		return nil
	}
	return requestMessage(s.Requests[len(s.Requests)-1], s.ID, s.Revision)
}
func findRequest(s domain.Snapshot, id string) *workorderv1.InitiatorRequest {
	for _, r := range s.Requests {
		if r.ID == id {
			return requestMessage(r, s.ID, s.Revision)
		}
	}
	return nil
}
func requestMessage(r domain.InitiatorRequest, orderID string, revision uint64) *workorderv1.InitiatorRequest {
	out := &workorderv1.InitiatorRequest{Id: r.ID, WorkOrderId: orderID, Kind: workorderv1.InitiatorRequestKind(workorderv1.InitiatorRequestKind_value["INITIATOR_REQUEST_KIND_"+string(r.Kind)]), RequesterId: r.RequesterID, Rationale: r.Rationale, CreatedAt: timestamppb.New(r.CreatedAt), WorkOrderRevision: revision}
	if r.EvidenceRef != "" {
		out.Evidence = []*commonv1.EvidenceRef{{EvidenceId: r.EvidenceRef}}
	}
	if len(r.EvidenceRefs) > 0 {
		out.Evidence = evidenceMessages(r.EvidenceRefs)
	}
	switch r.Status {
	case domain.RequestPending:
		out.Status = workorderv1.RequestStatus_REQUEST_STATUS_PENDING
	case domain.RequestApproved:
		out.Status = workorderv1.RequestStatus_REQUEST_STATUS_APPROVED
	case domain.RequestRejected:
		out.Status = workorderv1.RequestStatus_REQUEST_STATUS_REJECTED
	case domain.RequestReturned:
		out.Status = workorderv1.RequestStatus_REQUEST_STATUS_RETURNED
	}
	switch r.Kind {
	case domain.RequestBudget, domain.RequestBudgetChange:
		out.Details = &workorderv1.InitiatorRequest_Budget{Budget: &workorderv1.BudgetRequest{RequestedLimit: moneyMessage(r.Amount, r.Currency), CostCategory: r.CostCategory, FundingSource: r.FundingSource, Rationale: r.Rationale, Evidence: evidenceMessages(r.EvidenceRefs)}}
	case domain.RequestApproval:
		out.Details = &workorderv1.InitiatorRequest_Approval{Approval: &workorderv1.ApprovalRequest{Subject: r.Subject, ProposedAction: r.ProposedAction, ApproverClass: r.ApproverClass, Rationale: r.Rationale, DueAt: timestampValue(r.DueWindow), Evidence: evidenceMessages(r.EvidenceRefs)}}
	case domain.RequestCrew, domain.RequestMaterial, domain.RequestEquipment:
		resource := &workorderv1.ResourceRequest{RoleOrItem: r.RoleOrItem, Quantity: decimalMessage(r.Quantity), Unit: r.Unit, Location: r.Location, NeededFrom: timestampValue(r.DueWindow), NeededUntil: timestampValue(r.NeededUntil), Rationale: r.Rationale, Evidence: evidenceMessages(r.EvidenceRefs)}
		if r.EstimatedCostSpecified {
			resource.EstimatedCost = moneyMessage(r.EstimatedCost, r.EstimatedCostCurrency)
		}
		out.Details = &workorderv1.InitiatorRequest_Resource{Resource: resource}
	case domain.RequestInspection:
		out.Details = &workorderv1.InitiatorRequest_Inspection{Inspection: &workorderv1.InspectionRequest{InspectionScope: r.Subject, EvidencePolicyRef: r.Policy, Rationale: r.Rationale, DueAt: timestampValue(r.DueWindow), Evidence: evidenceMessages(r.EvidenceRefs)}}
	case domain.RequestChangeOrder:
		baseline, _ := strconv.ParseUint(r.BaselineRevision, 10, 64)
		schedule, _ := strconv.ParseInt(r.ScheduleDelta, 10, 64)
		change := &workorderv1.ChangeOrderRequest{ScopeDelta: r.ScopeDelta, QuantityDelta: decimalMessage(r.QuantityDelta), Unit: r.QuantityDeltaUnit, ScheduleDeltaSeconds: schedule, BaselineRevision: baseline, Rationale: r.Rationale, Evidence: evidenceMessages(r.EvidenceRefs)}
		if r.PriceDeltaSpecified {
			change.PriceDelta = moneyMessage(r.PriceDelta, r.PriceDeltaCurrency)
		}
		out.Details = &workorderv1.InitiatorRequest_ChangeOrder{ChangeOrder: change}
	case domain.RequestBillingReview:
		period := strings.SplitN(r.BillingPeriod, "/", 2)
		billing := &workorderv1.BillingReviewRequest{ContractVersion: r.PricingVersion, Rationale: r.Rationale}
		if len(period) == 2 {
			billing.PeriodStart = timestampValue(period[0])
			billing.PeriodEnd = timestampValue(period[1])
		}
		if r.BaselineRevision != "" {
			billing.SourceCutoffRevision, _ = strconv.ParseUint(r.BaselineRevision, 10, 64)
		}
		out.Details = &workorderv1.InitiatorRequest_BillingReview{BillingReview: billing}
	case domain.RequestDocument:
		out.Details = &workorderv1.InitiatorRequest_Document{Document: &workorderv1.DocumentRequest{DocumentKind: r.Subject, EvidencePolicyRef: r.Policy, Rationale: r.Rationale, DueAt: timestampValue(r.DueWindow), Evidence: evidenceMessages(r.EvidenceRefs)}}
	}
	return out
}
func latestNote(s domain.Snapshot) *workorderv1.WorkOrderNote {
	if len(s.Notes) == 0 {
		return nil
	}
	n := s.Notes[len(s.Notes)-1]
	out := &workorderv1.WorkOrderNote{Id: n.ID, WorkOrderId: s.ID, AuthorId: n.AuthorID, Body: n.Body, Classification: n.Classification, Revision: s.Revision, CreatedAt: timestamppb.New(n.At)}
	parts := strings.SplitN(n.Anchor, "/", 2)
	if len(parts) > 0 {
		out.PhaseId = parts[0]
	}
	if len(parts) > 1 {
		out.LineId = parts[1]
	}
	switch n.Visibility {
	case "PARTICIPANTS":
		out.Visibility = workorderv1.NoteVisibility_NOTE_VISIBILITY_PARTICIPANTS
	case "SUPERVISORS":
		out.Visibility = workorderv1.NoteVisibility_NOTE_VISIBILITY_SUPERVISORS
	case "FINANCE":
		out.Visibility = workorderv1.NoteVisibility_NOTE_VISIBILITY_FINANCE
	}
	for _, ref := range n.AttachmentRefs {
		out.Attachments = append(out.Attachments, &commonv1.EvidenceRef{EvidenceId: ref})
	}
	return out
}
func latestProgress(s domain.Snapshot) *workorderv1.ProgressEntry {
	if len(s.Progress) == 0 {
		return nil
	}
	p := s.Progress[len(s.Progress)-1]
	out := &workorderv1.ProgressEntry{Id: p.ID, WorkOrderId: s.ID, LineId: p.LineID, Unit: p.Unit, CompletedQuantity: decimalMessage(p.Quantity), Status: workorderv1.ProgressStatus_PROGRESS_STATUS_SUBMITTED, Revision: s.Revision}
	if len(p.EvidenceRefs) > 0 {
		out.Evidence = evidenceMessages(p.EvidenceRefs)
	} else if p.EvidenceRef != "" {
		out.Evidence = []*commonv1.EvidenceRef{{EvidenceId: p.EvidenceRef}}
	}
	return out
}
func latestSpend(s domain.Snapshot) *workorderv1.SpendEntry {
	if len(s.Spending) == 0 {
		return nil
	}
	p := s.Spending[len(s.Spending)-1]
	disposition := workorderv1.SpendDisposition_SPEND_DISPOSITION_UNSPECIFIED
	if p.Disposition == domain.SpendCommitment {
		disposition = workorderv1.SpendDisposition_SPEND_DISPOSITION_COMMITMENT
	} else if p.Disposition == domain.SpendIncurred {
		disposition = workorderv1.SpendDisposition_SPEND_DISPOSITION_INCURRED
	}
	out := &workorderv1.SpendEntry{Id: p.ID, WorkOrderId: s.ID, Category: protoWorkEntryKind(p.Category), Disposition: disposition, Amount: moneyMessage(p.Amount, p.Currency), Unit: p.Unit, SourceDocumentRef: p.SourceRef, Revision: s.Revision}
	if p.QuantitySpecified {
		out.Quantity = decimalMessage(p.Quantity)
	}
	return out
}
func decimalMessage(d values.Decimal) *commonv1.Decimal {
	text := d.String()
	neg := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(text, "-")
	parts := strings.SplitN(text, ".", 2)
	digits := parts[0]
	scale := int32(0)
	if len(parts) == 2 {
		digits += parts[1]
		scale = int32(len(parts[1]))
	}
	mag, _ := new(big.Int).SetString(digits, 10)
	sign := commonv1.DecimalSign_DECIMAL_SIGN_POSITIVE
	if mag == nil || mag.Sign() == 0 {
		sign = commonv1.DecimalSign_DECIMAL_SIGN_ZERO
		mag = big.NewInt(0)
	} else if neg {
		sign = commonv1.DecimalSign_DECIMAL_SIGN_NEGATIVE
	}
	return &commonv1.Decimal{Sign: sign, UnscaledMagnitude: mag.Bytes(), Scale: scale}
}

func moneyMessage(amount values.Decimal, currency string) *commonv1.Money {
	if amount.Validate() != nil {
		return nil
	}
	return &commonv1.Money{Amount: decimalMessage(amount), CurrencyCode: currency}
}

func (s *server) ListWorkOrders(ctx context.Context, req *workorderv1.ListWorkOrdersRequest) (*workorderv1.ListWorkOrdersResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.available(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, invalid()
	}
	projectID, err := scopeContext(req.GetScopeContext(), p)
	if err != nil {
		return nil, err
	}
	projectID = req.GetProjectId()
	if strings.TrimSpace(projectID) == "" {
		return nil, invalid()
	}
	limit, err := pageSize(req.GetPage())
	if err != nil {
		return nil, err
	}
	statusFilter := ""
	switch req.GetStatus() {
	case workorderv1.WorkOrderStatus_WORK_ORDER_STATUS_DRAFT:
		statusFilter = "DRAFT"
	case workorderv1.WorkOrderStatus_WORK_ORDER_STATUS_ACTIVE:
		statusFilter = "ACTIVE"
	case workorderv1.WorkOrderStatus_WORK_ORDER_STATUS_BLOCKED:
		statusFilter = "BLOCKED"
	case workorderv1.WorkOrderStatus_WORK_ORDER_STATUS_ACCEPTED:
		statusFilter = "ACCEPTED"
	case workorderv1.WorkOrderStatus_WORK_ORDER_STATUS_CLOSED:
		statusFilter = "CLOSED"
	case workorderv1.WorkOrderStatus_WORK_ORDER_STATUS_CANCELLED:
		statusFilter = "CANCELLED"
	}
	page, err := s.service.List(ctx, p, workorderservice.ListRequest{ProjectID: projectID, Status: statusFilter, Cursor: req.GetPage().GetCursor(), Limit: limit})
	if err != nil {
		return nil, mapError(err)
	}
	out := &workorderv1.ListWorkOrdersResponse{WorkOrders: make([]*workorderv1.WorkOrder, 0, len(page.Orders)), Page: &commonv1.PageResponse{NextCursor: page.NextCursor}}
	for _, order := range page.Orders {
		out.WorkOrders = append(out.WorkOrders, workOrderMessage(order))
	}
	return out, nil
}

func (s *server) RecordWorkEntry(ctx context.Context, req *workorderv1.RecordWorkEntryRequest) (*workorderv1.RecordWorkEntryResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.available(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, invalid()
	}
	scope, err := workOrderScope(req.GetScopeContext(), p, req.GetWorkOrderId(), req.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	d := req.GetWorkDate()
	if d == nil || d.GetYear() < 1 || d.GetMonth() < 1 || d.GetMonth() > 12 || d.GetDay() < 1 || d.GetDay() > 31 {
		return nil, invalid()
	}
	date := time.Date(int(d.GetYear()), time.Month(d.GetMonth()), int(d.GetDay()), 0, 0, 0, 0, time.UTC)
	if date.Year() != int(d.GetYear()) || int(date.Month()) != int(d.GetMonth()) || date.Day() != int(d.GetDay()) {
		return nil, invalid()
	}
	minutes, err := durationMinutes(req.GetKind(), req.GetDurationSeconds())
	if err != nil {
		return nil, invalid()
	}
	qty, err := decimalValue(req.GetQuantity())
	if err != nil {
		return nil, invalid()
	}
	kind := workEntryKind(req.GetKind())
	if kind == "" {
		return nil, invalid()
	}
	result, err := s.service.RecordWorkEntry(ctx, p, workorderservice.WorkEntryRequest{ScopedRequest: scope, ExpectedRevision: req.GetExpectedRevision(), Input: domain.WorkEntryInput{WorkerID: req.GetWorkerId(), Kind: kind, WorkDate: date.Format("2006-01-02"), TimeZone: req.GetTimezone(), DurationMinutes: minutes, LineID: req.GetLineId(), Quantity: qty, QuantitySpecified: req.GetQuantity() != nil, Unit: req.GetUnit(), Description: req.GetDescription(), SourceRef: req.GetSourceRef(), CorrectionOfEntryID: req.GetCorrectionOfEntryId(), ExpectedRevision: req.GetExpectedRevision(), IdempotencyKey: req.GetIdempotencyKey()}})
	if err != nil {
		return nil, mapError(err)
	}
	e := result.Entry
	entry := workEntryMessage(e, result.Order.ID, result.Order.Revision)
	return &workorderv1.RecordWorkEntryResponse{WorkOrder: workOrderMessage(result.Order), Entry: entry}, nil
}

func workEntryMessage(e domain.WorkEntry, orderID string, revision uint64) *workorderv1.WorkEntry {
	entry := &workorderv1.WorkEntry{Id: e.ID, WorkOrderId: orderID, WorkerId: e.WorkerID, Kind: protoWorkEntryKind(e.Kind), Timezone: e.TimeZone, DurationSeconds: int64(e.DurationMinutes) * 60, LineId: e.LineID, Quantity: decimalMessage(e.Quantity), Unit: e.Unit, Description: e.Description, SourceRef: e.SourceRef, CorrectionOfEntryId: e.CorrectionOfEntryID, Revision: revision}
	parts := strings.Split(e.WorkDate, "-")
	if len(parts) == 3 {
		y, _ := strconv.Atoi(parts[0])
		m, _ := strconv.Atoi(parts[1])
		day, _ := strconv.Atoi(parts[2])
		entry.WorkDate = &commonv1.LocalDate{Year: int32(y), Month: int32(m), Day: int32(day)}
	}
	return entry
}

func (s *server) RequestWorkOrderReport(ctx context.Context, req *workorderv1.RequestWorkOrderReportRequest) (*workorderv1.RequestWorkOrderReportResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.available(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, invalid()
	}
	scope, err := workOrderScope(req.GetScopeContext(), p, req.GetWorkOrderId(), req.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	r, err := s.service.RequestReport(ctx, p, workorderservice.ReportRequest{ScopedRequest: scope, ExpectedRevision: req.GetExpectedRevision(), Kind: reportKind(req.GetKind()), DefinitionID: req.GetDefinitionId(), DefinitionVersion: req.GetDefinitionVersion()})
	if err != nil {
		return nil, mapError(err)
	}
	return &workorderv1.RequestWorkOrderReportResponse{Report: &workorderv1.WorkOrderReport{Id: r.Artifact.ID, WorkOrderId: scope.WorkOrderID, Kind: req.GetKind(), DefinitionId: r.Report.Definition.ID, DefinitionVersion: r.Report.Definition.Version, SourceCutoffRevision: r.Artifact.SourceRevision, CompletenessState: r.Report.Status, ArtifactRef: r.Artifact.ID, GeneratedAt: timestamppb.New(r.Artifact.CreatedAt)}}, nil
}

func (s *server) RequestBillingDraft(ctx context.Context, req *workorderv1.RequestBillingDraftRequest) (*workorderv1.RequestBillingDraftResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.available(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, invalid()
	}
	scope, err := workOrderScope(req.GetScopeContext(), p, req.GetWorkOrderId(), req.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	start, end := req.GetPeriodStart(), req.GetPeriodEnd()
	if start == nil || end == nil || !start.IsValid() || !end.IsValid() || !end.AsTime().After(start.AsTime()) {
		return nil, invalid()
	}
	draft, err := s.service.RequestBillingDraft(ctx, p, workorderservice.BillingDraftRequest{ScopedRequest: scope, ExpectedRevision: req.GetExpectedRevision(), ContractVersion: req.GetContractVersion(), PeriodStart: start.AsTime(), PeriodEnd: end.AsTime()})
	if err != nil {
		return nil, mapError(err)
	}
	return &workorderv1.RequestBillingDraftResponse{Draft: billingMessage(draft, scope.WorkOrderID)}, nil
}

func reportKind(k workorderv1.ReportKind) string {
	switch k {
	case workorderv1.ReportKind_REPORT_KIND_DAILY_FIELD:
		return workorderreport.DailyField
	case workorderv1.ReportKind_REPORT_KIND_QUANTITY_PROGRESS:
		return workorderreport.Progress
	case workorderv1.ReportKind_REPORT_KIND_COST_VARIANCE:
		return workorderreport.Cost
	case workorderv1.ReportKind_REPORT_KIND_OPEN_REQUESTS:
		return workorderreport.OpenRequests
	case workorderv1.ReportKind_REPORT_KIND_CLOSEOUT_EVIDENCE:
		return workorderreport.Closeout
	}
	return ""
}
func workEntryKind(k workorderv1.WorkEntryKind) string {
	switch k {
	case workorderv1.WorkEntryKind_WORK_ENTRY_KIND_LABOR:
		return "LABOR"
	case workorderv1.WorkEntryKind_WORK_ENTRY_KIND_MATERIAL:
		return "MATERIAL"
	case workorderv1.WorkEntryKind_WORK_ENTRY_KIND_EQUIPMENT:
		return "EQUIPMENT"
	case workorderv1.WorkEntryKind_WORK_ENTRY_KIND_SUBCONTRACT:
		return "SUBCONTRACT"
	case workorderv1.WorkEntryKind_WORK_ENTRY_KIND_OTHER:
		return "OTHER"
	}
	return ""
}
func protoWorkEntryKind(k string) workorderv1.WorkEntryKind {
	switch k {
	case "LABOR":
		return workorderv1.WorkEntryKind_WORK_ENTRY_KIND_LABOR
	case "MATERIAL":
		return workorderv1.WorkEntryKind_WORK_ENTRY_KIND_MATERIAL
	case "EQUIPMENT":
		return workorderv1.WorkEntryKind_WORK_ENTRY_KIND_EQUIPMENT
	case "SUBCONTRACT":
		return workorderv1.WorkEntryKind_WORK_ENTRY_KIND_SUBCONTRACT
	case "OTHER":
		return workorderv1.WorkEntryKind_WORK_ENTRY_KIND_OTHER
	}
	return workorderv1.WorkEntryKind_WORK_ENTRY_KIND_UNSPECIFIED
}
func spendDisposition(k workorderv1.SpendDisposition) (domain.SpendDisposition, error) {
	switch k {
	case workorderv1.SpendDisposition_SPEND_DISPOSITION_COMMITMENT:
		return domain.SpendCommitment, nil
	case workorderv1.SpendDisposition_SPEND_DISPOSITION_INCURRED:
		return domain.SpendIncurred, nil
	default:
		return "", errors.New("spend disposition is required")
	}
}
func durationMinutes(kind workorderv1.WorkEntryKind, seconds int64) (uint32, error) {
	if seconds < 0 || (seconds > 0 && seconds%60 != 0) || seconds/60 > int64(^uint32(0)) || (kind == workorderv1.WorkEntryKind_WORK_ENTRY_KIND_LABOR && seconds == 0) {
		return 0, errors.New("duration must be whole minutes and labor duration must be positive")
	}
	return uint32(seconds / 60), nil
}
func billingMessage(result workorderservice.BillingDraftResult, orderID string) *workorderv1.BillingDraft {
	d := result.Draft
	out := &workorderv1.BillingDraft{Id: d.ID, WorkOrderId: orderID, ContractVersion: d.PolicyVersion}
	out.SourceCutoffRevision = result.SourceRevision
	out.GeneratedAt = timestamppb.New(result.Artifact.CreatedAt)
	switch d.Status {
	case workorderbilling.Approved:
		out.Status = workorderv1.BillingDraftStatus_BILLING_DRAFT_STATUS_APPROVED
	case workorderbilling.Rejected:
		out.Status = workorderv1.BillingDraftStatus_BILLING_DRAFT_STATUS_REJECTED
	default:
		out.Status = workorderv1.BillingDraftStatus_BILLING_DRAFT_STATUS_PENDING_REVIEW
	}
	out.Lines = make([]*workorderv1.BillingDraftLine, 0, len(d.Lines))
	out.Total = moneyMessage(d.Total.Amount(), d.Total.Currency())
	for _, line := range d.Lines {
		out.Lines = append(out.Lines, &workorderv1.BillingDraftLine{Id: line.ID, Description: line.Description, Quantity: decimalMessage(line.Quantity), Unit: line.Unit, Amount: moneyMessage(line.Amount.Amount(), line.Amount.Currency()), SourceEntryIds: []string{line.SourceID}, PricingRuleRef: line.PolicyID})
	}
	return out
}

// NewConnectHandler mounts every generated RPC through the same handlers used
// by gRPC, preserving trusted-context admission and owned error projection.
func NewConnectHandler(deps Dependencies, opts ...connect.HandlerOption) http.Handler {
	s := &server{service: deps.Service}
	mux := http.NewServeMux()
	mountGeneric(mux, CreateWorkOrderProcedure, s.CreateWorkOrder, opts...)
	mountGeneric(mux, GetWorkOrderProcedure, s.GetWorkOrder, opts...)
	mountGeneric(mux, ListWorkOrdersProcedure, s.ListWorkOrders, opts...)
	mountGeneric(mux, SubmitInitiatorRequestProcedure, s.SubmitInitiatorRequest, opts...)
	mountGeneric(mux, DecideInitiatorRequestProcedure, s.DecideInitiatorRequest, opts...)
	mountGeneric(mux, AddWorkOrderNoteProcedure, s.AddWorkOrderNote, opts...)
	mountGeneric(mux, RequestPhaseTransitionProcedure, s.RequestPhaseTransition, opts...)
	mountGeneric(mux, RecordWorkEntryProcedure, s.RecordWorkEntry, opts...)
	mountGeneric(mux, RecordProgressEntryProcedure, s.RecordProgressEntry, opts...)
	mountGeneric(mux, RecordSpendEntryProcedure, s.RecordSpendEntry, opts...)
	mountGeneric(mux, RequestWorkOrderReportProcedure, s.RequestWorkOrderReport, opts...)
	mountGeneric(mux, RequestBillingDraftProcedure, s.RequestBillingDraft, opts...)
	return mux
}

func mountGeneric[Req, Resp any](mux *http.ServeMux, path string, handler func(context.Context, *Req) (*Resp, error), opts ...connect.HandlerOption) {
	mux.Handle(path, connect.NewUnaryHandler(path, func(ctx context.Context, req *connect.Request[Req]) (*connect.Response[Resp], error) {
		out, err := handler(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(out), nil
	}, opts...))
}
