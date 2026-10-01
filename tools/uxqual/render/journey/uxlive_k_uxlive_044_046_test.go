package journey

import (
	"strings"
	"testing"
)

func uxliveKCard(stage string) JourneyCard {
	stageLabel := map[string]string{
		"PROPOSED": "Ready to start approval", "BLOCKED": "Blocked", "AWAITING_APPROVAL": "Awaiting approval",
		"FINANCE_APPROVAL": "Finance approval", "MANAGER_APPROVAL": "Manager approval", "REAPPROVAL": "Approval required again",
		"WAITING_EFFECTIVE_DATE": "Waiting for effective date", "REVALIDATION": "Final checks", "EXECUTED": "Recording promotion",
		"OBSERVING_EFFECTS": "Checking downstream effects", "COMPLETED": "Completed", "RECORDED": "Recorded", "REJECTED": "Rejected", "FAILED": "Failed",
	}[stage]
	nextStep := "Manager review"
	if stage == "COMPLETED" || stage == "RECORDED" || stage == "REJECTED" || stage == "FAILED" {
		nextStep = ""
	}
	return JourneyCard{
		IntentID:   "intent-1",
		Href:       "/journeys/intent-1",
		WorkerName: "Amara Okafor",
		Stage:      stage,
		StageLabel: stageLabel,
		StageTone:  toneInfo,
		NextStep:   nextStep,
		TargetPosition: PositionIdentity{
			Title: "Senior HR Business Partner", Code: "OPS-HRBP3", State: PositionIdentityResolved,
		},
	}
}

