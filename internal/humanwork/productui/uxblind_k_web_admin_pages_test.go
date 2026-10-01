package productui

import (
	"strings"
	"testing"
)

func TestTodo_WEB_230_Integration(t *testing.T) {
	view := testView(PagePolicyStudio)
	view.PolicyStudio = &PolicyStudioProjection{
		Ready:       true,
		PolicyKey:   "people.promotion",
		Version:     "2026.09",
		PublishedAt: "2026-09-28T12:00:00Z",
		Rules: []PolicyRuleProjection{{
			ID: "promotion.manager.approval", Effect: "allow", Subject: "manager", Capability: "promotion.approve",
			Resource: "worker.compensation", DataDomain: "rewards", Purpose: "promotion_review", Condition: "same_org", Version: "2026.09",
		}},
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"policy-studio-page", "Live service projection", "people.promotion", "promotion.manager.approval", "promotion.approve", "promotion_review"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("live Policy Studio projection missing %q: %s", want, doc)
		}
	}
	if strings.Contains(doc, "policy_studio.unavailable_title") {
		t.Fatal("live Policy Studio projection retained the unavailable state")
	}
}

func TestTodo_WEB_230_Security(t *testing.T) {
	view := testView(PagePolicyStudio)
	view.PolicyStudio = &PolicyStudioProjection{Ready: true, CanEdit: true, EditHref: "javascript:alert(1)", Rules: []PolicyRuleProjection{{ID: "safe.rule"}}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(doc), "javascript:") {
		t.Fatal("Policy Studio rendered an unsafe projected edit destination")
	}
	if !strings.Contains(doc, "safe.rule") {
		t.Fatal("Policy Studio dropped the authorized rule projection")
	}
}

func TestTodo_WEB_230_Fault(t *testing.T) {
	view := testView(PagePolicyStudio)
	view.PolicyStudio = &PolicyStudioProjection{Ready: true, Rules: []PolicyRuleProjection{{ID: ""}}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "policy-studio-page") || strings.Contains(doc, `data-service-state="unavailable"`) {
		t.Fatal("Policy Studio did not retain a deterministic ready empty result")
	}
}

func TestTodo_WEB_231_Integration(t *testing.T) {
	view := testView(PagePolicySimulation)
	view.PolicySimulation = &PolicySimulationProps{
		Subject: "Avery Patel (manager)", Allowed: false, ReasonDetail: "Compensation fields are withheld for this purpose.",
		Rules: []string{"compensation.withhold"}, Versions: []string{"policy-2026.09"}, EvidenceRef: "ev:authz:230",
		ExitHref: "/workspace/app/home",
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"policy-simulation-page", "Avery Patel (manager)", "Compensation fields are withheld", "compensation.withhold", "policy-2026.09", "ev:authz:230"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("live policy simulation projection missing %q: %s", want, doc)
		}
	}
}

func TestTodo_WEB_231_Security(t *testing.T) {
	view := testView(PagePolicySimulation)
	view.PolicySimulation = &PolicySimulationProps{Subject: "forbidden subject", ExitHref: "javascript:alert(1)"}
	view.SignedOut = &SignedOutProps{}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "forbidden subject") || strings.Contains(strings.ToLower(doc), "javascript:") {
		t.Fatal("policy simulation exposed a projection after sign-out or an unsafe exit")
	}
}

func TestTodo_WEB_231_Fault(t *testing.T) {
	view := testView(PagePolicySimulation)
	view.PolicySimulation = &PolicySimulationProps{Subject: "   "}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, `data-service-state="ready"`) {
		t.Fatal("blank simulation subject produced a live simulation page")
	}
}

func TestTodo_WEB_232_Integration(t *testing.T) {
	view := testView(PageConfigurationCenter)
	view.ConfigurationCenter = &ConfigurationCenterProjection{Ready: true, Sections: []ConfigurationSectionProjection{{
		ID: "appearance", Label: "Brand and appearance", Description: "Organization-wide identity", State: "published", Version: "4", UpdatedAt: "2026-09-28", Href: "/workspace/app/admin/appearance",
	}}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"configuration-center-page", "Brand and appearance", "Organization-wide identity", "published", "/workspace/app/admin/appearance"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("live configuration projection missing %q: %s", want, doc)
		}
	}
}

func TestTodo_WEB_232_Security(t *testing.T) {
	view := testView(PageConfigurationCenter)
	view.ConfigurationCenter = &ConfigurationCenterProjection{Ready: true, Sections: []ConfigurationSectionProjection{{ID: "unsafe", Label: "Unsafe", Href: "javascript:alert(1)"}}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(doc), "javascript:") {
		t.Fatal("configuration center rendered an unsafe projected destination")
	}
	if !strings.Contains(doc, "Unsafe") {
		t.Fatal("configuration center dropped the authorized section projection")
	}
}

func TestTodo_WEB_232_Fault(t *testing.T) {
	view := testView(PageConfigurationCenter)
	view.ConfigurationCenter = &ConfigurationCenterProjection{Ready: true, Sections: []ConfigurationSectionProjection{{ID: "", Label: ""}}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "configuration-center-page") || !strings.Contains(doc, "No configuration areas") {
		t.Fatal("configuration center did not render its authorized empty result")
	}
}
