package journey

import (
	"strings"
	"testing"
)

func promoux008Detail(diagnostics bool) DetailView {
	return DetailView{
		Diagnostics: diagnostics,
		Journey:     JourneyCard{WorkerName: "Omar Reyes", WorkerRef: "worker:secret", IntentID: "intent-secret", InstanceID: "instance-secret", DiagnosticsAuthorized: diagnostics},
		Proposal:    []Fact{{Label: "Reason", Value: "Expanded scope"}, {Label: "Material digest", Value: "sha256:material-secret", Mono: true}},
		Engine:      []Fact{{Label: "Plan digest", Value: "sha256:plan-secret", Mono: true}},
		Nodes:       []NodeRow{{NodeID: "node-secret", StepType: "SET compensation", Status: "completed"}},
		WorkItems:   []WorkItemCard{{ID: "wi-secret", Kind: "approval", Owner: "principal:approver-secret", NodeID: "node-secret", Status: "done"}},
		Findings:    []Finding{{Severity: "warning", Code: "PROMO_INTERNAL_42", Message: "Policy explanation"}},
		Evidence:    []string{"evidence-secret"},
		Ledger:      &LedgerCard{Digest: "sha256:ledger-secret", SchemaRef: "hcmnext.Promotion/v2"},
		Timeline:    []TimelineEvent{{At: "today", Actor: "principal:approver-secret", Title: "Approval recorded", Detail: "Business approval completed", Ref: "event-secret"}},
	}
}

func TestTodo_PROMOUX_008_SecurityBoundary(t *testing.T) {
	markup := mustRender(t, Page{Detail: func() *DetailView { v := promoux008Detail(false); return &v }()})
	for _, forbidden := range []string{"worker:secret", "intent-secret", "instance-secret", "sha256:material-secret", "sha256:plan-secret", "node-secret", "wi-secret", "principal:approver-secret", "PROMO_INTERNAL_42", "evidence-secret", "sha256:ledger-secret", "event-secret", "JourneyService"} {
		if strings.Contains(markup, forbidden) {
			t.Errorf("unauthorized promotion view leaked %q: %s", forbidden, markup)
		}
	}
	if strings.Contains(markup, `class="jn-diagnostics"`) || strings.Contains(markup, "Technical details") {
		t.Fatalf("unauthorized view exposed a diagnostics disclosure or its contents: %s", markup)
	}
	if !strings.Contains(markup, "Approval recorded") || !strings.Contains(markup, "Business approval completed") {
		t.Fatalf("business timeline was not preserved: %s", markup)
	}
}

func TestTodo_PROMOUX_008_Accessibility_SecurityBoundary(t *testing.T) {
	markup := mustRender(t, Page{Locale: "de-DE", Detail: func() *DetailView { v := promoux008Detail(true); return &v }()})
	if !strings.Contains(markup, "Systemdiagnose") || !strings.Contains(markup, "Approval recorded") {
		t.Fatalf("authorized diagnostics or business history lost accessible presentation: %s", markup)
	}
}

func TestTodo_PROMOUX_008_Security(t *testing.T) {
	unauthorized := mustRender(t, Page{Detail: func() *DetailView { v := promoux008Detail(false); return &v }()})
	authorized := mustRender(t, Page{Detail: func() *DetailView { v := promoux008Detail(true); return &v }()})
	if strings.Count(unauthorized, `class="jn-diagnostics"`) != 0 || strings.Count(authorized, `class="jn-diagnostics"`) != 1 {
		t.Fatalf("diagnostics presence is not authorization-bound: unauthorized=%d authorized=%d", strings.Count(unauthorized, `class="jn-diagnostics"`), strings.Count(authorized, `class="jn-diagnostics"`))
	}
	for _, want := range []string{"sha256:plan-secret", "node-secret", "wi-secret", "event-secret"} {
		if !strings.Contains(authorized, want) || strings.Contains(unauthorized, want) {
			t.Errorf("authorization projection for %q is incorrect", want)
		}
	}
	if strings.Contains(authorized, "intent-secret") || !strings.Contains(authorized, maskIdentifier("intent-secret")) {
		t.Error("authorized identifier must still be masked on screen")
	}
	if strings.Contains(unauthorized, "principal:approver-secret") || strings.Contains(unauthorized, "journeyservice") {
		t.Fatalf("protocol principal escaped into business history: %s", unauthorized)
	}
}

func TestTodo_PROMOUX_008_I18N(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup := mustRender(t, Page{Locale: locale, Detail: func() *DetailView { v := promoux008Detail(false); return &v }()})
		if strings.Contains(markup, "⟦journey.") || strings.Contains(markup, "event-secret") {
			t.Errorf("%s exposed unresolved copy or a trace reference", locale)
		}
	}
}
