package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func issue3807Pct(n int) *int { return &n }

func issue3807Projection(viewer, invoker string, cost *PersonaCostFooter) PersonaProgressProjection {
	return PersonaProgressProjection{InvocationID: "inv-1", ViewerID: viewer, InvokerID: invoker, AgentName: "Comp Analyst", Task: &PersonaTaskCardProps{ID: "t1", Title: "Review pay bands"}, Cost: cost}
}

func TestIssue3807_FooterSaysWhatWasUsedAndWhatIsLeft(t *testing.T) {
	got := renderNode(t, issue3807CostFooter(issue3986Model("en-US"), "t", &PersonaCostFooter{UsedUnits: 3, AllowanceRemainingPct: issue3807Pct(88)}))
	for _, want := range []string{"Model calls used: 3", "Allowance left today: 88%", "agent-cost-footer"} {
		if !strings.Contains(got, want) {
			t.Errorf("footer lacks %q: %s", want, got)
		}
	}
}

func TestIssue3807_FooterIsQuietWhenThereIsNothingToSay(t *testing.T) {
	m := issue3986Model("en-US")
	for name, cost := range map[string]*PersonaCostFooter{"nil": nil, "empty": {}} {
		if node := issue3807CostFooter(m, "t", cost); node != nil {
			t.Errorf("%s cost drew a footer: %s", name, renderNode(t, node))
		}
	}
	only := renderNode(t, issue3807CostFooter(m, "t", &PersonaCostFooter{AllowanceRemainingPct: issue3807Pct(0)}))
	if !strings.Contains(only, "No allowance left today") || strings.Contains(only, "Model calls used") {
		t.Errorf("exhausted allowance: %s", only)
	}
}

func TestIssue3807_OnlyTheInvokerSeesTheirCost(t *testing.T) {
	cost := &PersonaCostFooter{UsedUnits: 4, AllowanceRemainingPct: issue3807Pct(40)}
	m := issue3986Model("en-US")
	m.RenderPersonaTask = func(PersonaTaskCardProps) ui.Node { return html.Div(html.Props{Class: "persona-task-card"}) }
	mine := renderNode(t, RenderPersonaProgress(m, issue3807Projection("alice", "alice", cost)))
	if !strings.Contains(mine, "Model calls used: 4") {
		t.Fatalf("the invoker does not see their own cost: %s", mine)
	}
	shared := renderNode(t, RenderPersonaProgress(m, issue3807Projection("bob", "alice", cost)))
	if strings.Contains(shared, "Model calls used") || strings.Contains(shared, "Allowance left") {
		t.Fatalf("a shared thread leaked the invoker's cost to another viewer: %s", shared)
	}
}

func TestIssue3807_CopyInEveryLanguage(t *testing.T) {
	cost := &PersonaCostFooter{UsedUnits: 3, AllowanceRemainingPct: issue3807Pct(88)}
	seen := map[string]bool{}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		got := renderNode(t, issue3807CostFooter(issue3986Model(locale), "t", cost))
		if locale != "en-US" && (strings.Contains(got, "Model calls") || strings.Contains(got, "Allowance")) {
			t.Errorf("%s shows English: %s", locale, got)
		}
		if seen[got] {
			t.Errorf("%s copy equals another language", locale)
		}
		seen[got] = true
	}
	if ar := renderNode(t, issue3807CostFooter(issue3986Model("ar"), "t", cost)); !strings.Contains(ar, "٣") || !strings.Contains(ar, "٨٨") {
		t.Errorf("Arabic numerals missing: %s", ar)
	}
}

func TestIssue3807_PrivateAnswerCarriesTheFooterForItsInvokerOnly(t *testing.T) {
	asked := time.Date(2026, 10, 1, 11, 59, 0, 0, time.UTC)
	build := func(viewer string) Model {
		m := agentUXReplyModel("en-US")
		m.EphemeralMessages = []EphemeralMessage{{ID: "answer", ThreadID: "question", Body: "Carry over up to five days.", OnlyVisibleToYou: true, CreatedAt: asked, ExpiresAt: asked.Add(12 * time.Hour)}}
		projection := &m.PersonaInvocations[0].Projection
		projection.Progress = nil
		projection.PrivateReplyHref = "/workspace/app/chat#channel=agent-dm"
		projection.Cost = &PersonaCostFooter{UsedUnits: 2, AllowanceRemainingPct: issue3807Pct(75)}
		projection.ViewerID = viewer
		return m
	}
	own := renderAgentUXReplyRows(t, build("alice"), "question", 1440)
	if !strings.Contains(own, "Model calls used: 2") || !strings.Contains(own, "Allowance left today: 75%") {
		t.Fatalf("the invoker's answer has no cost footer: %s", own)
	}
	other := renderAgentUXReplyRows(t, build("bob"), "question", 1440)
	if strings.Contains(other, "Model calls used") || strings.Contains(other, "Allowance left") {
		t.Fatalf("another viewer sees the invoker's cost: %s", other)
	}
}
