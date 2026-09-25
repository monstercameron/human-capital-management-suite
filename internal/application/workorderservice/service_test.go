package workorderservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorder"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workordertemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type testAuthorizer struct {
	denied     bool
	deniedCaps map[workorderaccess.Capability]bool
	calls      int
	project    string
	cap        workorderaccess.Capability
}

type testWorkers struct {
	worker          string
	found, eligible bool
}

type testPhaseGate struct {
	err   error
	calls int
}

func (g *testPhaseGate) EvaluateTransition(_ context.Context, _ *trust.Principal, _ workorder.Snapshot, _ workorder.TransitionInput) error {
	g.calls++
	return g.err
}

func (w testWorkers) ResolveWorker(context.Context, string, string) (string, bool, error) {
	return w.worker, w.found, nil
}
func (w testWorkers) ResolveEligible(context.Context, string, string) (bool, error) {
	return w.eligible, nil
}

func (a *testAuthorizer) Authorize(_ context.Context, _ *trust.Principal, project, _ string, cap workorderaccess.Capability) error {
	a.calls++
	a.project = project
	a.cap = cap
	if a.denied || a.deniedCaps[cap] {
		return errors.New("denied")
	}
	return nil
}
func (a *testAuthorizer) AuthorizeCreate(context.Context, *trust.Principal, string) error { return nil }
func (a *testAuthorizer) AuthorizeList(context.Context, *trust.Principal, string) error   { return nil }

type testOrders struct {
	snapshot                workorder.Snapshot
	gets, creates, executes int
	receipts                map[string]workorder.Snapshot
	digests                 map[string]string
}

type testTemplates struct{ published workordertemplate.Published }

