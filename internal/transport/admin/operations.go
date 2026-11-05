package admin

import (
	"context"
	"errors"
	"maps"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/authorizedhealth"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/productcorrelation"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/repairworkbench"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// operationsServer adapts Dependencies to the generated
// adminv1.OperationsServiceServer interface (REV-087-01). Every method is a
// thin authenticated envelope over one pure operations package: health
// projection, the repair workbench, one governed repair submit, and
// telemetry correlation. requireOperator runs first, the tenant always
// comes from the verified principal (never from the scope message, which
// admission already reconciled and overwrote server-side), and the
// remaining lines convert the wire shape, call the package, and map its
// typed result back. No method here decides a freshness bound, an
// admission, a sanitizer drop or a gateway refusal; those stay in the
// packages, and their sentinels map to owned codes in [operationsError].
type operationsServer struct {
	adminv1.UnimplementedOperationsServiceServer

	deps Dependencies
}

// RegisterOperations adds hcmnext.admin.v1.OperationsService to srv under
// the same trusted-request interceptor chain as AdminService.
func RegisterOperations(srv *grpc.Server, deps Dependencies) {
	adminv1.RegisterOperationsServiceServer(srv, &operationsServer{deps: deps})
}

func (s *operationsServer) now() time.Time {
	if s.deps.Now != nil {
		return s.deps.Now().UTC()
	}
	return time.Now().UTC()
}

// operationsError maps the operations packages' sentinels and the
// operator gateway's refusal codes onto owned conditions. An unrecognized
// failure coerces to a generic UNAVAILABLE with the original nested as an
// unprojected diagnostic, and every constructed message passes the
// envelope safety screen, so a storage-shaped gateway cause can never
// cross the wire as its own text.
func operationsError(principal *trust.Principal, inv *transport.Invocation, err error) error {
	base := envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"}
	owned := func(code envelope.Code, reason string) error {
		return envelope.New(code, reason, err.Error()).
			WithCorrelation(inv.RequestID()).WithEvidence(base)
	}
	switch {
	case errors.Is(err, authorizedhealth.ErrUnauthorized) ||
		errors.Is(err, repairworkbench.ErrUnauthorized) ||
		errors.Is(err, productcorrelation.ErrUnauthorized):
		return owned(envelope.CodePermissionDenied, "admin.operations_denied")
	case errors.Is(err, repairworkbench.ErrNotInView):
		return owned(envelope.CodeNotFound, "admin.operations_repair_not_found")
	case errors.Is(err, repairworkbench.ErrStaleEvidence):
		return owned(envelope.CodeAborted, "admin.operations_repair_stale")
	case errors.Is(err, repairworkbench.ErrActionRefused):
		return owned(envelope.CodeFailedPrecondition, "admin.operations_action_refused")
	case errors.Is(err, authorizedhealth.ErrInvalidInput) ||
		errors.Is(err, repairworkbench.ErrInvalid) ||
		errors.Is(err, productcorrelation.ErrInvalid):
		return owned(envelope.CodeInvalidArgument, "admin.operations_invalid")
	}
	switch operator.CodeOf(err) {
	case operator.CodeInvalidRequest, operator.CodeUnknownKind:
		return owned(envelope.CodeInvalidArgument, "admin.operations_gateway_invalid")
	case operator.CodeAuthorityRequired, operator.CodeAuthorityMismatch,
		operator.CodeAuthorityInactive, operator.CodeDualControlRequired,
		operator.CodeSimulationRequired, operator.CodeBypassReason,
		operator.CodeUnauthorizedEffect:
		return owned(envelope.CodeFailedPrecondition, "admin.operations_gateway_refused")
	case operator.CodeIdempotencyConflict:
		return owned(envelope.CodeAborted, "admin.operations_gateway_conflict")
	case operator.CodeRepairRequired:
		return owned(envelope.CodeFailedPrecondition, "admin.operations_gateway_repair_required")
	case operator.CodeNoExecutor, operator.CodeJournal:
		return owned(envelope.CodeUnavailable, "admin.operations_gateway_unavailable")
	default:
		return envelope.Coerce(err)
	}
}

// operationsInstant converts a required wire timestamp. A nil timestamp
// must fail here rather than convert: (*timestamppb.Timestamp)(nil).AsTime
// reads as the 1970 epoch through nil-safe getters, which would silently
// reclassify a missing observation instant as maximally stale evidence.
func operationsInstant(ts *timestamppb.Timestamp) (time.Time, *envelope.Error) {
	if ts == nil {
		return time.Time{}, envelope.New(envelope.CodeInvalidArgument,
			"admin.operations_missing_instant",
			"the observation carries no instant")
	}
	return ts.AsTime(), nil
}

// operationsOptionalInstant converts a timestamp the package treats as
// meaningful only alongside other evidence (the outbox oldest-pending
// mark): nil stays the zero time so the package's pending-gated UNKNOWN
// branch still applies.
func operationsOptionalInstant(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}

// GetOperationsHealth projects caller-supplied freshness observations for
// the operator's own tenant through authorizedhealth.Project. The request
// clock defaults to the server clock when the request pins no observed_at;
// every observation keeps its own instant and bound.
func (s *operationsServer) GetOperationsHealth(ctx context.Context, req *adminv1.GetOperationsHealthRequest) (*adminv1.GetOperationsHealthResponse, error) {
	principal, inv, opErr := requireOperator(ctx)
	if opErr != nil {
		return nil, opErr
	}
	now := s.now()
	if ts := req.GetObservedAt(); ts != nil {
		now = ts.AsTime()
	}
	if now.IsZero() {
		return nil, envelope.New(envelope.CodeInvalidArgument,
			"admin.operations_missing_instant",
			"the request carries no observation time").
			WithCorrelation(inv.RequestID()).
			WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
	}

	projections := make([]authorizedhealth.ProjectionObservation, 0, len(req.GetProjections()))
	for _, pb := range req.GetProjections() {
		observedAt, tErr := operationsInstant(pb.GetObservedAt())
		if tErr != nil {
			return nil, tErr.WithCorrelation(inv.RequestID()).
				WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
		}
		projections = append(projections, authorizedhealth.ProjectionObservation{
			Name: pb.GetName(), SchemaVersion: pb.GetSchemaVersion(), DefinitionVersion: pb.GetDefinitionVersion(),
			SourceSequence: pb.GetSourceSequence(), AppliedSequence: pb.GetAppliedSequence(),
			ObservedAt: observedAt, MaxAge: time.Duration(pb.GetMaxAgeSeconds()) * time.Second,
			Status: pb.GetStatus(),
		})
	}
	workflows := make([]authorizedhealth.WorkflowObservation, 0, len(req.GetWorkflows()))
	for _, pb := range req.GetWorkflows() {
		observedAt, tErr := operationsInstant(pb.GetObservedAt())
		if tErr != nil {
			return nil, tErr.WithCorrelation(inv.RequestID()).
				WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
		}
		workflows = append(workflows, authorizedhealth.WorkflowObservation{
			Name: pb.GetName(), SchemaVersion: pb.GetSchemaVersion(), DefinitionVersion: pb.GetDefinitionVersion(),
			ActiveRuns: pb.GetActiveRuns(), FailedRuns: pb.GetFailedRuns(), BlockedRuns: pb.GetBlockedRuns(),
			PendingWorkItems: pb.GetPendingWorkItems(), OverdueWorkItems: pb.GetOverdueWorkItems(),
			ObservedAt: observedAt, MaxAge: time.Duration(pb.GetMaxAgeSeconds()) * time.Second,
			Status: pb.GetStatus(),
		})
	}
	var outbox *authorizedhealth.OutboxObservation
	if pb := req.GetOutbox(); pb != nil {
		observedAt, tErr := operationsInstant(pb.GetObservedAt())
		if tErr != nil {
			return nil, tErr.WithCorrelation(inv.RequestID()).
				WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
		}
		outbox = &authorizedhealth.OutboxObservation{
			SchemaVersion: pb.GetSchemaVersion(),
			Pending:       pb.GetPending(), InFlight: pb.GetInFlight(), Failed: pb.GetFailed(), Abandoned: pb.GetAbandoned(),
			OldestPendingAt: operationsOptionalInstant(pb.GetOldestPendingAt()),
			ObservedAt:      observedAt,
			MaxAge:          time.Duration(pb.GetMaxAgeSeconds()) * time.Second,
			MaxPendingAge:   time.Duration(pb.GetMaxPendingAgeSeconds()) * time.Second,
			Status:          pb.GetStatus(),
		}
	}
	var reconciliation *authorizedhealth.ReconciliationObservation
	if pb := req.GetReconciliation(); pb != nil {
		observedAt, tErr := operationsInstant(pb.GetObservedAt())
		if tErr != nil {
			return nil, tErr.WithCorrelation(inv.RequestID()).
				WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
		}
		reconciliation = &authorizedhealth.ReconciliationObservation{
			SchemaVersion: pb.GetSchemaVersion(),
			Pending:       pb.GetPending(), Observing: pb.GetObserving(), Mismatch: pb.GetMismatch(), RepairRequired: pb.GetRepairRequired(),
			ObservedAt: observedAt, MaxAge: time.Duration(pb.GetMaxAgeSeconds()) * time.Second,
			Status: pb.GetStatus(),
		}
	}

	view, err := authorizedhealth.Project(principal, authorizedhealth.Request{
		Tenant: principal.Tenant(), Now: now,
		Projections: projections, Workflows: workflows,
		Outbox: outbox, Reconciliation: reconciliation,
	})
	if err != nil {
		return nil, operationsError(principal, inv, err)
	}

	resp := &adminv1.GetOperationsHealthResponse{
		PolicyVersion: view.PolicyVersion, Purpose: view.Purpose, EvidenceId: view.EvidenceID,
		ObservedAt: timestamppb.New(view.ObservedAt),
		State:      string(view.State), Digest: view.Digest(),
		EvidenceRef: evidenceRef(principal, "admin.operations.get_operations_health"),
	}
	for _, p := range view.Projections {
		resp.Projections = append(resp.Projections, &adminv1.ProjectionHealthView{
			Name: p.Name, SchemaVersion: p.SchemaVersion, DefinitionVersion: p.DefinitionVersion,
			SourceSequence: p.SourceSequence, AppliedSequence: p.AppliedSequence, Lag: p.Lag,
			Freshness: string(p.Freshness), State: string(p.State), Status: p.Status,
		})
	}
	for _, w := range view.Workflows {
		resp.Workflows = append(resp.Workflows, &adminv1.WorkflowHealthView{
			Name: w.Name, SchemaVersion: w.SchemaVersion, DefinitionVersion: w.DefinitionVersion,
			ActiveRuns: w.ActiveRuns, FailedRuns: w.FailedRuns, BlockedRuns: w.BlockedRuns,
			PendingWorkItems: w.PendingWorkItems, OverdueWorkItems: w.OverdueWorkItems,
			Freshness: string(w.Freshness), State: string(w.State), Status: w.Status,
		})
	}
	if view.Outbox != nil {
		resp.Outbox = &adminv1.OutboxHealthView{
			SchemaVersion: view.Outbox.SchemaVersion,
			Pending:       view.Outbox.Pending, InFlight: view.Outbox.InFlight,
			Failed: view.Outbox.Failed, Abandoned: view.Outbox.Abandoned,
			OldestPendingAgeSeconds: int64(view.Outbox.OldestPendingAge / time.Second),
			Freshness:               string(view.Outbox.Freshness), State: string(view.Outbox.State), Status: view.Outbox.Status,
		}
	}
	if view.Reconciliation != nil {
		resp.Reconciliation = &adminv1.ReconciliationHealthView{
			SchemaVersion: view.Reconciliation.SchemaVersion,
			Pending:       view.Reconciliation.Pending, Observing: view.Reconciliation.Observing,
			Mismatch: view.Reconciliation.Mismatch, RepairRequired: view.Reconciliation.RepairRequired,
			Freshness: string(view.Reconciliation.Freshness), State: string(view.Reconciliation.State), Status: view.Reconciliation.Status,
		}
	}
	return resp, nil
}

// repairFindingsFromProto converts the wire findings Open and Submit both
// re-check. Conversion is mechanical: the closed kind/severity vocabulary
// and every completeness rule stay in repairworkbench.validate, which
// reports them as ErrInvalid.
func repairFindingsFromProto(principal *trust.Principal, inv *transport.Invocation, pbs []*adminv1.RepairFindingInput) ([]repairworkbench.Finding, error) {
	findings := make([]repairworkbench.Finding, 0, len(pbs))
	for _, pb := range pbs {
		observedAt, tErr := operationsInstant(pb.GetObservedAt())
		if tErr != nil {
			return nil, tErr.WithCorrelation(inv.RequestID()).
				WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
		}
		findings = append(findings, repairworkbench.Finding{
			ID: pb.GetId(), Slice: pb.GetSlice(), Tenant: values.TenantId(pb.GetTenant()),
			Kind: repairworkbench.FindingKind(pb.GetKind()), Severity: repairworkbench.Severity(pb.GetSeverity()),
			Resource: pb.GetResource(), TargetRefs: append([]string(nil), pb.GetTargetRefs()...),
			EvidenceRef: pb.GetEvidenceRef(), ObservedAt: observedAt,
		})
	}
	return findings, nil
}

// OpenRepairWorkbench opens the bounded, tenant-scoped repair queue for
// one product slice through repairworkbench.Open.
func (s *operationsServer) OpenRepairWorkbench(ctx context.Context, req *adminv1.OpenRepairWorkbenchRequest) (*adminv1.OpenRepairWorkbenchResponse, error) {
	principal, inv, opErr := requireOperator(ctx)
	if opErr != nil {
		return nil, opErr
	}
	findings, err := repairFindingsFromProto(principal, inv, req.GetFindings())
	if err != nil {
		return nil, err
	}
	view, err := repairworkbench.Open(principal, req.GetSlice(), findings)
	if err != nil {
		return nil, operationsError(principal, inv, err)
	}
	resp := &adminv1.OpenRepairWorkbenchResponse{
		ContractVersion: int64(view.ContractVersion),
		Tenant:          string(view.Tenant), Slice: view.Slice,
		Truncated: view.Truncated, Digest: view.Digest(),
		EvidenceRef: evidenceRef(principal, "admin.operations.open_repair_workbench"),
	}
	for _, item := range view.Items {
		actions := make([]string, 0, len(item.Actions))
		for _, a := range item.Actions {
			actions = append(actions, string(a))
		}
		resp.Items = append(resp.Items, &adminv1.RepairWorkbenchItem{
			FindingId: item.FindingID, Kind: string(item.Kind), Severity: string(item.Severity),
			Resource: item.Resource, TargetCount: int64(item.TargetCount),
			EvidenceDigest: item.EvidenceDigest, Actions: actions,
		})
	}
	return resp, nil
}

// SubmitRepairAction re-checks one repair against the operator's current
// view of the findings and forwards it to the governed operator gateway
// through repairworkbench.Submit. The wire carries no governance of its
// own: the gateway applies its JIT authority, dual control, simulation
// and idempotency rules to the forwarded request and its refusal codes
// map back through [operationsError].
func (s *operationsServer) SubmitRepairAction(ctx context.Context, req *adminv1.SubmitRepairActionRequest) (*adminv1.SubmitRepairActionResponse, error) {
	principal, inv, opErr := requireOperator(ctx)
	if opErr != nil {
		return nil, opErr
	}
	if s.deps.RepairSubmitter == nil {
		return nil, envelope.New(envelope.CodeUnavailable,
			"admin.operations_repair_unconfigured",
			"the repair gateway port is not configured").
			WithCorrelation(inv.RequestID()).
			WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
	}
	findings, err := repairFindingsFromProto(principal, inv, req.GetFindings())
	if err != nil {
		return nil, err
	}
	receipt, err := repairworkbench.Submit(ctx, s.deps.RepairSubmitter, principal, req.GetSlice(), findings, repairworkbench.Action{
		FindingID: req.GetFindingId(), EvidenceDigest: req.GetEvidenceDigest(),
		Kind:           operator.Kind(req.GetKind()),
		IdempotencyKey: req.GetIdempotencyKey(), Reason: req.GetReason(), TicketRef: req.GetTicketRef(),
	})
	if err != nil {
		return nil, operationsError(principal, inv, err)
	}
	return &adminv1.SubmitRepairActionResponse{
		IntentInstanceId: receipt.IntentInstanceID, IntentType: receipt.IntentType,
		Kind: string(receipt.Kind), Tenant: string(receipt.Tenant), Operator: receipt.Operator,
		ScopeResource: receipt.Scope.Resource, ScopeIds: append([]string(nil), receipt.Scope.IDs...),
		IdempotencyKey: receipt.IdempotencyKey, RequestDigest: receipt.RequestDigest,
		Reason: receipt.Reason, TicketRef: receipt.TicketRef,
		AuthorityKind: receipt.AuthorityKind, AuthorityRef: receipt.AuthorityRef,
		Outcome: string(receipt.Outcome), EffectRef: receipt.EffectRef,
		EvidenceRef: evidenceRef(principal, "admin.operations.submit_repair_action"),
	}, nil
}

// CorrelateProductTelemetry reconstructs product-action timelines from raw
// surface events through productcorrelation.Correlate, sanitized against
// the configured telemetry allowlist (or the compiled-in default).
func (s *operationsServer) CorrelateProductTelemetry(ctx context.Context, req *adminv1.CorrelateProductTelemetryRequest) (*adminv1.CorrelateProductTelemetryResponse, error) {
	principal, inv, opErr := requireOperator(ctx)
	if opErr != nil {
		return nil, opErr
	}
	allow := s.deps.CorrelationAllowlist
	if allow == nil {
		def, err := telemetry.DefaultAllowlist()
		if err != nil {
			return nil, envelope.New(envelope.CodeUnavailable,
				"admin.operations_telemetry_unavailable",
				"the telemetry allowlist could not be loaded").WithDiagnostic(err).
				WithCorrelation(inv.RequestID()).
				WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
		}
		allow = def
	}
	events := make([]productcorrelation.Event, 0, len(req.GetEvents()))
	for _, pb := range req.GetEvents() {
		at, tErr := operationsInstant(pb.GetAt())
		if tErr != nil {
			return nil, tErr.WithCorrelation(inv.RequestID()).
				WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
		}
		events = append(events, productcorrelation.Event{
			Surface: productcorrelation.Surface(pb.GetSurface()), Name: pb.GetName(),
			Tenant:        values.TenantId(pb.GetTenant()),
			CorrelationID: pb.GetCorrelationId(), TraceID: pb.GetTraceId(), At: at,
			Attributes: maps.Clone(pb.GetAttributes()),
		})
	}
	report, err := productcorrelation.Correlate(principal, allow, events)
	if err != nil {
		return nil, operationsError(principal, inv, err)
	}
	resp := &adminv1.CorrelateProductTelemetryResponse{
		Tenant: string(report.Tenant), Digest: report.Digest(),
		EvidenceRef: evidenceRef(principal, "admin.operations.correlate_product_telemetry"),
	}
	for _, tl := range report.Timelines {
		wire := &adminv1.CorrelationTimeline{
			CorrelationId: tl.CorrelationID, Complete: tl.Complete,
		}
		for _, step := range tl.Steps {
			wire.Steps = append(wire.Steps, &adminv1.CorrelationStep{
				Surface: string(step.Surface), Name: step.Name, At: timestamppb.New(step.At),
				TraceId: step.TraceID, Attributes: maps.Clone(step.Attributes),
			})
		}
		for _, surface := range tl.Surfaces {
			wire.Surfaces = append(wire.Surfaces, string(surface))
		}
		resp.Timelines = append(resp.Timelines, wire)
	}
	if len(report.Dropped) > 0 {
		resp.Dropped = make(map[string]int64, len(report.Dropped))
		for reason, count := range report.Dropped {
			resp.Dropped[reason] = int64(count)
		}
	}
	return resp, nil
}
