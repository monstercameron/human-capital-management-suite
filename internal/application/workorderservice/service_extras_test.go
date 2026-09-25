package workorderservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorder"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderbilling"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderreport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workordertemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type extrasOrders struct {
	*testOrders
	orders    []workorder.Snapshot
	cursor    string
	gotCursor string
}

func (r *extrasOrders) List(_ context.Context, _, _, _, cursor string, _ int) ([]workorder.Snapshot, string, error) {
	r.gotCursor = cursor
	return r.orders, r.cursor, nil
}

func TestListCursorIsOpaqueAndBoundToTenantProjectAndFilter(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	svc := Service{CursorKey: key}
	token, err := svc.signCursor(listCursor{Tenant: "tenant-a", Project: "project-a", Status: "ACTIVE", Store: "wo-9"})
	if err != nil {
		t.Fatal(err)
	}
	if token == "wo-9" {
		t.Fatal("cursor exposed the store continuation value")
	}
	if got, err := svc.verifyCursor(token, "tenant-a", "project-a", "ACTIVE"); err != nil || got != "wo-9" {
		t.Fatalf("valid cursor = (%q, %v)", got, err)
	}
	for _, scope := range []struct{ tenant, project, status string }{
		{"tenant-b", "project-a", "ACTIVE"},
		{"tenant-a", "project-b", "ACTIVE"},
		{"tenant-a", "project-a", "CLOSED"},
	} {
		if _, err := svc.verifyCursor(token, scope.tenant, scope.project, scope.status); !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("cursor accepted wrong scope/filter %+v: %v", scope, err)
		}
	}
	last := token[:len(token)-1]
	if _, err := svc.verifyCursor(last, "tenant-a", "project-a", "ACTIVE"); !errors.Is(err, ErrInvalidCursor) {
		t.Errorf("tampered cursor accepted: %v", err)
	}
}

func TestCostReportsRequireExplicitPayRateGrantWhenInputContainsRates(t *testing.T) {
	if containsPayRateFields(nil) {
		t.Fatal("empty report was marked as containing pay data")
	}
	if containsPayRateFields([]workorderreport.Source{{Entries: []workorderreport.Entry{{Fields: map[string]string{"amount": "200.00", "hourly rate": "40.00"}}}}}) != true {
		t.Fatal("pay-rate field was not detected")
	}
	if containsPayRateFields([]workorderreport.Source{{Entries: []workorderreport.Entry{{Fields: map[string]string{"amount": "200.00", "approved_quantity": "4"}}}}}) {
		t.Fatal("ordinary cost fields were incorrectly classified as pay rates")
	}
}