func (t testTemplates) ResolvePublished(_ context.Context, _, id, version string) (workordertemplate.Published, error) {
	if t.published.Pin().TemplateID != id || t.published.Version() != version {
		return workordertemplate.Published{}, workordertemplate.ErrInvalid
	}
	return t.published, nil
}
func createTemplate(t *testing.T) workordertemplate.Published {
	t.Helper()
	draft := workordertemplate.Draft{ID: "RIVERSIDE", Name: "Riverside field work", Phases: []workordertemplate.Phase{
		{ID: "DRAFT", AllowedExits: []string{"AUTHORIZATION"}, ActorRoles: []string{"INITIATOR"}},
		{ID: "AUTHORIZATION", AllowedExits: []string{"READY"}, ActorRoles: []string{"SUPERVISOR"}, Gates: []workordertemplate.Gate{{ID: "SCOPE_APPROVAL", Kind: "AUTHORIZATION", Required: true}, {ID: "SAFETY_REVIEW", Kind: "SAFETY", Required: true}}},
		{ID: "READY", AllowedExits: []string{"EXECUTION"}, ActorRoles: []string{"SUPERVISOR"}},
		{ID: "EXECUTION", AllowedExits: []string{"INSPECTION"}, ActorRoles: []string{"CREW"}},
		{ID: "INSPECTION", AllowedExits: []string{"ACCEPTED"}, ActorRoles: []string{"INSPECTOR"}, Gates: []workordertemplate.Gate{{ID: "RECONCILE", Kind: "RECONCILIATION", Required: true}}},
		{ID: "ACCEPTED", AllowedExits: []string{"CLOSED"}, ActorRoles: []string{"SUPERVISOR"}},
		{ID: "CLOSED", ActorRoles: []string{"SUPERVISOR"}, Gates: []workordertemplate.Gate{{ID: "CLOSEOUT", Kind: "CLOSURE", Required: true}}},
	}, Forms: []workordertemplate.RequestForm{{ID: "BUDGET_FORM", Version: "1", Fields: []workordertemplate.FormField{{ID: "amount", Type: workordertemplate.FieldDecimal, Required: true}, {ID: "reason", Type: workordertemplate.FieldText, Required: true}}}}, Requests: []workordertemplate.RequestDefinition{{ID: "INITIAL_BUDGET", Kind: workordertemplate.RequestBudget, FormRef: "BUDGET_FORM", AllowedPhases: []string{"DRAFT", "AUTHORIZATION"}}}, Roles: []workordertemplate.RoleGrant{{Role: "INITIATOR", Actions: []string{"create", "request"}}, {Role: "SUPERVISOR", Actions: []string{"advance", "approve"}}, {Role: "CREW", Actions: []string{"log_work"}}, {Role: "INSPECTOR", Actions: []string{"inspect"}}}, ReportPolicies: []workordertemplate.ReportPolicy{{ID: "DAILY_FIELD", Version: "1", Kind: workordertemplate.ReportDailyField, Required: true}}, Billing: &workordertemplate.BillingPolicy{ID: "UNIT_BILLING", Version: "1", Mode: "UNIT_PRICE"}}
	pub, err := workordertemplate.Publish(draft, workordertemplate.PublishMeta{Version: "1.0.0", PublishedBy: "alice", ReviewRef: "review"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

func (r *testOrders) Create(_ context.Context, _ string, s workorder.Snapshot, _, key string) error {
	r.creates++
	if prior, ok := r.receipts[key]; ok {
		if prior.ID != s.ID {
			return workorder.ErrConflict
		}
		return nil
	}
	if r.receipts == nil {
		r.receipts = map[string]workorder.Snapshot{}
	}
	r.receipts[key] = s
	r.snapshot = s
	return nil
}
func (r *testOrders) Get(_ context.Context, tenant, id string) (workorder.Snapshot, error) {
	r.gets++
	if tenant != r.snapshot.TenantID || id != r.snapshot.ID {
		return workorder.Snapshot{}, workorder.ErrNotFound
	}
	return r.snapshot, nil
}
func (r *testOrders) Execute(_ context.Context, tenant, id, _, key string, expected uint64, digest string, mutate func(workorder.Snapshot) (workorder.Snapshot, error)) (workorder.Snapshot, error) {
	r.executes++
	if tenant != r.snapshot.TenantID || id != r.snapshot.ID {
		return workorder.Snapshot{}, workorder.ErrNotFound
	}
	if r.receipts == nil {
		r.receipts = map[string]workorder.Snapshot{}
		r.digests = map[string]string{}
	}
	if prior, ok := r.receipts[key]; ok {
		if r.digests[key] != digest {
			return workorder.Snapshot{}, workorder.ErrConflict
		}
		return prior, nil
	}
	if expected != r.snapshot.Revision {
		return workorder.Snapshot{}, workorder.ErrStale
	}
	next, err := mutate(r.snapshot)
	if err != nil {
		return workorder.Snapshot{}, err
	}
	if next.Revision != expected+1 {
		return workorder.Snapshot{}, workorder.ErrStale
	}
	r.snapshot = next
	r.receipts[key] = next
	r.digests[key] = digest
	return next, nil
}

func servicePrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	now := time.Now()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "alice", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session-a", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "credential"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func testSnapshot(t *testing.T) workorder.Snapshot {
	t.Helper()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	agg, err := workorder.NewWorkOrder(workorder.CreateInput{ID: "wo-1", TenantID: "tenant-a", ProjectID: "project-a", TemplateID: "RIVERSIDE", TemplateVersion: "1", TemplateDigest: "sha256:template", ActorID: "alice", Now: now, IdempotencyKey: "create-1", InitialPhase: workorder.PhaseDraft})
	if err != nil {
		t.Fatal(err)
	}
	return agg.Snapshot()
}

func testSnapshotForTemplate(t *testing.T, pub workordertemplate.Published) workorder.Snapshot {
	t.Helper()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	agg, err := workorder.NewWorkOrder(workorder.CreateInput{ID: "wo-1", TenantID: "tenant-a", ProjectID: "project-a", TemplateID: pub.Pin().TemplateID, TemplateVersion: pub.Version(), TemplateDigest: pub.Digest(), ActorID: "alice", Now: now, IdempotencyKey: "create-1", InitialPhase: workorder.PhaseDraft})
	if err != nil {
		t.Fatal(err)
	}
	return agg.Snapshot()
}

func TestGetDerivesProjectThenAuthorizesExactTenantScope(t *testing.T) {
	store := &testOrders{snapshot: testSnapshot(t)}
	auth := &testAuthorizer{}
	svc := Service{Auth: auth, Orders: store}
	got, err := svc.Get(context.Background(), servicePrincipal(t), ScopedRequest{WorkOrderID: "wo-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "wo-1" || auth.calls != 1 || auth.project != "project-a" || auth.cap != workorderaccess.Read {
		t.Fatalf("authorization not bound to stored project: order=%+v auth=%+v", got, auth)
	}
}

func TestGetProjectsFinancialAndPayFieldsByCurrentCapabilities(t *testing.T) {
	amount := values.MustDecimal("125.50", 2, values.RoundingExactRequired)
	zero := values.MustDecimal("0", 0, values.RoundingExactRequired)
	snapshot := testSnapshot(t)
	snapshot.Title = "Riverside trench repair"
	snapshot.LinkedTaskIDs = []string{"task-123"}
	snapshot.Requests = []workorder.InitiatorRequest{{ID: "budget-1", DefinitionID: "BUDGET", Kind: workorder.RequestBudget, Status: workorder.RequestPending, RequesterID: "alice", Subject: "Supplier quote $125.50", Rationale: "Need purchase at $125.50", Amount: amount, Currency: "USD", CostCategory: "materials", FundingSource: "vendor quote 125.50", EvidenceRef: "invoice-$125", EvidenceRefs: []string{"quote-$125"}, FormValues: map[string]string{"price": "125.50"}, Quantity: zero, EstimatedCost: zero, QuantityDelta: zero, PriceDelta: zero}}
	snapshot.Spending = []workorder.Spend{{ID: "spend-1", Category: "materials", Currency: "USD", Amount: amount, Quantity: zero, Disposition: workorder.SpendIncurred}}
	replacement := amount
	snapshot.Corrections = []workorder.Correction{{ID: "correction-1", TargetRecordID: "spend-1", ReplacementAmount: &replacement}}
	snapshot.WorkEntries = []workorder.WorkEntry{{ID: "work-1", Kind: "LABOR", WorkerID: "worker-7", DurationMinutes: 90, Quantity: zero}}
	snapshot.Journal[0].Detail = "sensitive cost payload"
	snapshot.Journal[0].CommandDigest = "sensitive-digest"
	snapshot.Journal[0].IdempotencyKey = "sensitive-key"

	deniedAuth := &testAuthorizer{deniedCaps: map[workorderaccess.Capability]bool{workorderaccess.ViewCost: true, workorderaccess.ViewPay: true}}
	deniedSvc := Service{Auth: deniedAuth, Orders: &testOrders{snapshot: snapshot}}
	denied, err := deniedSvc.Get(context.Background(), servicePrincipal(t), ScopedRequest{WorkOrderID: snapshot.ID})
	if err != nil {
		t.Fatal(err)
	}
	if denied.Requests[0].Amount.Validate() == nil || denied.Requests[0].Currency != "" || denied.Requests[0].Subject != "" || denied.Requests[0].Rationale != "" || denied.Requests[0].FundingSource != "" || len(denied.Requests[0].EvidenceRefs) != 0 || len(denied.Requests[0].FormValues) != 0 || len(denied.Spending) != 0 || denied.Corrections[0].ReplacementAmount != nil || denied.Corrections[0].Reason != "" {
		t.Fatalf("financial values escaped without ViewCost: request=%+v spending=%+v correction=%+v", denied.Requests[0], denied.Spending, denied.Corrections[0])
	}
	if denied.WorkEntries[0].WorkerID != "" || denied.WorkEntries[0].DurationMinutes != 0 {
		t.Fatalf("labor pay details escaped without ViewPay: %+v", denied.WorkEntries[0])
	}
	if len(denied.LinkedTaskIDs) != 1 || denied.LinkedTaskIDs[0] != "task-123" || denied.ID != snapshot.ID {
		t.Fatalf("projection broke safe project links/order identity: %+v", denied)
	}
	for _, event := range denied.Journal {
		if event.Detail != "" || event.CommandDigest != "" || event.IdempotencyKey != "" {
			t.Fatalf("journal leaked command data: %+v", event)
		}
	}

	managerSvc := Service{Auth: &testAuthorizer{}, Orders: &testOrders{snapshot: snapshot}}
	manager, err := managerSvc.Get(context.Background(), servicePrincipal(t), ScopedRequest{WorkOrderID: snapshot.ID})
	if err != nil {
		t.Fatal(err)
	}
	if manager.Requests[0].Amount.String() != amount.String() || manager.Spending[0].Amount.String() != amount.String() || manager.Corrections[0].ReplacementAmount == nil || manager.WorkEntries[0].WorkerID != "worker-7" || manager.WorkEntries[0].DurationMinutes != 90 {
		t.Fatalf("authorized manager lost financial/pay fields: %+v", manager)
	}
}

func TestDeniedMutationHasNoDurableEffect(t *testing.T) {
	store := &testOrders{snapshot: testSnapshot(t)}
	auth := &testAuthorizer{denied: true}
	svc := Service{Auth: auth, Orders: store, Clock: func() time.Time { return time.Date(2026, 9, 25, 12, 1, 0, 0, time.UTC) }}
	_, err := svc.AddNote(context.Background(), servicePrincipal(t), NoteRequest{ScopedRequest: ScopedRequest{WorkOrderID: "wo-1", IdempotencyKey: "note-1"}, ExpectedRevision: 1, Input: workorder.NoteInput{ID: "n1", Body: "field note", Visibility: "PARTICIPANTS", Classification: "INTERNAL"}})
	if err == nil || store.executes != 0 || store.snapshot.Revision != 1 {
		t.Fatalf("denied mutation changed durable state: err=%v executes=%d revision=%d", err, store.executes, store.snapshot.Revision)
	}
}

func TestTransitionFailsClosedWithoutBoundWorkflowGate(t *testing.T) {
	store := &testOrders{snapshot: testSnapshot(t)}
	auth := &testAuthorizer{}
	svc := Service{Auth: auth, Orders: store}
	_, err := svc.Transition(context.Background(), servicePrincipal(t), TransitionRequest{ScopedRequest: ScopedRequest{WorkOrderID: "wo-1", IdempotencyKey: "advance-1"}, ExpectedRevision: 1, Input: workorder.TransitionInput{Target: workorder.PhaseAuthorization, Reason: "ready"}})
	if !errors.Is(err, ErrUnavailable) || store.executes != 0 || store.snapshot.Phase != workorder.PhaseDraft {
		t.Fatalf("unbound phase transition was not refused: err=%v executions=%d phase=%s", err, store.executes, store.snapshot.Phase)
	}
}

func TestMutationUsesTrustedActorRevisionAndReplayReceipt(t *testing.T) {
	store := &testOrders{snapshot: testSnapshot(t)}
	auth := &testAuthorizer{}
	now := time.Date(2026, 9, 25, 12, 1, 0, 0, time.UTC)
	svc := Service{Auth: auth, Orders: store, Clock: func() time.Time { return now }}
	req := NoteRequest{ScopedRequest: ScopedRequest{ProjectID: "project-a", WorkOrderID: "wo-1", IdempotencyKey: "note-1"}, ExpectedRevision: 1, Input: workorder.NoteInput{ID: "n1", AuthorID: "forged", Body: "field note", Visibility: "PARTICIPANTS", Classification: "INTERNAL", IdempotencyKey: "forged"}}
	first, err := svc.AddNote(context.Background(), servicePrincipal(t), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != 2 || len(first.Notes) != 1 || first.Notes[0].AuthorID != "alice" || auth.calls != 2 {
		t.Fatalf("trusted mutation fields not enforced: %+v auth=%+v", first, auth)
	}
	replay, err := svc.AddNote(context.Background(), servicePrincipal(t), req)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Revision != 2 || store.executes != 2 || len(replay.Notes) != 1 {
		t.Fatalf("idempotent retry changed result: %+v execute calls=%d", replay, store.executes)
	}
	_, err = svc.AddNote(context.Background(), servicePrincipal(t), NoteRequest{ScopedRequest: ScopedRequest{ProjectID: "project-a", WorkOrderID: "wo-1", IdempotencyKey: "stale"}, ExpectedRevision: 1, Input: workorder.NoteInput{ID: "n2", Body: "stale", Visibility: "PARTICIPANTS", Classification: "INTERNAL"}})
	if !errors.Is(err, workorder.ErrStale) || len(store.snapshot.Notes) != 1 {
		t.Fatalf("stale revision was not rejected: err=%v snapshot=%+v", err, store.snapshot)
	}
}

func TestMismatchedProjectScopeIsNeutralNotFound(t *testing.T) {
	store := &testOrders{snapshot: testSnapshot(t)}
	auth := &testAuthorizer{}
	svc := Service{Auth: auth, Orders: store}
	_, err := svc.Get(context.Background(), servicePrincipal(t), ScopedRequest{ProjectID: "project-other", WorkOrderID: "wo-1"})
	if !errors.Is(err, workorder.ErrNotFound) || auth.calls != 0 {
		t.Fatalf("mismatched scope was not concealed: err=%v auth calls=%d", err, auth.calls)
	}
}

func TestCreateUsesDRAFTAndReturnsPersistedReplay(t *testing.T) {
	store := &testOrders{}
	auth := &testAuthorizer{}
	published := createTemplate(t)
	tick := 0
	svc := Service{Auth: auth, Templates: testTemplates{published}, Orders: store, Clock: func() time.Time { tick++; return time.Date(2026, 9, 25, 12, tick, 0, 0, time.UTC) }}
	req := CreateRequest{ProjectID: "project-a", Title: "Stair stringer repair", Scope: "Replace west stair stringer", SupervisorID: "supervisor-1", LinkedTaskIDs: []string{"task-rfi", "task-co-03"}, TemplateID: "RIVERSIDE", TemplateVersion: "1.0.0", IdempotencyKey: "create-wo-1"}
	first, err := svc.Create(context.Background(), servicePrincipal(t), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Phase != workorder.PhaseDraft || first.TemplateDigest != published.Digest() || first.Title != req.Title || first.SupervisorID != req.SupervisorID || len(first.LinkedTaskIDs) != 2 {
		t.Fatalf("creation failed to pin work order configuration: %+v", first)
	}
	replayed, err := svc.Create(context.Background(), servicePrincipal(t), req)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != first.ID || !replayed.CreatedAt.Equal(first.CreatedAt) || store.creates != 2 {
		t.Fatalf("create replay did not return stored record: first=%+v replay=%+v", first, replayed)
	}
}

func TestSubmitRequestEnforcesPinnedFormRequiredFields(t *testing.T) {
	pub := createTemplate(t)
	store := &testOrders{snapshot: testSnapshotForTemplate(t, pub)}
	auth := &testAuthorizer{}
	svc := Service{Auth: auth, Templates: testTemplates{pub}, Orders: store, Clock: func() time.Time { return time.Date(2026, 9, 25, 12, 1, 0, 0, time.UTC) }}
	_, err := svc.SubmitRequest(context.Background(), servicePrincipal(t), InitiatorRequest{ScopedRequest: ScopedRequest{WorkOrderID: "wo-1", IdempotencyKey: "budget-1"}, ExpectedRevision: 1, Input: workorder.InitiatorRequestInput{Kind: workorder.RequestBudget, Subject: "Steel order", Rationale: "Required for stringer repair", Amount: values.MustDecimal("100.00", 2, values.RoundingHalfEven), Currency: "USD", CostCategory: "materials", FundingSource: "project", BaselineRevision: "base-1"}})
	if !errors.Is(err, ErrInvalidRequest) || store.snapshot.Revision != 1 || len(store.snapshot.Requests) != 0 {
		t.Fatalf("template required fields were not enforced: err=%v snapshot=%+v", err, store.snapshot)
	}
}

func TestValidConfiguredRequestAndGatedTransitionAdvanceRevision(t *testing.T) {
	ctx := context.Background()
	pub := createTemplate(t)
	now := time.Date(2026, 9, 25, 14, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	t.Run("request pins definition and form", func(t *testing.T) {
		store := &testOrders{snapshot: testSnapshotForTemplate(t, pub)}
		svc := Service{Auth: &testAuthorizer{}, Templates: testTemplates{pub}, Orders: store, Clock: clock}
		got, err := svc.SubmitRequest(ctx, servicePrincipal(t), InitiatorRequest{ScopedRequest: ScopedRequest{WorkOrderID: "wo-1", IdempotencyKey: "budget-valid"}, ExpectedRevision: 1, Input: workorder.InitiatorRequestInput{Kind: workorder.RequestBudget, Subject: "Steel", Rationale: "Riverside repair", Amount: values.MustDecimal("100.00", 2, values.RoundingHalfEven), Currency: "USD", CostCategory: "materials", FundingSource: "project", BaselineRevision: "base-1", FormValues: map[string]string{"amount": "100.00", "reason": "repair"}}})
		if err != nil {
			t.Fatal(err)
		}
		if got.Revision != 2 || len(got.Requests) != 1 || got.Requests[0].DefinitionID != "INITIAL_BUDGET" || got.Requests[0].FormValues["reason"] != "repair" {
			t.Fatalf("request was not pinned to published form: %+v", got)
		}
	})
	t.Run("workflow gate permits current target", func(t *testing.T) {
		store := &testOrders{snapshot: testSnapshotForTemplate(t, pub)}
		gate := &testPhaseGate{}
		svc := Service{Auth: &testAuthorizer{}, Templates: testTemplates{pub}, Orders: store, Phases: gate, Clock: clock}
		got, err := svc.Transition(ctx, servicePrincipal(t), TransitionRequest{ScopedRequest: ScopedRequest{WorkOrderID: "wo-1", IdempotencyKey: "advance"}, ExpectedRevision: 1, Input: workorder.TransitionInput{Target: workorder.PhaseAuthorization, Reason: "Scope approved"}})
		if err != nil {
			t.Fatal(err)
		}
		if got.Revision != 2 || got.Phase != workorder.PhaseAuthorization || gate.calls != 1 {
			t.Fatalf("transition not gated or persisted: gate=%d order=%+v", gate.calls, got)
		}
	})
}

func TestCommandServicesAssignProgressSpendAndDecide(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	ctx := context.Background()
	principal := servicePrincipal(t)
	t.Run("assignment resolves canonical worker", func(t *testing.T) {
		store := &testOrders{snapshot: testSnapshot(t)}
		svc := Service{Auth: &testAuthorizer{}, Orders: store, Workers: testWorkers{worker: "worker-1", found: true, eligible: true}, Clock: clock}
		got, err := svc.Assign(ctx, principal, AssignmentRequest{ScopedRequest: ScopedRequest{WorkOrderID: "wo-1", IdempotencyKey: "assign"}, ExpectedRevision: 1, Input: workorder.AssignmentInput{WorkerID: "legacy-ref", Role: "carpenter"}})
		if err != nil {
			t.Fatal(err)
		}
		if got.Revision != 2 || len(got.Assignments) != 1 || got.Assignments[0].WorkerID != "worker-1" {
			t.Fatalf("assignment did not use canonical eligible worker: %+v", got)
		}
	})
	t.Run("progress and spend", func(t *testing.T) {
		progressStore := &testOrders{snapshot: testSnapshot(t)}
		progressSvc := Service{Auth: &testAuthorizer{}, Orders: progressStore, Clock: clock}
		progress, err := progressSvc.RecordProgress(ctx, principal, ProgressRequest{ScopedRequest: ScopedRequest{WorkOrderID: "wo-1", IdempotencyKey: "progress"}, ExpectedRevision: 1, Input: workorder.ProgressInput{LineID: "stringer", Unit: "ft", Quantity: values.MustDecimal("4", 0, values.RoundingHalfEven)}})
		if err != nil {
			t.Fatal(err)
		}
		if len(progress.Progress) != 1 || progress.Revision != 2 {
			t.Fatalf("progress was not recorded: %+v", progress)
		}
		spendStore := &testOrders{snapshot: testSnapshot(t)}
		spendSvc := Service{Auth: &testAuthorizer{}, Orders: spendStore, Clock: clock}
		spend, err := spendSvc.RecordSpend(ctx, principal, SpendRequest{ScopedRequest: ScopedRequest{WorkOrderID: "wo-1", IdempotencyKey: "spend"}, ExpectedRevision: 1, Input: workorder.SpendInput{Category: "materials", Currency: "USD", SourceRef: "receipt-1", Amount: values.MustDecimal("125.00", 2, values.RoundingHalfEven), Disposition: workorder.SpendIncurred}})
		if err != nil {
			t.Fatal(err)
		}
		if len(spend.Spending) != 1 || spend.Revision != 2 {
			t.Fatalf("spend was not recorded: %+v", spend)
		}
	})
	t.Run("independent approval", func(t *testing.T) {
		pub := createTemplate(t)
		agg, err := workorder.Restore(testSnapshotForTemplate(t, pub))
		if err != nil {
			t.Fatal(err)
		}
		err = agg.SubmitRequest(workorder.InitiatorRequestInput{ID: "budget-1", DefinitionID: "INITIAL_BUDGET", Kind: workorder.RequestBudget, Subject: "Steel", Rationale: "Riverside repair", ActorID: "bob", Amount: values.MustDecimal("50.00", 2, values.RoundingHalfEven), Currency: "USD", CostCategory: "materials", FundingSource: "project", BaselineRevision: "base", FormValues: map[string]string{"amount": "50.00", "reason": "repair"}, ExpectedRevision: 1, IdempotencyKey: "request", Now: now})
		if err != nil {
			t.Fatal(err)
		}
		store := &testOrders{snapshot: agg.Snapshot()}
		svc := Service{Auth: &testAuthorizer{}, Orders: store, Clock: clock}
		got, err := svc.DecideRequest(ctx, principal, DecisionRequest{ScopedRequest: ScopedRequest{WorkOrderID: "wo-1", IdempotencyKey: "decision"}, ExpectedRevision: 2, Input: workorder.RequestDecisionInput{RequestID: "budget-1", Decision: workorder.RequestApproved, Reason: "Approved"}})
		if err != nil {
			t.Fatal(err)
		}
		if got.Revision != 3 || got.Requests[0].DecisionActor != "alice" || got.Requests[0].Status != workorder.RequestApproved {
			t.Fatalf("decision was not persisted by independent approver: %+v", got)
		}
	})
}

func TestCorrectRecordRequiresExistingCorrectableFact(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	agg, err := workorder.Restore(testSnapshot(t))
	if err != nil {
		t.Fatal(err)
	}
	if err = agg.RecordProgress(workorder.ProgressInput{ID: "progress-1", LineID: "stringer", Unit: "ft", ActorID: "bob", Quantity: values.MustDecimal("2", 0, values.RoundingHalfEven), ExpectedRevision: 1, IdempotencyKey: "progress-1", Now: now}); err != nil {
		t.Fatal(err)
	}
	store := &testOrders{snapshot: agg.Snapshot()}
	svc := Service{Auth: &testAuthorizer{}, Orders: store, Clock: func() time.Time { return now }}
	got, err := svc.CorrectRecord(context.Background(), servicePrincipal(t), CorrectionRequest{ScopedRequest: ScopedRequest{WorkOrderID: "wo-1", IdempotencyKey: "correction"}, ExpectedRevision: 2, Input: workorder.CorrectionInput{TargetRecordID: "progress-1", Reason: "Measured quantity adjusted"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 3 || len(got.Corrections) != 1 || got.Corrections[0].TargetRecordID != "progress-1" {
		t.Fatalf("correction missing from journaled aggregate: %+v", got)
	}
}
