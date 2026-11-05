package application_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/wedge"
)

var servedWedgeAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func servedWedgeMetric() wedge.AdoptionMetric {
	return wedge.AdoptionMetric{
		SchemaVersion: wedge.WedgeSchemaVersion, MetricID: "promotion-adoption-v1",
		Window: wedge.EvidenceWindow{From: servedWedgeAt, To: servedWedgeAt.Add(24 * time.Hour), Source: "PLACEHOLDER_SOURCE_SYSTEM_EXPORT", Exclusions: []string{"PLACEHOLDER_EXCLUSION"}},
		Taxonomy: wedge.BypassTaxonomy{Version: "v1", Codes: []wedge.BypassReason{
			{Code: "MANUAL", Label: "Manual partner handling"},
			{Code: "INCUMBENT", Label: "Incumbent workflow"},
			{Code: "SPREADSHEET", Label: "Spreadsheet workflow"},
			{Code: "EMERGENCY", Label: "Emergency path"},
		}},
		Records: []wedge.AdoptionRecord{
			{TransactionID: "txn-1", Eligible: true, Path: wedge.AdoptionHCMNext},
			{TransactionID: "txn-2", Eligible: true, Path: wedge.AdoptionApprovedBypass, BypassCode: "MANUAL"},
			{TransactionID: "txn-3", Eligible: true, Path: wedge.AdoptionUnexplained},
			{TransactionID: "txn-ignored", Eligible: false},
		},
	}
}

// TestTodo_WEDGE_006_Served proves WEDGE-006's GREEN clause through the served
// application boundary: every eligible transaction classifies as HCM Next,
// approved bypass or unexplained exception, and a hidden bypass or an
// unregistered code is rejected rather than silently absorbed.
func TestTodo_WEDGE_006_Served(t *testing.T) {
	surface := application.NewServedWedgeSurface()
	result, err := surface.EvaluateAdoption(servedWedgeMetric())
	if err != nil {
		t.Fatalf("served EvaluateAdoption: %v", err)
	}
	if result.Eligible != 3 || result.HCMNext != 1 || result.ApprovedBypass != 1 || result.Unexplained != 1 {
		t.Fatalf("served adoption result = %+v, want 3 eligible split 1/1/1", result)
	}
	if result.MetricDigest == "" {
		t.Fatal("served adoption result has no metric digest")
	}

	hidden := servedWedgeMetric()
	hidden.Records[1].Path = ""
	if _, err := surface.EvaluateAdoption(hidden); !errors.Is(err, wedge.ErrInvalidWedgeRecord) {
		t.Fatalf("served hidden bypass error = %v, want invalid adoption metric", err)
	}
	unregistered := servedWedgeMetric()
	unregistered.Records[1].BypassCode = "NOT_IN_TAXONOMY"
	if _, err := surface.EvaluateAdoption(unregistered); !errors.Is(err, wedge.ErrInvalidWedgeRecord) {
		t.Fatalf("served unregistered bypass error = %v, want invalid adoption metric", err)
	}
}

func TestServedWedgeAppSurface(t *testing.T) {
	var app application.App
	if got := app.Wedge(); got.EvaluateAdoption == nil || got.ValidateAdoption == nil {
		t.Fatal("composed application omitted Wedge surface")
	}
	var nilApp *application.App
	if got := nilApp.Wedge(); got.EvaluateAdoption != nil {
		t.Fatal("nil application exposed Wedge capabilities")
	}
}
