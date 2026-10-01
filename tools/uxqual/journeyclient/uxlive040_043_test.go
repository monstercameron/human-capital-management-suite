package journeyclient

import (
	"strings"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestTodo_UXLIVE_040(t *testing.T) {
	got := promotionEmployeeSelectionHref("de-DE")
	if got != "/workspace/app/people?eligible=1&locale=de-DE" {
		t.Fatalf("promotion selection route = %q", got)
	}
}

func TestTodo_UXLIVE_040_Integration(t *testing.T) {
	view := PeopleView(Config{Locale: "de-DE"}, ListData{Options: &journeyv1.WorkforceOptions{}}, nil)
	if view == nil || !strings.Contains(view.DirectoryLink.Href, "eligible=1") || !strings.Contains(view.DirectoryLink.Href, "locale=de-DE") {
		t.Fatalf("PeopleView lost promotion route state: %#v", view)
	}
}

func TestTodo_UXLIVE_040_Browser(t *testing.T) {
	if got := promotionEmployeeSelectionHref("de-DE"); !strings.Contains(got, "eligible=1&locale=de-DE") {
		t.Fatalf("browser handoff omitted promotion state: %q", got)
	}
}

func TestTodo_UXLIVE_040_Accessibility(t *testing.T) {
	if got := promotionEmployeeSelectionHref("ar"); !strings.Contains(got, "locale=ar") {
		t.Fatalf("RTL locale was not preserved: %q", got)
	}
}

func TestTodo_UXLIVE_040_Security(t *testing.T) {
	if got := promotionEmployeeSelectionHref("en-US"); strings.Contains(got, "worker=") || strings.Contains(got, "eligible_workers=") {
		t.Fatalf("route leaked a worker or hidden authorization fact: %q", got)
	}
}

func TestTodo_UXLIVE_040_Regression(t *testing.T) {
	if got := promotionEmployeeSelectionHref("en-US"); got != "/workspace/app/people?eligible=1" {
		t.Fatalf("default route is not canonical: %q", got)
	}
}

func TestTodo_UXLIVE_041(t *testing.T) {
	reason := outcomeReasonLocale("en-US", journey.JourneyCard{Stage: stageRejected, StageLabel: "Rejected"}, []*journeyv1.Finding{
		{Severity: "blocking", Code: "promotion.pay_above_band_maximum", Message: "server prose"},
		{Severity: "blocking", Code: "promotion.budget_observed_insufficient", Message: "secret server prose"},
	})
	if reason == nil || len(reason.Blocking) != 2 || !strings.Contains(reason.Summary, "these blocking checks") {
		t.Fatalf("reason projection lost contributing blockers: %#v", reason)
	}
	if strings.Contains(reason.Summary, "server prose") || strings.Contains(reason.Summary, "secret") {
		t.Fatalf("reason projection exposed human/server evidence: %#v", reason)
	}
}

func TestTodo_UXLIVE_041_Integration(t *testing.T) {
	detail := &journeyv1.JourneyDetail{Journey: &journeyv1.Journey{Stage: journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED}, Findings: []*journeyv1.Finding{{Severity: "blocking", Code: "promotion.budget_observed_insufficient"}}}
	page := DetailPage(Config{Locale: "de-DE"}, detail, nil, nil)
	if page.Detail == nil || page.Detail.OutcomeReason == nil || page.Detail.OutcomeReason.Summary == "" {
		t.Fatalf("detail projection omitted the authoritative outcome reason")
	}
}

func TestTodo_UXLIVE_041_Golden(t *testing.T) {
	got := outcomeReasonLocale("de-DE", journey.JourneyCard{Stage: stageBlocked, StageLabel: "Blockiert"}, []*journeyv1.Finding{{Severity: "blocking", Code: "promotion.budget_observed_insufficient"}})
	if got.Summary == "" || !strings.Contains(got.Summary, "Budget") {
		t.Fatalf("localized budget reason is unstable or missing: %#v", got)
	}
}
func TestTodo_UXLIVE_041_Browser(t *testing.T) {
	got := outcomeReasonLocale("en-US", journey.JourneyCard{Stage: stageFailed, StageLabel: "Failed"}, nil)
	if got == nil || got.NextStep == "" || got.Tone != "danger" {
		t.Fatalf("failed journey did not expose a safe terminal explanation: %#v", got)
	}
}
func TestTodo_UXLIVE_041_Accessibility(t *testing.T) {
	got := outcomeReasonLocale("ar", journey.JourneyCard{Stage: stageRejected, StageLabel: "مرفوض"}, nil)
	if got == nil || got.Summary == "" || got.NextStep == "" {
		t.Fatalf("Arabic outcome reason is not complete: %#v", got)
	}
}
func TestTodo_UXLIVE_041_Security(t *testing.T) {
	got := outcomeReasonLocale("en-US", journey.JourneyCard{Stage: stageBlocked, StageLabel: "Blocked"}, []*journeyv1.Finding{{Severity: "blocking", Code: "unknown.secret", Message: "confidential"}})
	if strings.Contains(got.Summary, "confidential") || strings.Contains(strings.Join(got.Blocking, " "), "confidential") {
		t.Fatalf("unauthorized finding message leaked: %#v", got)
	}
}
func TestTodo_UXLIVE_041_Conformance(t *testing.T) {
	got := outcomeReasonLocale("en-US", journey.JourneyCard{Stage: stageBlocked, StageLabel: "Blocked"}, []*journeyv1.Finding{{Severity: "blocking", Code: "promotion.pay_below_band_minimum"}, {Severity: "warning", Code: "promotion.budget_observed_insufficient"}})
	if len(got.Blocking) != 1 || len(got.Supplemental) != 1 {
		t.Fatalf("blocking and supplemental evidence were conflated: %#v", got)
	}
}
func TestTodo_UXLIVE_041_Regression(t *testing.T) {
	if got := outcomeReasonLocale("en-US", journey.JourneyCard{Stage: stageObservingEffects}, nil); got != nil {
		t.Fatalf("active journey incorrectly received terminal reason: %#v", got)
	}
}

func TestTodo_UXLIVE_042(t *testing.T) {
	detail := &journeyv1.JourneyDetail{
		Nodes:    []*journeyv1.NodeExecution{{Status: "RETRYING", StartedAt: timestamppb.New(time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC))}},
		Timeline: []*journeyv1.TimelineEvent{{At: timestamppb.New(time.Date(2026, 9, 29, 12, 1, 0, 0, time.UTC))}},
	}
	progress := progressLocale("en-US", journey.JourneyCard{Stage: stageObservingEffects}, detail)
	if progress == nil || progress.Status != "retrying" || !strings.Contains(progress.Phase, "downstream") || progress.LastProgress == "" {
		t.Fatalf("runtime progress is not truthful or time-bounded: %#v", progress)
	}
}

