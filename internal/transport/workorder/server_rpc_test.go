package workorder

import (
	"context"
	"testing"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	workorderv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workorder/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/application/workorderservice"
	domain "github.com/monstercameron/human-capital-management-suite/internal/domains/workorder"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderbilling"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderreport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// rpcService is intentionally a recording application boundary: these tests
// exercise transport conversions and response projection without duplicating
// application business rules.
type rpcService struct {
	workorderservice.Service
	snapshot  domain.Snapshot
	submitted workorderservice.InitiatorRequest
	created   workorderservice.CreateRequest
	listed    workorderservice.ListRequest
	workEntry workorderservice.WorkEntryRequest
	spend     workorderservice.SpendRequest
	progress  workorderservice.ProgressRequest
	err       error
}

func (f *rpcService) Create(_ context.Context, _ *trust.Principal, r workorderservice.CreateRequest) (domain.Snapshot, error) {
	f.created = r
	return f.snapshot, f.err
}
func (f *rpcService) Get(context.Context, *trust.Principal, workorderservice.ScopedRequest) (domain.Snapshot, error) {
	return f.snapshot, f.err
}
func (f *rpcService) SubmitRequest(_ context.Context, _ *trust.Principal, r workorderservice.InitiatorRequest) (domain.Snapshot, error) {
	f.submitted = r
	s := f.snapshot
	s.Requests = append(append([]domain.InitiatorRequest(nil), s.Requests...), domain.InitiatorRequest{ID: "submitted", Kind: r.Input.Kind, Status: domain.RequestPending, Subject: r.Input.Subject, RoleOrItem: r.Input.RoleOrItem, Rationale: r.Input.Rationale, Amount: r.Input.Amount, Currency: r.Input.Currency, CostCategory: r.Input.CostCategory, FundingSource: r.Input.FundingSource, BaselineRevision: r.Input.BaselineRevision, Quantity: r.Input.Quantity, Unit: r.Input.Unit, NeededUntil: r.Input.NeededUntil, EstimatedCost: r.Input.EstimatedCost, EstimatedCostSpecified: r.Input.EstimatedCostSpecified, EstimatedCostCurrency: r.Input.EstimatedCostCurrency, QuantityDelta: r.Input.QuantityDelta, QuantityDeltaUnit: r.Input.QuantityDeltaUnit, PriceDelta: r.Input.PriceDelta, PriceDeltaSpecified: r.Input.PriceDeltaSpecified, PriceDeltaCurrency: r.Input.PriceDeltaCurrency, ScheduleDelta: r.Input.ScheduleDelta, ScopeDelta: r.Input.ScopeDelta, PricingVersion: r.Input.PricingVersion, BillingPeriod: r.Input.BillingPeriod, Policy: r.Input.Policy, ApproverClass: r.Input.ApproverClass, ProposedAction: r.Input.ProposedAction, DueWindow: r.Input.DueWindow, EvidenceRefs: append([]string(nil), r.Input.EvidenceRefs...)})
	return s, f.err
}
func (f *rpcService) DecideRequest(context.Context, *trust.Principal, workorderservice.DecisionRequest) (domain.Snapshot, error) {
	return f.snapshot, f.err
}
func (f *rpcService) AddNote(context.Context, *trust.Principal, workorderservice.NoteRequest) (domain.Snapshot, error) {
	return f.snapshot, f.err
}
func (f *rpcService) Transition(context.Context, *trust.Principal, workorderservice.TransitionRequest) (domain.Snapshot, error) {
	return f.snapshot, f.err
}
func (f *rpcService) Assign(context.Context, *trust.Principal, workorderservice.AssignmentRequest) (domain.Snapshot, error) {
	return f.snapshot, f.err
}
func (f *rpcService) RecordProgress(_ context.Context, _ *trust.Principal, r workorderservice.ProgressRequest) (domain.Snapshot, error) {
	f.progress = r
	return f.snapshot, f.err
}
func (f *rpcService) RecordSpend(_ context.Context, _ *trust.Principal, r workorderservice.SpendRequest) (domain.Snapshot, error) {
	f.spend = r
	s := f.snapshot
	s.Spending = append(append([]domain.Spend(nil), s.Spending...), domain.Spend{ID: "spend-new", Category: r.Input.Category, Disposition: r.Input.Disposition, Amount: r.Input.Amount, Currency: r.Input.Currency, Quantity: r.Input.Quantity, QuantitySpecified: r.Input.QuantitySpecified, Unit: r.Input.Unit, SourceRef: r.Input.SourceRef})
	return s, f.err
}
func (f *rpcService) List(_ context.Context, _ *trust.Principal, r workorderservice.ListRequest) (workorderservice.ListResult, error) {
	f.listed = r
	return workorderservice.ListResult{Orders: []domain.Snapshot{f.snapshot}, NextCursor: "next"}, f.err
}
func (f *rpcService) RecordWorkEntry(_ context.Context, _ *trust.Principal, r workorderservice.WorkEntryRequest) (workorderservice.WorkEntryResult, error) {
	f.workEntry = r
	e := domain.WorkEntry{ID: "entry-1", WorkerID: r.Input.WorkerID, Kind: r.Input.Kind, WorkDate: r.Input.WorkDate, TimeZone: r.Input.TimeZone, DurationMinutes: r.Input.DurationMinutes, Description: r.Input.Description, Quantity: r.Input.Quantity, QuantitySpecified: r.Input.QuantitySpecified}
	return workorderservice.WorkEntryResult{Order: f.snapshot, Entry: e}, f.err
}
func (f *rpcService) RequestReport(context.Context, *trust.Principal, workorderservice.ReportRequest) (workorderservice.ReportResult, error) {
	return workorderservice.ReportResult{Report: workorderreport.Result{Definition: workorderreport.Definition{ID: "daily", Version: 2}, Status: "complete"}, Artifact: workorderservice.ArtifactRecord{ID: "artifact-1", SourceRevision: 4, CreatedAt: testTime()}}, f.err
}
func (f *rpcService) RequestBillingDraft(context.Context, *trust.Principal, workorderservice.BillingDraftRequest) (workorderservice.BillingDraftResult, error) {
	amount, _ := values.NewMoney("10.00", "USD", 2, values.RoundingExactRequired)
	qty, _ := values.NewDecimal("2", 0, values.RoundingExactRequired)
	d := workorderbilling.BillingDraft{ID: "draft-1", PolicyVersion: "contract-v2", Status: workorderbilling.Draft, Total: amount, Lines: []workorderbilling.Line{{ID: "line-1", Description: "accepted work", Quantity: qty, Unit: "hour", Amount: amount, SourceID: "entry-1", PolicyID: "rule-1"}}}
	return workorderservice.BillingDraftResult{Draft: d, SourceRevision: 4, Artifact: workorderservice.ArtifactRecord{CreatedAt: testTime()}}, f.err
}

func TestAllWorkOrderRPCsConvertAndProject(t *testing.T) {
	amount := wireMoney("12.50", "USD")
	qty := wireDecimal("2.5")
	snap := domain.Snapshot{ID: "wo-1", ProjectID: "project-1", Title: "Repair", Scope: "North gate", TemplateID: "field", TemplateVersion: "2.1.0", Phase: domain.PhaseExecution, Revision: 4, InitiatorID: "actor", SupervisorID: "supervisor", Journal: []domain.Event{{At: testTime()}}, Notes: []domain.Note{{ID: "note-1", AuthorID: "actor", Body: "update", Visibility: "PARTICIPANTS", Classification: "general", Anchor: "EXECUTION/line-1", At: testTime(), AttachmentRefs: []string{"e-1"}}}, Progress: []domain.Progress{{ID: "progress-1", LineID: "line-1", Quantity: mustDecimal(t, "2"), Unit: "hour", EvidenceRef: "e-1"}}, Spending: []domain.Spend{{ID: "spend-1", Category: "MATERIAL", Disposition: domain.SpendCommitment, Amount: mustDecimal(t, "12.50"), Currency: "USD", Quantity: mustDecimal(t, "1"), QuantitySpecified: true, Unit: "piece"}}}
	fake := &rpcService{snapshot: snap}
	srv := &server{service: fake}
	ctx := admittedTestContext(t)
	scope := &commonv1.ScopeContext{TenantId: "tenant-test"}
	date := &commonv1.LocalDate{Year: 2026, Month: 9, Day: 25}

	t.Run("create", func(t *testing.T) {
		got, err := srv.CreateWorkOrder(ctx, &workorderv1.CreateWorkOrderRequest{ScopeContext: scope, ProjectId: "project-1", Title: "Repair", Scope: "North gate", TemplateId: "field", TemplateVersion: "2.1.0", SupervisorId: "supervisor", LinkedTaskIds: []string{"task-1"}, IdempotencyKey: "create-1"})
		if err != nil || got.GetWorkOrder().GetTitle() != "Repair" || fake.created.TemplateVersion != "2.1.0" || len(fake.created.LinkedTaskIDs) != 1 {
			t.Fatalf("create=(%v,%v) input=%+v", got, err, fake.created)
		}
	})
	t.Run("get", func(t *testing.T) {
		got, err := srv.GetWorkOrder(ctx, &workorderv1.GetWorkOrderRequest{ScopeContext: scope, WorkOrderId: "wo-1"})
		if err != nil || got.GetWorkOrder().GetId() != "wo-1" {
			t.Fatalf("get=(%v,%v)", got, err)
		}
	})
	t.Run("list", func(t *testing.T) {
		got, err := srv.ListWorkOrders(ctx, &workorderv1.ListWorkOrdersRequest{ScopeContext: scope, ProjectId: "project-1", Status: workorderv1.WorkOrderStatus_WORK_ORDER_STATUS_ACTIVE, Page: &commonv1.PageRequest{PageSize: 9, Cursor: "cursor"}})
		if err != nil || len(got.GetWorkOrders()) != 1 || got.GetPage().GetNextCursor() != "next" || fake.listed.Limit != 9 || fake.listed.Status != "ACTIVE" {
			t.Fatalf("list=(%v,%v) input=%+v", got, err, fake.listed)
		}
	})
	t.Run("submit budget", func(t *testing.T) {
		detail := &workorderv1.BudgetRequest{RequestedLimit: amount, CostCategory: "materials", FundingSource: "capital", Rationale: "repair"}
		got, err := srv.SubmitInitiatorRequest(ctx, &workorderv1.SubmitInitiatorRequestRequest{ScopeContext: scope, WorkOrderId: "wo-1", ExpectedRevision: 4, Kind: workorderv1.InitiatorRequestKind_INITIATOR_REQUEST_KIND_BUDGET, Details: &workorderv1.SubmitInitiatorRequestRequest_Budget{Budget: detail}, FormValues: map[string]string{"drawing_revision": "A"}, IdempotencyKey: "req-1"})
		if err != nil || got.GetRequest().GetBudget().GetRequestedLimit().GetCurrencyCode() != "USD" || fake.submitted.Input.FormValues["drawing_revision"] != "A" {
			t.Fatalf("submit=(%v,%v) input=%+v", got, err, fake.submitted.Input)
		}
	})
	t.Run("submit approval", func(t *testing.T) {
		got, err := srv.SubmitInitiatorRequest(ctx, &workorderv1.SubmitInitiatorRequestRequest{ScopeContext: scope, WorkOrderId: "wo-1", ExpectedRevision: 4, Kind: workorderv1.InitiatorRequestKind_INITIATOR_REQUEST_KIND_APPROVAL, Details: &workorderv1.SubmitInitiatorRequestRequest_Approval{Approval: &workorderv1.ApprovalRequest{Subject: "Permit", ProposedAction: "proceed", ApproverClass: "manager", DueAt: timestamp(testTime()), Evidence: evidence("e-1")}}, IdempotencyKey: "req-2"})
		if err != nil || got.GetRequest().GetApproval().GetSubject() != "Permit" {
			t.Fatalf("submit approval=(%v,%v)", got, err)
		}
	})
	t.Run("submit resource", func(t *testing.T) {
		got, err := srv.SubmitInitiatorRequest(ctx, &workorderv1.SubmitInitiatorRequestRequest{ScopeContext: scope, WorkOrderId: "wo-1", ExpectedRevision: 4, Kind: workorderv1.InitiatorRequestKind_INITIATOR_REQUEST_KIND_MATERIAL, Details: &workorderv1.SubmitInitiatorRequestRequest_Resource{Resource: &workorderv1.ResourceRequest{RoleOrItem: "stringer", Quantity: qty, Unit: "piece", EstimatedCost: amount, NeededFrom: timestamp(testTime()), NeededUntil: timestamp(testTime().Add(time.Hour)), Evidence: evidence("e-1")}}, IdempotencyKey: "req-3"})
		if err != nil || got.GetRequest().GetResource().GetRoleOrItem() != "stringer" || !fake.submitted.Input.EstimatedCostSpecified {
			t.Fatalf("submit resource=(%v,%v)", got, err)
		}
	})
	t.Run("submit inspection", func(t *testing.T) {
		got, err := srv.SubmitInitiatorRequest(ctx, &workorderv1.SubmitInitiatorRequestRequest{ScopeContext: scope, WorkOrderId: "wo-1", ExpectedRevision: 4, Kind: workorderv1.InitiatorRequestKind_INITIATOR_REQUEST_KIND_INSPECTION, Details: &workorderv1.SubmitInitiatorRequestRequest_Inspection{Inspection: &workorderv1.InspectionRequest{InspectionScope: "welds", EvidencePolicyRef: "policy-1", DueAt: timestamp(testTime()), Evidence: evidence("e-1")}}, IdempotencyKey: "req-4"})
		if err != nil || got.GetRequest().GetInspection().GetInspectionScope() != "welds" {
			t.Fatalf("submit inspection=(%v,%v)", got, err)
		}
	})
	t.Run("submit change", func(t *testing.T) {
		got, err := srv.SubmitInitiatorRequest(ctx, &workorderv1.SubmitInitiatorRequestRequest{ScopeContext: scope, WorkOrderId: "wo-1", ExpectedRevision: 4, Kind: workorderv1.InitiatorRequestKind_INITIATOR_REQUEST_KIND_CHANGE_ORDER, Details: &workorderv1.SubmitInitiatorRequestRequest_ChangeOrder{ChangeOrder: &workorderv1.ChangeOrderRequest{ScopeDelta: "extra rail", QuantityDelta: qty, Unit: "meter", PriceDelta: amount, ScheduleDeltaSeconds: -60, BaselineRevision: 2}}, IdempotencyKey: "req-5"})
		if err != nil || got.GetRequest().GetChangeOrder().GetUnit() != "meter" || !fake.submitted.Input.PriceDeltaSpecified {
			t.Fatalf("submit change=(%v,%v)", got, err)
		}
	})
	t.Run("submit billing review", func(t *testing.T) {
		got, err := srv.SubmitInitiatorRequest(ctx, &workorderv1.SubmitInitiatorRequestRequest{ScopeContext: scope, WorkOrderId: "wo-1", ExpectedRevision: 4, Kind: workorderv1.InitiatorRequestKind_INITIATOR_REQUEST_KIND_BILLING_REVIEW, Details: &workorderv1.SubmitInitiatorRequestRequest_BillingReview{BillingReview: &workorderv1.BillingReviewRequest{ContractVersion: "v1", PeriodStart: timestamp(testTime()), PeriodEnd: timestamp(testTime().Add(24 * time.Hour)), SourceCutoffRevision: 3}}, IdempotencyKey: "req-6"})
		if err != nil || got.GetRequest().GetBillingReview().GetContractVersion() != "v1" {
			t.Fatalf("submit billing=(%v,%v)", got, err)
		}
	})
	t.Run("submit document", func(t *testing.T) {
		got, err := srv.SubmitInitiatorRequest(ctx, &workorderv1.SubmitInitiatorRequestRequest{ScopeContext: scope, WorkOrderId: "wo-1", ExpectedRevision: 4, Kind: workorderv1.InitiatorRequestKind_INITIATOR_REQUEST_KIND_DOCUMENT, Details: &workorderv1.SubmitInitiatorRequestRequest_Document{Document: &workorderv1.DocumentRequest{DocumentKind: "invoice", EvidencePolicyRef: "evidence-policy", Evidence: evidence("e-1")}}, IdempotencyKey: "req-7"})
		if err != nil || got.GetRequest().GetDocument().GetDocumentKind() != "invoice" {
			t.Fatalf("submit document=(%v,%v)", got, err)
		}
	})
	t.Run("decide", func(t *testing.T) {
		got, err := srv.DecideInitiatorRequest(ctx, &workorderv1.DecideInitiatorRequestRequest{ScopeContext: scope, WorkOrderId: "wo-1", RequestId: "req-1", ExpectedRevision: 4, Decision: workorderv1.DecisionKind_DECISION_KIND_APPROVE, Reason: "approved", IdempotencyKey: "decision-1"})
		if err != nil || got.GetWorkOrder().GetId() != "wo-1" {
			t.Fatalf("decide=(%v,%v)", got, err)
		}
	})
	t.Run("note", func(t *testing.T) {
		got, err := srv.AddWorkOrderNote(ctx, &workorderv1.AddWorkOrderNoteRequest{ScopeContext: scope, WorkOrderId: "wo-1", ExpectedRevision: 4, Body: "update", Classification: "general", Visibility: workorderv1.NoteVisibility_NOTE_VISIBILITY_PARTICIPANTS, Attachments: evidence("e-1"), IdempotencyKey: "note-1"})
		if err != nil || got.GetNote().GetBody() != "update" {
			t.Fatalf("note=(%v,%v)", got, err)
		}
	})
	t.Run("phase", func(t *testing.T) {
		got, err := srv.RequestPhaseTransition(ctx, &workorderv1.RequestPhaseTransitionRequest{ScopeContext: scope, WorkOrderId: "wo-1", ExpectedRevision: 4, TargetPhaseId: "CLOSED", Evidence: evidence("e-1"), IdempotencyKey: "phase-1"})
		if err != nil || !got.GetResult().GetApplied() {
			t.Fatalf("phase=(%v,%v)", got, err)
		}
	})
	t.Run("work entry", func(t *testing.T) {
		got, err := srv.RecordWorkEntry(ctx, &workorderv1.RecordWorkEntryRequest{ScopeContext: scope, WorkOrderId: "wo-1", ExpectedRevision: 4, WorkerId: "worker-1", Kind: workorderv1.WorkEntryKind_WORK_ENTRY_KIND_OTHER, WorkDate: date, Timezone: "America/New_York", DurationSeconds: 1800, Description: "site support", Quantity: qty, Unit: "hour", IdempotencyKey: "work-1"})
		if err != nil || got.GetEntry().GetDescription() != "site support" || fake.workEntry.Input.DurationMinutes != 30 {
			t.Fatalf("work entry=(%v,%v) input=%+v", got, err, fake.workEntry.Input)
		}
	})
	t.Run("progress", func(t *testing.T) {
		got, err := srv.RecordProgressEntry(ctx, &workorderv1.RecordProgressEntryRequest{ScopeContext: scope, WorkOrderId: "wo-1", ExpectedRevision: 4, LineId: "line-1", CompletedQuantity: qty, Unit: "hour", Evidence: evidence("e-1"), IdempotencyKey: "progress-1"})
		if err != nil || got.GetEntry().GetCompletedQuantity() == nil || len(got.GetEntry().GetEvidence()) != 1 {
			t.Fatalf("progress=(%v,%v)", got, err)
		}
	})
	t.Run("spend", func(t *testing.T) {
		got, err := srv.RecordSpendEntry(ctx, &workorderv1.RecordSpendEntryRequest{ScopeContext: scope, WorkOrderId: "wo-1", ExpectedRevision: 4, Category: workorderv1.WorkEntryKind_WORK_ENTRY_KIND_MATERIAL, Disposition: workorderv1.SpendDisposition_SPEND_DISPOSITION_INCURRED, Amount: amount, Quantity: qty, Unit: "piece", IdempotencyKey: "spend-1"})
		if err != nil || got.GetEntry().GetDisposition() != workorderv1.SpendDisposition_SPEND_DISPOSITION_INCURRED || !fake.spend.Input.QuantitySpecified {
			t.Fatalf("spend=(%v,%v)", got, err)
		}
	})
	t.Run("report", func(t *testing.T) {
		got, err := srv.RequestWorkOrderReport(ctx, &workorderv1.RequestWorkOrderReportRequest{ScopeContext: scope, WorkOrderId: "wo-1", ExpectedRevision: 4, Kind: workorderv1.ReportKind_REPORT_KIND_DAILY_FIELD, DefinitionId: "daily", DefinitionVersion: 2, IdempotencyKey: "report-1"})
		if err != nil || got.GetReport().GetArtifactRef() != "artifact-1" || got.GetReport().GetDefinitionVersion() != 2 {
			t.Fatalf("report=(%v,%v)", got, err)
		}
	})
	t.Run("billing", func(t *testing.T) {
		got, err := srv.RequestBillingDraft(ctx, &workorderv1.RequestBillingDraftRequest{ScopeContext: scope, WorkOrderId: "wo-1", ExpectedRevision: 4, ContractVersion: "contract-v2", PeriodStart: timestamp(testTime()), PeriodEnd: timestamp(testTime().Add(24 * time.Hour)), IdempotencyKey: "billing-1"})
		if err != nil || got.GetDraft().GetId() != "draft-1" || got.GetDraft().GetTotal().GetAmount() == nil || len(got.GetDraft().GetLines()) != 1 {
			t.Fatalf("billing=(%v,%v)", got, err)
		}
	})
}

func TestTransportRejectsMalformedInputsBeforeApplication(t *testing.T) {
	fake := &rpcService{}
	srv := &server{service: fake}
	ctx := admittedTestContext(t)
	_, err := srv.SubmitInitiatorRequest(ctx, &workorderv1.SubmitInitiatorRequestRequest{ScopeContext: &commonv1.ScopeContext{TenantId: "tenant-test"}, WorkOrderId: "wo-1", Kind: workorderv1.InitiatorRequestKind_INITIATOR_REQUEST_KIND_APPROVAL, Details: &workorderv1.SubmitInitiatorRequestRequest_Budget{Budget: &workorderv1.BudgetRequest{RequestedLimit: wireMoney("1", "USD")}}})
	if err == nil || fake.submitted.Input.Kind != "" {
		t.Fatalf("mismatched detail reached application: err=%v input=%+v", err, fake.submitted)
	}
	_, err = srv.RecordWorkEntry(ctx, &workorderv1.RecordWorkEntryRequest{ScopeContext: &commonv1.ScopeContext{TenantId: "tenant-test"}, WorkOrderId: "wo-1", Kind: workorderv1.WorkEntryKind_WORK_ENTRY_KIND_LABOR, WorkDate: &commonv1.LocalDate{Year: 2026, Month: 2, Day: 30}, DurationSeconds: 60})
	if err == nil || fake.workEntry.Input.WorkerID != "" {
		t.Fatalf("invalid date reached application: %v", err)
	}
	_, err = srv.RecordSpendEntry(ctx, &workorderv1.RecordSpendEntryRequest{ScopeContext: &commonv1.ScopeContext{TenantId: "tenant-test"}, WorkOrderId: "wo-1", Amount: wireMoney("1", "USD"), Category: workorderv1.WorkEntryKind_WORK_ENTRY_KIND_MATERIAL})
	if err == nil {
		t.Fatal("spend with unspecified disposition accepted")
	}
}

func admittedTestContext(t *testing.T) context.Context {
	t.Helper()
	now := time.Now().UTC()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-test"), Subject: "actor-1", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	return transport.WithInvocation(trust.WithPrincipal(context.Background(), p), &transport.Invocation{})
}

func testTime() time.Time { return time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC) }
func wireDecimal(text string) *commonv1.Decimal {
	d, err := values.NewDecimal(text, int32(decimalScale(text)), values.RoundingExactRequired)
	if err != nil {
		panic(err)
	}
	return decimalMessage(d)
}
func wireMoney(text, currency string) *commonv1.Money {
	return &commonv1.Money{Amount: wireDecimal(text), CurrencyCode: currency}
}
func mustDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, int32(decimalScale(text)), values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func decimalScale(text string) int {
	for i, r := range text {
		if r == '.' {
			return len(text) - i - 1
		}
	}
	return 0
}
func timestamp(t time.Time) *timestamppb.Timestamp { return timestamppb.New(t) }
func evidence(ids ...string) []*commonv1.EvidenceRef {
	out := make([]*commonv1.EvidenceRef, 0, len(ids))
	for _, id := range ids {
		out = append(out, &commonv1.EvidenceRef{EvidenceId: id})
	}
	return out
}