func TestListWrapsAndUnwrapsStoreCursorAfterAuthorization(t *testing.T) {
	snapshot := testSnapshot(t)
	store := &extrasOrders{testOrders: &testOrders{snapshot: snapshot}, orders: []workorder.Snapshot{snapshot}, cursor: "internal-next"}
	auth := &testAuthorizer{}
	svc := Service{Auth: auth, Orders: store, CursorKey: []byte("0123456789abcdef0123456789abcdef")}
	first, err := svc.List(context.Background(), servicePrincipal(t), ListRequest{ProjectID: "project-a", Status: "ACTIVE", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Orders) != 1 || first.NextCursor == "" || first.NextCursor == store.cursor {
		t.Fatalf("list did not return scoped page and opaque cursor: %+v", first)
	}
	second, err := svc.List(context.Background(), servicePrincipal(t), ListRequest{ProjectID: "project-a", Status: "ACTIVE", Cursor: first.NextCursor, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if store.gotCursor != "internal-next" || len(second.Orders) != 1 {
		t.Fatalf("opaque cursor was not decoded for the repository: raw=%q result=%+v", store.gotCursor, second)
	}
	if auth.project != "project-a" || auth.cap != "read" {
		t.Fatalf("returned orders were not read-authorized: %+v", auth)
	}
}

type extrasAuthorizer struct {
	testAuthorizer
	workerDenied bool
}

func (a *extrasAuthorizer) AuthorizeWorkEntry(context.Context, *trust.Principal, string, string, string) error {
	if a.workerDenied {
		return errors.New("worker posting denied")
	}
	return nil
}

type extrasWorkers struct{}

func (extrasWorkers) ResolveWorker(_ context.Context, _, ref string) (string, bool, error) {
	return "worker-canonical", ref != "missing", nil
}
func (extrasWorkers) ResolveEligible(_ context.Context, _, id string) (bool, error) {
	return id == "worker-canonical", nil
}

func TestRecordWorkEntryUsesCanonicalWorkerAndFailsClosedOnActorMismatch(t *testing.T) {
	snapshot := testSnapshot(t)
	snapshot.Assignments = []workorder.Assignment{{ID: "a1", WorkerID: "worker-canonical", Role: "laborer", Start: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}}
	store := &testOrders{snapshot: snapshot}
	auth := &extrasAuthorizer{}
	svc := Service{Auth: auth, Orders: store, Workers: extrasWorkers{}, Clock: func() time.Time { return time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC) }}
	req := WorkEntryRequest{ScopedRequest: ScopedRequest{WorkOrderID: "wo-1", IdempotencyKey: "labor-1"}, ExpectedRevision: 1, Input: workorder.WorkEntryInput{WorkerID: "worker-ref", Kind: "LABOR", WorkDate: "2026-09-25", TimeZone: "America/New_York", LineID: "line-1", DurationMinutes: 60}}
	got, err := svc.RecordWorkEntry(context.Background(), servicePrincipal(t), req)
	if err != nil {
		t.Fatalf("record work entry: %v; snapshot=%+v", err, store.snapshot)
	}
	if got.Entry.ID == "" || got.Entry.WorkerID != "worker-canonical" || got.Entry.ActorID != "alice" || got.Order.Revision != 2 {
		t.Fatalf("work entry did not use trusted identities: %+v", got)
	}
	privacyStore := &testOrders{snapshot: snapshot}
	privacySvc := Service{Auth: &extrasAuthorizer{testAuthorizer: testAuthorizer{deniedCaps: map[workorderaccess.Capability]bool{workorderaccess.ViewPay: true}}}, Orders: privacyStore, Workers: extrasWorkers{}, Clock: svc.Clock}
	privacyReq := req
	privacyReq.IdempotencyKey = "labor-private"
	private, err := privacySvc.RecordWorkEntry(context.Background(), servicePrincipal(t), privacyReq)
	if err != nil {
		t.Fatal(err)
	}
	if private.Entry.ID == "" || private.Entry.ID != private.Order.WorkEntries[len(private.Order.WorkEntries)-1].ID || private.Entry.WorkerID != "" || private.Entry.DurationMinutes != 0 {
		t.Fatalf("redaction made the newly recorded work entry unfindable or exposed pay details: %+v", private)
	}

	deniedStore := &testOrders{snapshot: snapshot}
	denied := Service{Auth: &extrasAuthorizer{workerDenied: true}, Orders: deniedStore, Workers: extrasWorkers{}, Clock: svc.Clock}
	_, err = denied.RecordWorkEntry(context.Background(), servicePrincipal(t), req)
	if err == nil || deniedStore.executes != 0 {
		t.Fatalf("unauthorized actor recorded for worker: err=%v executions=%d", err, deniedStore.executes)
	}
}

type extrasArtifacts struct{ rows map[string]ArtifactRecord }

func (a *extrasArtifacts) GetArtifact(_ context.Context, tenantID, orderID, actorID, key, kind string) (ArtifactRecord, error) {
	if row, ok := a.rows[kind+"/"+key]; ok {
		return row, nil
	}
	return ArtifactRecord{}, ErrArtifactNotFound
}
func (a *extrasArtifacts) RecordArtifact(_ context.Context, tenantID, orderID, projectID, actorID, key string, rev uint64, kind, digest string, payload []byte) (ArtifactRecord, error) {
	if a.rows == nil {
		a.rows = map[string]ArtifactRecord{}
	}
	idx := kind + "/" + key
	if prior, ok := a.rows[idx]; ok {
		if prior.CommandDigest != digest {
			return ArtifactRecord{}, workorder.ErrConflict
		}
		return prior, nil
	}
	row := ArtifactRecord{ID: "artifact-1", TenantID: tenantID, WorkOrderID: orderID, ProjectID: projectID, Kind: kind, ActorID: actorID, IdempotencyKey: key, CommandDigest: digest, SourceRevision: rev, Payload: append([]byte(nil), payload...), CreatedAt: time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)}
	a.rows[idx] = row
	return row, nil
}

type extrasReports struct{ fail bool }

func (r extrasReports) ResolveDefinition(context.Context, string, string, string, string, uint32) (workorderreport.Definition, error) {
	if r.fail {
		return workorderreport.Definition{}, errors.New("should not resolve during replay")
	}
	return workorderreport.Definition{ID: "field-progress", Version: 1, Digest: "sha256:def", Kind: workorderreport.Progress}, nil
}
func (r extrasReports) BuildRequest(_ context.Context, s workorder.Snapshot, d workorderreport.Definition) (workorderreport.Request, error) {
	return workorderreport.Request{Sources: []workorderreport.Source{{Name: "progress", Watermark: workorderreport.Watermark{Source: "progress", Value: "revision-" + d.ID}, Complete: true, Entries: []workorderreport.Entry{{TenantID: s.TenantID, ProjectID: s.ProjectID, WorkOrderID: s.ID, Source: "progress", ID: "p1", Fields: map[string]string{"quantity": "2"}}}}}}, nil
}

type extrasTemplates struct{ published workordertemplate.Published }

func (r extrasTemplates) ResolvePublished(_ context.Context, _, id, version string) (workordertemplate.Published, error) {
	if id != r.published.Snapshot().ID || version != r.published.Version() {
		return workordertemplate.Published{}, errors.New("pinned template not found")
	}
	return r.published, nil
}

func extrasPublished(t *testing.T) workordertemplate.Published {
	t.Helper()
	role := "WORK_ORDER_ACTOR"
	phases := []workordertemplate.Phase{
		{ID: "DRAFT", AllowedExits: []string{"AUTHORIZATION"}, ActorRoles: []string{role}},
		{ID: "AUTHORIZATION", AllowedExits: []string{"READY"}, ActorRoles: []string{role}, Gates: []workordertemplate.Gate{{ID: "SCOPE", Kind: "AUTHORIZATION", Required: true}}},
		{ID: "READY", AllowedExits: []string{"EXECUTION"}, ActorRoles: []string{role}, Gates: []workordertemplate.Gate{{ID: "SAFETY", Kind: "SAFETY", Required: true}}},
		{ID: "EXECUTION", AllowedExits: []string{"INSPECTION"}, ActorRoles: []string{role}},
		{ID: "INSPECTION", AllowedExits: []string{"ACCEPTED"}, ActorRoles: []string{role}, Gates: []workordertemplate.Gate{{ID: "RECONCILE", Kind: "RECONCILIATION", Required: true}}},
		{ID: "ACCEPTED", AllowedExits: []string{"CLOSED"}, ActorRoles: []string{role}},
		{ID: "CLOSED", ActorRoles: []string{role}, Gates: []workordertemplate.Gate{{ID: "CLOSE", Kind: "CLOSURE", Required: true}}},
	}
	draft := workordertemplate.Draft{ID: "RIVERSIDE", Name: "Test work order", Phases: phases, Roles: []workordertemplate.RoleGrant{{Role: role, Actions: []string{"create", "request"}}}, ReportPolicies: []workordertemplate.ReportPolicy{{ID: "field-progress", Version: "1", Kind: workordertemplate.ReportProgress, Required: true}, {ID: "cost", Version: "1", Kind: workordertemplate.ReportCost}}, Billing: &workordertemplate.BillingPolicy{ID: "contract-a", Version: "v1", Mode: "UNIT_PRICE"}}
	published, err := workordertemplate.Publish(draft, workordertemplate.PublishMeta{Version: "1.0.0", PublishedBy: "test-owner", ReviewRef: "test-review"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return published
}

func testSnapshotWithPublishedTemplate(t *testing.T) (workorder.Snapshot, extrasTemplates) {
	t.Helper()
	published := extrasPublished(t)
	snapshot := testSnapshot(t)
	snapshot.TemplateID = published.Snapshot().ID
	snapshot.TemplateVersion = published.Version()
	snapshot.TemplateDigest = published.Digest()
	return snapshot, extrasTemplates{published: published}
}

type reportCostDenied struct{ caps []workorderaccess.Capability }

func (a *reportCostDenied) Authorize(_ context.Context, _ *trust.Principal, _, _ string, capability workorderaccess.Capability) error {
	a.caps = append(a.caps, capability)
	if capability == workorderaccess.ViewCost {
		return errors.New("cost permission denied")
	}
	return nil
}
func (*reportCostDenied) AuthorizeCreate(context.Context, *trust.Principal, string) error { return nil }
func (*reportCostDenied) AuthorizeList(context.Context, *trust.Principal, string) error   { return nil }
func (*reportCostDenied) AuthorizeWorkEntry(context.Context, *trust.Principal, string, string, string) error {
	return nil
}

func TestRequestReportPersistsAndReplaysExactArtifactBeforeSourceResolution(t *testing.T) {
	artifacts := &extrasArtifacts{}
	snapshot, templates := testSnapshotWithPublishedTemplate(t)
	svc := Service{Auth: &testAuthorizer{}, Orders: &testOrders{snapshot: snapshot}, Templates: templates, Artifacts: artifacts, Reports: extrasReports{}, Clock: func() time.Time { return time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC) }}
	req := ReportRequest{ScopedRequest: ScopedRequest{WorkOrderID: "wo-1", IdempotencyKey: "report-1"}, ExpectedRevision: 1, Kind: workorderreport.Progress, DefinitionID: "field-progress", DefinitionVersion: 1}
	first, err := svc.RequestReport(context.Background(), servicePrincipal(t), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Artifact.ID != "artifact-1" || first.Artifact.SourceRevision != 1 || first.Report.Status != workorderreport.StatusComplete {
		t.Fatalf("report artifact incomplete: %+v", first)
	}
	svc.Reports = extrasReports{fail: true}
	replay, err := svc.RequestReport(context.Background(), servicePrincipal(t), req)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Artifact.ID != first.Artifact.ID || replay.Report.Status != first.Report.Status {
		t.Fatalf("replay changed the report: first=%+v replay=%+v", first, replay)
	}
	req.DefinitionVersion++
	if _, err := svc.RequestReport(context.Background(), servicePrincipal(t), req); !errors.Is(err, workorder.ErrConflict) {
		t.Fatalf("idempotency key reused for a different report: %v", err)
	}
}

func TestCostReportRequiresViewCostInAdditionToInspect(t *testing.T) {
	auth := &reportCostDenied{}
	snapshot, templates := testSnapshotWithPublishedTemplate(t)
	svc := Service{Auth: auth, Orders: &testOrders{snapshot: snapshot}, Templates: templates, Artifacts: &extrasArtifacts{}, Reports: extrasReports{}}
	_, err := svc.RequestReport(context.Background(), servicePrincipal(t), ReportRequest{ScopedRequest: ScopedRequest{WorkOrderID: "wo-1", IdempotencyKey: "cost-report"}, ExpectedRevision: 1, Kind: workorderreport.Cost, DefinitionID: "cost", DefinitionVersion: 1})
	if err == nil {
		t.Fatal("cost report passed without ViewCost")
	}
	if len(auth.caps) != 2 || auth.caps[0] != workorderaccess.Inspect || auth.caps[1] != workorderaccess.ViewCost {
		t.Fatalf("report authorization omitted expected gates: %v", auth.caps)
	}
}

type extrasPricing struct{ fail bool }

func (p extrasPricing) ResolvePricing(context.Context, string, string, string, string) (workorderbilling.PricingPolicy, error) {
	if p.fail {
		return workorderbilling.PricingPolicy{}, errors.New("pricing should not resolve during replay")
	}
	rate, _ := values.NewDecimal("12.50", 2, values.RoundingExactRequired)
	return workorderbilling.PricingPolicy{ID: "contract-a", Version: "v1", Currency: "USD", Mode: workorderbilling.PricingUnitPrice, AmountScale: 2, Rounding: values.RoundingExactRequired, UnitRates: map[string]values.Decimal{"EACH": rate}}, nil
}

type extrasBillingSources struct{}

func (extrasBillingSources) Sources(_ context.Context, s workorder.Snapshot, _, _ time.Time) ([]workorderbilling.Source, map[string]string, error) {
	quantity, _ := values.NewDecimal("2", 0, values.RoundingExactRequired)
	return []workorderbilling.Source{{ID: "progress-1", Revision: "2", WorkOrderID: s.ID, Kind: workorderbilling.SourceAcceptedQuantity, Accepted: true, Quantity: quantity, Unit: "EACH"}}, map[string]string{"progress-1": "Completed units"}, nil
}

func TestRequestBillingDraftUsesPinnedPolicyAndStoresSourceRevision(t *testing.T) {
	artifacts := &extrasArtifacts{}
	snapshot, templates := testSnapshotWithPublishedTemplate(t)
	svc := Service{Auth: &testAuthorizer{}, Orders: &testOrders{snapshot: snapshot}, Templates: templates, Pricing: extrasPricing{}, BillingSources: extrasBillingSources{}, Artifacts: artifacts}
	req := BillingDraftRequest{ScopedRequest: ScopedRequest{WorkOrderID: "wo-1", IdempotencyKey: "billing-1"}, ExpectedRevision: 1, ContractVersion: "v1", PeriodStart: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), PeriodEnd: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	got, err := svc.RequestBillingDraft(context.Background(), servicePrincipal(t), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Draft.ID == "" || got.Draft.Total.Amount().String() != "25.00" || got.SourceRevision != 1 || got.Artifact.Kind != "BILLING" {
		t.Fatalf("billing draft not pinned or persisted: %+v", got)
	}
	svc.Pricing = extrasPricing{fail: true}
	replay, err := svc.RequestBillingDraft(context.Background(), servicePrincipal(t), req)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Artifact.ID != got.Artifact.ID || replay.Draft.Total.String() != got.Draft.Total.String() {
		t.Fatalf("billing replay changed artifact: first=%+v replay=%+v", got, replay)
	}

	deniedSnapshot, deniedTemplates := testSnapshotWithPublishedTemplate(t)
	denied := Service{Auth: &testAuthorizer{denied: true}, Orders: &testOrders{snapshot: deniedSnapshot}, Templates: deniedTemplates, Pricing: extrasPricing{}, BillingSources: extrasBillingSources{}, Artifacts: &extrasArtifacts{}}
	if _, err := denied.RequestBillingDraft(context.Background(), servicePrincipal(t), req); err == nil {
		t.Fatal("billing request bypassed current Bill authorization")
	}
}
