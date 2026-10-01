package application_test

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/reporting"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/reportrender"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/reportschedule"
)

func servedReportDefinition() reporting.Definition {
	return reporting.Definition{
		ID: "served-headcount", Version: "v1", Owner: "analytics",
		SourceIDs: []string{"people"}, Fields: []string{"worker.id"},
		Population: "active-workers", RenderFormats: []string{"table"}, ExportFormats: []string{"csv"},
	}
}

func TestTodo_REPORT_001_Served(t *testing.T) {
	surface := application.NewServedReportingSurface()
	if surface.NewDefinitionRegistry == nil || surface.PublishDefinition == nil || surface.PublishDashboard == nil {
		t.Fatal("served reporting surface omitted immutable definition operations")
	}
	registry := surface.NewDefinitionRegistry()
	for _, err := range []error{
		registry.RegisterSource(reporting.Source{ID: "people", Version: "v1", Owner: "people"}),
		registry.RegisterField(reporting.Field{ID: "worker.id", Type: "string", SourceID: "people"}),
		registry.RegisterPopulation(reporting.Population{ID: "active-workers", SourceID: "people", Predicate: "active"}),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	published, err := surface.PublishDefinition(registry, servedReportDefinition())
	if err != nil || published.CompatibilityDigest == "" {
		t.Fatalf("served report definition = %+v, err=%v", published, err)
	}
	if (&application.App{}).Reporting().PublishDefinition == nil {
		t.Fatal("composed application omitted report definition capability")
	}
}

func TestTodo_REPORT_002_Served(t *testing.T) {
	surface := application.NewServedReportingSurface()
	if surface.ExecuteReport == nil || surface.RenderReport == nil || surface.ExportReport == nil {
		t.Fatal("served reporting surface omitted execution and output operations")
	}
	definition, err := reportrender.Publish(reportrender.ReportDefinition{ID: "served", Version: 1, Fields: []string{"name"}, Population: "tenant/workers"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := surface.ExecuteReport(reportrender.ExecutionRequest{
		Definition: definition,
		Authorization: reportrender.Authorization{
			Principal: reportrender.Principal{ID: "analyst", Tenant: "tenant", Purpose: "planning"},
			Scope:     reportrender.Scope{Population: "tenant/workers", Fields: []string{"name"}},
			Allow:     func(reportrender.Principal, reportrender.Scope) bool { return true },
		},
		Sources: []reportrender.Source{{Name: "people", Complete: true, Watermark: reportrender.Watermark{Source: "people", Value: "1"}, Rows: []reportrender.Row{{"name": "Ada"}}}},
	})
	if err != nil || result.RowDigest == "" {
		t.Fatalf("served report result = %+v, err=%v", result, err)
	}
	view, err := surface.RenderReport(result, "json")
	if err != nil || len(view.Bytes) == 0 || view.Watermark != "people=1" {
		t.Fatalf("served report view = %+v, err=%v", view, err)
	}
	if (&application.App{}).Reporting().ExecuteReport == nil {
		t.Fatal("composed application omitted report execution capability")
	}
}

func TestTodo_REPORT_003_Served(t *testing.T) {
	surface := application.NewServedReportingSurface()
	if surface.NewScheduleStore == nil || surface.CreateSchedule == nil || surface.RunScheduledReport == nil || surface.Reproduce == nil {
		t.Fatal("served reporting surface omitted schedule and reproduction operations")
	}
	schedule := reportschedule.Schedule{
		ID: "served-dashboard", TenantID: "tenant", Every: time.Hour,
		Definition: reportschedule.Definition{ID: "report", Version: "v1", Digest: "definition"},
		Data:       reportschedule.Data{ID: "data", Version: "v1", Digest: "data"},
		Control:    reportschedule.Control{ID: "control", Version: "v1", Digest: "control"},
		Locale:     reportschedule.Locale{ID: "en-US", Version: "v1", Digest: "locale"},
	}
	store := surface.NewScheduleStore(1)
	if _, err := surface.CreateSchedule(store, schedule); err != nil {
		t.Fatal(err)
	}
	run, err := surface.RunScheduledReport(store, schedule.ID, reportschedule.Authorization{PrincipalID: "analyst", TenantID: "tenant", Purpose: "planning"}, time.Unix(100, 0), "evidence", "secure", "attachment")
	if err != nil || run.TenantID != "tenant" || !run.Delivery.Authorized {
		t.Fatalf("served scheduled run = %+v, err=%v", run, err)
	}
	if (&application.App{}).Reporting().RunScheduledReport == nil {
		t.Fatal("composed application omitted report scheduling capability")
	}
}
