package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/workorderservice"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorder"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderreport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workordertemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type reportAdapterTemplates struct{ published workordertemplate.Published }

func (r reportAdapterTemplates) ResolvePublished(_ context.Context, _, id, version string) (workordertemplate.Published, error) {
	if r.published.Pin().TemplateID != id || r.published.Version() != version {
		return workordertemplate.Published{}, workordertemplate.ErrInvalid
	}
	return r.published, nil
}

type reportAdapterOrders struct{ snapshot workorder.Snapshot }

func (r reportAdapterOrders) Create(context.Context, string, workorder.Snapshot, string, string) error {
	return errors.New("unexpected create")
}
func (r reportAdapterOrders) Get(_ context.Context, tenant, id string) (workorder.Snapshot, error) {
	if tenant != r.snapshot.TenantID || id != r.snapshot.ID {
		return workorder.Snapshot{}, workorder.ErrNotFound
	}
	return r.snapshot, nil
}
func (r reportAdapterOrders) Execute(context.Context, string, string, string, string, uint64, string, func(workorder.Snapshot) (workorder.Snapshot, error)) (workorder.Snapshot, error) {
	return workorder.Snapshot{}, errors.New("unexpected execute")
}

func reportAdapterFixture(t *testing.T) (workorder.Snapshot, workordertemplate.Published) {
	t.Helper()
	draft := workordertemplate.Draft{
		ID: "RIVERSIDE", Name: "Riverside field work",
		Phases: []workordertemplate.Phase{
			{ID: "DRAFT", AllowedExits: []string{"AUTHORIZATION"}, ActorRoles: []string{"INITIATOR"}},
			{ID: "AUTHORIZATION", AllowedExits: []string{"READY"}, ActorRoles: []string{"SUPERVISOR"}, Gates: []workordertemplate.Gate{{ID: "APPROVAL", Kind: "AUTHORIZATION", Required: true}, {ID: "SAFETY", Kind: "SAFETY", Required: true}}},
			{ID: "READY", AllowedExits: []string{"EXECUTION"}, ActorRoles: []string{"SUPERVISOR"}},
			{ID: "EXECUTION", AllowedExits: []string{"INSPECTION"}, ActorRoles: []string{"CREW"}},
			{ID: "INSPECTION", AllowedExits: []string{"ACCEPTED"}, ActorRoles: []string{"INSPECTOR"}, Gates: []workordertemplate.Gate{{ID: "RECONCILIATION", Kind: "RECONCILIATION", Required: true}}},
			{ID: "ACCEPTED", AllowedExits: []string{"CLOSED"}, ActorRoles: []string{"SUPERVISOR"}},
			{ID: "CLOSED", ActorRoles: []string{"SUPERVISOR"}, Gates: []workordertemplate.Gate{{ID: "CLOSURE", Kind: "CLOSURE", Required: true}}},
		},
		Roles:          []workordertemplate.RoleGrant{{Role: "INITIATOR", Actions: []string{"create"}}, {Role: "SUPERVISOR", Actions: []string{"approve"}}, {Role: "CREW", Actions: []string{"log_work"}}, {Role: "INSPECTOR", Actions: []string{"inspect"}}},
		ReportPolicies: []workordertemplate.ReportPolicy{{ID: "FIELD_SHIFT_SUMMARY", Version: "2", Kind: workordertemplate.ReportDailyField, Required: true}, {ID: "COST", Version: "1", Kind: workordertemplate.ReportCost}, {ID: "OPEN_REQUESTS", Version: "1", Kind: workordertemplate.ReportOpenRequests}, {ID: "CLOSEOUT", Version: "1", Kind: workordertemplate.ReportCloseout}, {ID: "CUSTOM_PROGRESS_EXPORT", Version: "1", Kind: workordertemplate.ReportProgress}},
	}
	pub, err := workordertemplate.Publish(draft, workordertemplate.PublishMeta{Version: "1.0.0", PublishedBy: "owner", ReviewRef: "review:1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	amount := values.MustDecimal("125.50", 2, values.RoundingExactRequired)
	replacement := values.MustDecimal("118.25", 2, values.RoundingExactRequired)
	order := workorder.Snapshot{
		ID: "wo-1", TenantID: "tenant-a", ProjectID: "project-a", TemplateID: pub.Pin().TemplateID,
		TemplateVersion: pub.Version(), TemplateDigest: pub.Digest(), Revision: 8,
		WorkEntries: []workorder.WorkEntry{{ID: "work-1", WorkerID: "private-worker", Kind: "LABOR", WorkDate: "2026-09-25", DurationMinutes: 90, Description: "private work notes", ActorID: "private-actor", Unit: "hours", At: at}},
		Progress:    []workorder.Progress{{ID: "progress-1", LineID: "line-2", Unit: "feet", Quantity: amount, ActorID: "private-actor", EvidenceRef: "private-evidence", At: at}},
		Spending:    []workorder.Spend{{ID: "spend-1", Category: "MATERIAL", Disposition: workorder.SpendIncurred, Amount: amount, Currency: "USD", ActorID: "private-actor", Description: "private cost detail", SourceRef: "private-source", At: at}},
		Corrections: []workorder.Correction{{ID: "correction-1", TargetRecordID: "spend-1", Reason: "private correction reason", ActorID: "private-actor", ReplacementAmount: &replacement, At: at.Add(time.Minute)}},
		Requests:    []workorder.InitiatorRequest{{ID: "request-1", DefinitionID: "BUDGET", Kind: workorder.RequestBudget, Status: workorder.RequestPending, Subject: "private subject", Rationale: "private rationale", RequesterID: "private-requester", FormValues: map[string]string{"secret": "private form data"}, Amount: amount, Currency: "USD", DecisionReason: "private decision reason", CreatedAt: at}},
		Notes:       []workorder.Note{{ID: "note-1", Body: "secret field note", AuthorID: "private-author", Visibility: "PRIVATE", Classification: "RESTRICTED", AttachmentRefs: []string{"private-attachment"}}},
	}
	return order, pub
}

func TestSnapshotWorkOrderReportEnginePinsPolicyAndBuildsMinimalWatermarkedSources(t *testing.T) {
	order, pub := reportAdapterFixture(t)
	engine := NewSnapshotWorkOrderReportEngine(reportAdapterTemplates{pub}, reportAdapterOrders{order})
	definition, err := engine.ResolveDefinition(context.Background(), order.TenantID, order.ProjectID, order.ID, "FIELD_SHIFT_SUMMARY", 2)
	if err != nil {
		t.Fatal(err)
	}
	if definition.Kind != workorderreport.DailyField || definition.Digest == "" || definition.Version != 2 {
		t.Fatalf("definition does not bind the published policy: %+v", definition)
	}
	request, err := engine.BuildRequest(context.Background(), order, definition)
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Sources) != 4 {
		t.Fatalf("daily report sources = %d, want all four aggregate sources", len(request.Sources))
	}
	for _, source := range request.Sources {
		if !source.Complete || source.Watermark.Value != "revision:8" || source.Watermark.Source != source.Name {
			t.Errorf("source is not complete and revision-pinned: %+v", source)
		}
		if source.Name == "work-log" {
			fields := source.Entries[0].Fields
			if fields["quantity_status"] != "NOT_RECORDED" || fields["quantity"] != "" || fields["duration_minutes"] != "90" {
				t.Errorf("duration-only labor entry was misrepresented: %+v", fields)
			}
		}
		if source.Name == "cost" {
			if len(source.Entries) != 2 || source.Entries[0].Fields["corrected"] != "true" || source.Entries[1].Fields["record_type"] != "CORRECTION" || source.Entries[1].Fields["replacement_amount"] != "118.25" {
				t.Errorf("cost correction lineage was lost: %+v", source.Entries)
			}
			if _, leaked := source.Entries[1].Fields["reason"]; leaked {
				t.Errorf("free-form correction reason leaked into report: %+v", source.Entries[1].Fields)
			}
		}
	}
	request.Scope = workorderreport.Scope{TenantID: order.TenantID, ProjectID: order.ProjectID, WorkOrderID: order.ID}
	request.Definition = definition
	request.Authorization = workorderreport.Authorization{Principal: workorderreport.Principal{ID: "supervisor", Tenant: order.TenantID, Purpose: "work-order-report"}, Allow: func(workorderreport.Principal, workorderreport.Scope, string) bool { return true }}
	result, err := workorderreport.Generate(request)
	if err != nil || result.Status != workorderreport.StatusComplete {
		t.Fatalf("generated daily report = (%+v, %v)", result, err)
	}
	for _, renderedSource := range result.RenderInput.Sources {
		for _, row := range renderedSource.Rows {
			for _, value := range row {
				if strings.Contains(value, "private") || strings.Contains(value, "secret") {
					t.Fatalf("report leaked sensitive snapshot content: %v", row)
				}
			}
		}
	}
}

func TestSnapshotWorkOrderReportEngineFailsClosedForUnsupportedDefinitionsAndMissingSources(t *testing.T) {
	order, pub := reportAdapterFixture(t)
	engine := NewSnapshotWorkOrderReportEngine(reportAdapterTemplates{pub}, reportAdapterOrders{order})
	custom, err := engine.ResolveDefinition(context.Background(), order.TenantID, order.ProjectID, order.ID, "CUSTOM_PROGRESS_EXPORT", 1)
	if err != nil || custom.Kind != workorderreport.Progress {
		t.Fatalf("customer-defined policy ID did not resolve by its declared kind: def=%+v err=%v", custom, err)
	}
	if _, err := engine.ResolveDefinition(context.Background(), order.TenantID, order.ProjectID, order.ID, "UNKNOWN_REPORT", 1); !errors.Is(err, workorderservice.ErrInvalidRequest) {
		t.Fatalf("unknown policy resolved: %v", err)
	}
	definition, err := engine.ResolveDefinition(context.Background(), order.TenantID, order.ProjectID, order.ID, "CLOSEOUT", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = engine.BuildRequest(context.Background(), order, definition); !errors.Is(err, errWorkOrderReportSourceUnavailable) {
		t.Fatalf("closeout accepted without evidence and inspection adapters: %v", err)
	}
	wrong := definition
	wrong.Digest = "sha256:caller-forged"
	if _, err = engine.BuildRequest(context.Background(), order, wrong); !errors.Is(err, workorder.ErrConflict) {
		t.Fatalf("caller-forged report policy accepted: %v", err)
	}
}