func TestTodo_UXLIVE_044(t *testing.T) {
	markup := renderNode(t, journeyCardLocale("en-US", uxliveKCard("MANAGER_APPROVAL")))
	for _, want := range []string{"Senior HR Business Partner", "OPS-HRBP3", "Position"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("position identity omitted %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "70ae5423-59a9-55ba-abf3-00f495284b09") {
		t.Fatal("default card scan exposed a raw position token")
	}
}

func TestTodo_UXLIVE_044_Golden(t *testing.T) {
	markup := renderNode(t, heroSectionLocale("en-US", uxliveKCard("COMPLETED"), false))
	want := `<p class="jn-position-identity"><span class="jn-meta-key">Position </span><span class="jn-position-value">Senior HR Business Partner</span><span class="jn-position-code" dir="ltr" translate="no"> · OPS-HRBP3</span></p>`
	if !strings.Contains(markup, want) {
		t.Fatalf("position identity golden fragment changed:\n%s", markup)
	}
}

func TestTodo_UXLIVE_044_Browser(t *testing.T) {
	markup := renderNode(t, journeyCardLocale("en-US", uxliveKCard("AWAITING_APPROVAL")))
	if !strings.Contains(markup, `class="jn-position-identity"`) || !strings.Contains(markup, `dir="ltr"`) {
		t.Fatalf("position identity is not a stable, readable browser region: %s", markup)
	}
}

func TestTodo_UXLIVE_044_Security(t *testing.T) {
	for _, identity := range []PositionIdentity{
		{Code: "70ae5423-59a9-55ba-abf3-00f495284b09", State: PositionIdentityUnresolved},
		{Code: "position-revision-7", State: PositionIdentityWithheld},
	} {
		markup := renderNode(t, journeyCardLocale("en-US", JourneyCard{WorkerName: "Amara", TargetPosition: identity}))
		if strings.Contains(markup, "70ae5423-59a9-55ba-abf3-00f495284b09") || strings.Contains(markup, "position-revision-7") {
			t.Fatalf("raw identity crossed the safe renderer boundary: %s", markup)
		}
		if !strings.Contains(markup, "Position unavailable") && !strings.Contains(markup, "Position not shown") {
			t.Fatalf("missing explicit safe identity state: %s", markup)
		}
	}
}

func TestTodo_UXLIVE_044_Regression(t *testing.T) {
	markup := renderNode(t, journeyCardLocale("en-US", JourneyCard{WorkerName: "Amara", Headline: "OPS-HRBP2 · P2 → OPS-HRBP3 · P3"}))
	if strings.Contains(markup, "Position unavailable") || strings.Contains(markup, "⟦journey.") {
		t.Fatalf("legacy position-less card gained an invented or unresolved identity: %s", markup)
	}
}

func TestTodo_UXLIVE_045(t *testing.T) {
	for stage, want := range map[string]string{
		"PROPOSED":               "Start approval",
		"BLOCKED":                "Correct proposal",
		"MANAGER_APPROVAL":       "Complete approval",
		"WAITING_EFFECTIVE_DATE": "Track request",
		"COMPLETED":              "Review decision",
		"REJECTED":               "Review decision",
		"FAILED":                 "Review decision",
	} {
		markup := renderNode(t, journeyCardLocale("en-US", uxliveKCard(stage)))
		if !strings.Contains(markup, want) || strings.Contains(markup, ">Open request<") {
			t.Fatalf("%s action = %q, markup: %s", stage, want, markup)
		}
	}
}

func TestTodo_UXLIVE_045_Golden(t *testing.T) {
	markup := renderNode(t, journeyCardLocale("en-US", uxliveKCard("REJECTED")))
	if !strings.Contains(markup, `aria-label="Promotion for Amara Okafor, Request NTENT1, Rejected — Review decision"`) {
		t.Fatalf("terminal accessible action drifted: %s", markup)
	}
}

func TestTodo_UXLIVE_045_Browser(t *testing.T) {
	markup := renderNode(t, journeyCardLocale("en-US", uxliveKCard("MANAGER_APPROVAL")))
	if !strings.Contains(markup, "Manager review") || !strings.Contains(markup, "Complete approval") {
		t.Fatalf("open card did not expose its next useful step and action: %s", markup)
	}
	if strings.Contains(markup, "Technical details") {
		t.Fatal("unauthorized card exposed diagnostics in the default scan")
	}
}

func TestTodo_UXLIVE_045_Accessibility(t *testing.T) {
	markup := renderNode(t, journeyCardLocale("en-US", uxliveKCard("FAILED")))
	if !strings.Contains(markup, `aria-label="Promotion for Amara Okafor, Request NTENT1, Failed — Review decision"`) {
		t.Fatalf("accessible name omitted person, reference, status or action: %s", markup)
	}
}

func TestTodo_UXLIVE_045_I18N(t *testing.T) {
	for locale, wants := range map[string][]string{
		"de-DE": {"Entscheidung prüfen", "Position"},
		"ar":    {"مراجعة القرار", "المنصب"},
	} {
		markup := renderNode(t, journeyCardLocale(locale, uxliveKCard("REJECTED")))
		for _, want := range wants {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s missing translated %q: %s", locale, want, markup)
			}
		}
		if strings.Contains(markup, "⟦journey.") {
			t.Fatalf("%s exposed an unresolved catalog key: %s", locale, markup)
		}
	}
}

func TestTodo_UXLIVE_045_Regression(t *testing.T) {
	markup := renderNode(t, journeyCardLocale("en-US", JourneyCard{WorkerName: "Amara", Stage: "NEW_SCHEMA_STAGE"}))
	if !strings.Contains(markup, "Review request") || strings.Contains(markup, "Open request") {
		t.Fatalf("unknown stage fell back to generic open copy: %s", markup)
	}
}

func TestTodo_UXLIVE_046(t *testing.T) {
	stages := []string{"PROPOSED", "FINANCE_APPROVAL", "MANAGER_APPROVAL", "WAITING_EFFECTIVE_DATE", "COMPLETED", "BLOCKED", "REJECTED", "FAILED"}
	for _, stage := range stages {
		markup := renderNode(t, journeyCardLocale("en-US", uxliveKCard(stage)))
		if strings.Contains(markup, "Open request") || strings.Contains(markup, "Technical details") {
			t.Fatalf("release gate found generic action or diagnostics in %s: %s", stage, markup)
		}
	}
}
