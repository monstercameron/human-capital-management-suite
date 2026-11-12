package application_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/continuity"
)

// TestTodo_UXFLOW_008_Served proves UXFLOW-008's continuity contract through
// the served application boundary: a complete flow validated by the surface
// yields the same normalized outcome across accessible browser routes, and a
// manual receipt with a mismatched digest is rejected.
func TestTodo_UXFLOW_008_Served(t *testing.T) {
	var nilApp *application.App
	if got := nilApp.ExperienceContinuity(); got.Execute != nil {
		t.Fatalf("nil app surface exposes capabilities, want none")
	}

	surface := application.NewServedExperienceContinuitySurface()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	flow := continuity.FlowSpec{ID: "promotion", Version: "v3", Owner: "experience", Stages: []continuity.Stage{{ID: "discover", Required: true}, {ID: "submit", Required: true}}}
	intent := map[string]string{"effective_date": "2026-10-01", "proposed_amount": "100.00"}
	_, digest, err := surface.NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("NormalizeIntent: %v", err)
	}
	base := continuity.Request{
		FlowID: flow.ID, FlowVersion: flow.Version, Subject: "worker:1", Actor: "worker:1",
		Meaning: "promotion.request.v1", Intent: intent, Deadline: now.Add(time.Hour), At: now,
		PrivacyClass: "restricted", Locale: "en-US", TemplateVersion: "tpl-1",
		WCAGEvidence: "wcag-2.2-aa", FocusEvidence: "focus-order", AnnouncementEvidence: "live-region",
	}

	browser := base
	browser.Route = continuity.RouteBrowser
	keyboard := base
	keyboard.Route = continuity.RouteKeyboard
	a, err := surface.Execute(flow, browser)
	if err != nil {
		t.Fatalf("browser route Execute: %v", err)
	}
	b, err := surface.Execute(flow, keyboard)
	if err != nil {
		t.Fatalf("keyboard route Execute: %v", err)
	}
	if a.IntentDigest != digest || !surface.Equivalent(a, b) {
		t.Fatalf("browser and keyboard routes diverge: digest=%q want %q", a.IntentDigest, digest)
	}

	manual := base
	manual.Route = continuity.RouteManual
	manual.WCAGEvidence, manual.FocusEvidence, manual.AnnouncementEvidence = "", "", ""
	manual.ManualReceipt = "0000"
	if err := surface.ValidateCompleteFlow(flow, manual); !errors.Is(err, continuity.ErrSemanticDrift) {
		t.Fatalf("manual receipt with wrong digest error = %v, want ErrSemanticDrift", err)
	}
}
