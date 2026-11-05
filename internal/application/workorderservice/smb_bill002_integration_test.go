package workorderservice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorder"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderbilling"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workordertemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func bill002UnitPolicy(t *testing.T) workorderbilling.PricingPolicy {
	t.Helper()
	rate, err := values.NewDecimal("90", 0, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return workorderbilling.PricingPolicy{ID: "contract-a", Version: "v1", Currency: "USD", Mode: workorderbilling.PricingUnitPrice, AmountScale: 2, Rounding: values.RoundingExactRequired, UnitRates: map[string]values.Decimal{"hour": rate}}
}

func bill002TMPolicy(t *testing.T) workorderbilling.PricingPolicy {
	t.Helper()
	mult, err := values.NewDecimal("1.20", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return workorderbilling.PricingPolicy{ID: "contract-tm", Version: "v2", Currency: "USD", Mode: workorderbilling.PricingTimeMaterial, AmountScale: 2, Rounding: values.RoundingExactRequired, TMMultipliers: map[string]values.Decimal{"recharge": mult}}
}

func bill002Fixture(t *testing.T) *workorderbilling.SourceClaimRegistry {
	t.Helper()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	registry := workorderbilling.NewSourceClaimRegistry(func() time.Time { return now })
	op := workorderbilling.BillOp{TenantID: "tenant-a", LegalEntityID: "le-1", ActorID: "billing-setup", BillingAllowed: true}
	if err := registry.RegisterRate(op, workorderbilling.BillRateInput{Ref: "rate:USD90/hour", Currency: "USD", Amount: "90", Unit: "hour"}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.RegisterSource(op, workorderbilling.BillSourceInput{
		Owner: "JOB007", RootID: "alloc-7", Revision: 1,
		WorkOrderID: "wo-1", JobRef: "job-9",
		Kind: workorderbilling.BillKindTime, UnitDimension: "time", Unit: "minute",
		ApprovedQty: "120", RateRef: "rate:USD90/hour",
		PricingMode: workorderbilling.PricingUnitPrice, BillableUnit: "hour",
		EvidenceRef: "alloc-appr-7", ApprovedBy: "supervisor-2", ApprovedAt: "2026-09-20T15:04:05Z",
		Description: "Field labor, Sep 15", ServiceDate: "2026-09-15",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.RegisterExpense(op, workorderbilling.BillExpenseInput{
		Owner: "BILL010", RootID: "exp-50", WorkOrderID: "wo-1", JobRef: "job-9",
		CostAmount: "50.00 USD", RechargeRate: "0.20", RechargeEligible: true,
		ReimbursementAmount: "50.00 USD",
		EvidenceRef:         "bill010-appr-4", RevisionBinding: "bill010-rev-4",
		ApprovedBy: "controller-1", ApprovedAt: "2026-09-20T16:00:00Z",
		Description: "Parts run", ServiceDate: "2026-09-15", Category: "recharge",
	}); err != nil {
		t.Fatal(err)
	}
	return registry
}

func bill002TMPublished(t *testing.T) workordertemplate.Published {
	t.Helper()
	role := "WORK_ORDER_ACTOR"
	draft := workordertemplate.Draft{ID: "RIVERSIDE", Name: "Test work order", Phases: []workordertemplate.Phase{
		{ID: "DRAFT", AllowedExits: []string{"AUTHORIZATION"}, ActorRoles: []string{role}},
		{ID: "AUTHORIZATION", AllowedExits: []string{"READY"}, ActorRoles: []string{role}, Gates: []workordertemplate.Gate{{ID: "SCOPE", Kind: "AUTHORIZATION", Required: true}}},
		{ID: "READY", AllowedExits: []string{"EXECUTION"}, ActorRoles: []string{role}, Gates: []workordertemplate.Gate{{ID: "SAFETY", Kind: "SAFETY", Required: true}}},
		{ID: "EXECUTION", AllowedExits: []string{"INSPECTION"}, ActorRoles: []string{role}},
		{ID: "INSPECTION", AllowedExits: []string{"ACCEPTED"}, ActorRoles: []string{role}, Gates: []workordertemplate.Gate{{ID: "RECONCILE", Kind: "RECONCILIATION", Required: true}}},
		{ID: "ACCEPTED", AllowedExits: []string{"CLOSED"}, ActorRoles: []string{role}},
		{ID: "CLOSED", ActorRoles: []string{role}, Gates: []workordertemplate.Gate{{ID: "CLOSE", Kind: "CLOSURE", Required: true}}},
	}, Roles: []workordertemplate.RoleGrant{{Role: role, Actions: []string{"create", "request"}}}, Billing: &workordertemplate.BillingPolicy{ID: "contract-tm", Version: "v2", Mode: "TIME_AND_MATERIAL"}}
	published, err := workordertemplate.Publish(draft, workordertemplate.PublishMeta{Version: "1.0.0", PublishedBy: "test-owner", ReviewRef: "test-review"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return published
}

// TestTodo_SMB_BILL_002_Integration drives the real RequestBillingDraft
// orchestration with production-shaped adapters: the approved 120-minute
// source plus pinned USD 90/hour pricing resolves to a durable USD 180
// artifact, replays idempotently, conflicts on changed payload, fails closed
// without either port, and binds drafts to claims so consumed sources stop
// billing. The BILL010 expense leg resolves to exactly USD 60 through the
// time/material path.
func TestTodo_SMB_BILL_002_Integration(t *testing.T) {
	registry := bill002Fixture(t)
	book := NewBill002PolicyBook()
	book.Add("tenant-a", "wo-1", "v1", bill002UnitPolicy(t))
	book.Add("tenant-a", "wo-1", "v2", bill002TMPolicy(t))
	unitSources := NewBill002SourceAdapter(registry, "le-1", "billing-service", workorderbilling.PricingUnitPrice)
	tmSources := NewBill002SourceAdapter(registry, "le-1", "billing-service", workorderbilling.PricingTimeMaterial)

	snapshot, templates := testSnapshotWithPublishedTemplate(t)
	svc := Service{Auth: &testAuthorizer{}, Orders: &testOrders{snapshot: snapshot}, Templates: templates, Pricing: book, BillingSources: unitSources, Artifacts: &extrasArtifacts{}}
	req := BillingDraftRequest{ScopedRequest: ScopedRequest{WorkOrderID: "wo-1", IdempotencyKey: "bill002-1"}, ExpectedRevision: 1, ContractVersion: "v1", PeriodStart: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), PeriodEnd: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	got, err := svc.RequestBillingDraft(context.Background(), servicePrincipal(t), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Draft.Total.Amount().String() != "180.00" || len(got.Draft.Lines) != 1 || got.SourceRevision != 1 || got.Artifact.Kind != "BILLING" {
		t.Fatalf("billing draft = %+v, want durable USD 180 artifact", got)
	}
	if got.Draft.Lines[0].SourceRevision != "1" || got.Draft.Lines[0].Quantity.String() != "2" {
		t.Fatalf("draft line = %+v, want rev 1 / 2 hours", got.Draft.Lines[0])
	}
	replay, err := svc.RequestBillingDraft(context.Background(), servicePrincipal(t), req)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Artifact.ID != got.Artifact.ID || replay.Draft.Total.String() != got.Draft.Total.String() {
		t.Fatalf("replay changed artifact: first=%+v replay=%+v", got, replay)
	}
	changed := req
	changed.ExpectedRevision = 2
	if _, err := svc.RequestBillingDraft(context.Background(), servicePrincipal(t), changed); !errors.Is(err, workorder.ErrConflict) {
		t.Fatalf("changed payload err = %v, want conflict", err)
	}

	bare := svc
	bare.Pricing = nil
	if _, err := bare.RequestBillingDraft(context.Background(), servicePrincipal(t), req); !errors.Is(err, ErrArtifactUnavailable) {
		t.Fatalf("nil pricing err = %v, want ErrArtifactUnavailable", err)
	}
	bare.Pricing = book
	bare.BillingSources = nil
	if _, err := bare.RequestBillingDraft(context.Background(), servicePrincipal(t), req); !errors.Is(err, ErrArtifactUnavailable) {
		t.Fatalf("nil sources err = %v, want ErrArtifactUnavailable", err)
	}

	op := workorderbilling.BillOp{TenantID: "tenant-a", LegalEntityID: "le-1", ActorID: "biller-1", BillingAllowed: true}
	if _, err := registry.Claim(op, workorderbilling.BillClaimInput{
		FamilyID: "JOB007:alloc-7", Revision: 1, Qty: "120",
		PolicyID: "contract-a", PolicyVersion: "v1", Kind: workorderbilling.BillClaimIssue, DraftRef: got.Draft.ID,
	}, "bind-1"); err != nil {
		t.Fatal(err)
	}
	empty := req
	empty.IdempotencyKey = "bill002-2"
	if _, err := svc.RequestBillingDraft(context.Background(), servicePrincipal(t), empty); err == nil || !strings.Contains(err.Error(), "at least one billable source") {
		t.Fatalf("consumed rebuild err = %v, want fail-closed no-source error", err)
	}
	foreign, err := registry.QuerySources(op, "wo-2", req.PeriodStart, req.PeriodEnd)
	if err != nil || len(foreign) != 0 {
		t.Fatalf("cross-job query = %+v err=%v, want empty, never foreign sources", foreign, err)
	}

	tmPublished := bill002TMPublished(t)
	tmSnapshot := testSnapshotForTemplate(t, tmPublished)
	tmSvc := Service{Auth: &testAuthorizer{}, Orders: &testOrders{snapshot: tmSnapshot}, Templates: extrasTemplates{published: tmPublished}, Pricing: book, BillingSources: tmSources, Artifacts: &extrasArtifacts{}}
	tmReq := BillingDraftRequest{ScopedRequest: ScopedRequest{WorkOrderID: "wo-1", IdempotencyKey: "bill002-tm"}, ExpectedRevision: 1, ContractVersion: "v2", PeriodStart: req.PeriodStart, PeriodEnd: req.PeriodEnd}
	tmGot, err := tmSvc.RequestBillingDraft(context.Background(), servicePrincipal(t), tmReq)
	if err != nil {
		t.Fatal(err)
	}
	if tmGot.Draft.Total.Amount().String() != "60.00" || len(tmGot.Draft.Lines) != 1 {
		t.Fatalf("expense draft = %+v, want exactly USD 60.00", tmGot)
	}
	expenseReceipt, err := registry.ClaimExpense(op, "exp:BILL010:exp-50", true, "contract-tm", "v2", "bind-exp")
	if err != nil {
		t.Fatal(err)
	}
	if expenseReceipt.ClaimedAmount != "60.00 USD" || expenseReceipt.RemainingAmount != "0.00 USD" {
		t.Fatalf("expense receipt = %+v, want 60.00 claimed / 0.00 left", expenseReceipt)
	}
	parallel := tmReq
	parallel.IdempotencyKey = "bill002-tm-2"
	if _, err := tmSvc.RequestBillingDraft(context.Background(), servicePrincipal(t), parallel); err == nil || !strings.Contains(err.Error(), "at least one billable source") {
		t.Fatalf("parallel expense draft err = %v, want fail-closed, never a second 60", err)
	}
}

// TestBill002ExpenseExclusion proves the exclude leg: an explicit exclusion
// records a zero claim, leaves the customer total unclaimed and keeps the
// worker reimbursement eligibility untouched.
func TestBill002ExpenseExclusion(t *testing.T) {
	registry := bill002Fixture(t)
	op := workorderbilling.BillOp{TenantID: "tenant-a", LegalEntityID: "le-1", ActorID: "biller-1", BillingAllowed: true}
	excluded, err := registry.ClaimExpense(op, "exp:BILL010:exp-50", false, "contract-tm", "v2", "excl-1")
	if err != nil {
		t.Fatal(err)
	}
	if excluded.ClaimedAmount != "0.00 USD" || excluded.RemainingAmount != "60.00 USD" || excluded.Status != "excluded" {
		t.Fatalf("exclusion = %+v, want zero claim with 60.00 still available", excluded)
	}
	again, err := registry.ClaimExpense(op, "exp:BILL010:exp-50", false, "contract-tm", "v2", "excl-1")
	if err != nil || again.ClaimID != excluded.ClaimID {
		t.Fatalf("exclusion replay = %+v err=%v, want same receipt", again, err)
	}
}