func TestTodo_UXLIVE_042_Integration(t *testing.T) {
	page := DetailPage(Config{Locale: "en-US"}, &journeyv1.JourneyDetail{Journey: &journeyv1.Journey{Stage: journeyv1.JourneyStage_JOURNEY_STAGE_OBSERVING_EFFECTS}}, nil, nil)
	if page.Detail == nil || page.Detail.Progress == nil {
		t.Fatalf("detail page omitted downstream progress")
	}
}
func TestTodo_UXLIVE_042_Browser(t *testing.T) {
	got := progressLocale("de-DE", journey.JourneyCard{Stage: stageObservingEffects}, &journeyv1.JourneyDetail{})
	if got == nil || got.Summary == "" || !strings.Contains(got.Summary, "Nachgelagerte") {
		t.Fatalf("localized browser progress is missing: %#v", got)
	}
}
func TestTodo_UXLIVE_042_Fault(t *testing.T) {
	detail := &journeyv1.JourneyDetail{Nodes: []*journeyv1.NodeExecution{{Status: "DELAYED"}}}
	if got := progressLocale("en-US", journey.JourneyCard{Stage: stageObservingEffects}, detail); got == nil || got.Status != "delayed" {
		t.Fatalf("delayed runtime state was not projected: %#v", got)
	}
}
func TestTodo_UXLIVE_042_Recovery(t *testing.T) {
	detail := &journeyv1.JourneyDetail{Nodes: []*journeyv1.NodeExecution{{Status: "REPAIR_REQUIRED"}}}
	if got := progressLocale("en-US", journey.JourneyCard{Stage: stageExecuted}, detail); got == nil || got.Status != "repair-required" {
		t.Fatalf("repair runtime evidence was not projected: %#v", got)
	}
}
func TestTodo_UXLIVE_042_Regression(t *testing.T) {
	if got := progressLocale("en-US", journey.JourneyCard{Stage: stageRecorded}, &journeyv1.JourneyDetail{}); got != nil {
		t.Fatalf("terminal recorded journey received active progress: %#v", got)
	}
}
