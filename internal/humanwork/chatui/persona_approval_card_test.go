package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/agentapproval"
)

func personaApprovalCardFixture(t *testing.T, now time.Time) PersonaApprovalCard {
	t.Helper()
	approval := agentapproval.AgentActionApproval{
		ID: "approval-1", TaskID: "task-1", AssignedUserID: "user-1", State: agentapproval.StatePending,
		Items:         []agentapproval.ActionItem{{ID: "item-1", Kind: agentapproval.ItemGovernedIntent, Tier: agentapproval.TierSubmitGoverned, IntentDefinitionID: "promotion.submit", IntentDefinitionVersion: "v1", MaterialFields: []agentapproval.MaterialField{{Path: "job.level", Before: "L3", After: "L4"}}, Sources: []agentapproval.Source{{Ref: "plan-1", Taint: "TRUSTED_INTERNAL"}}, Uncertainty: "source is current"}},
		TaskExpiresAt: now.Add(time.Hour), ExpiresAt: now.Add(time.Hour),
	}
	approval.Digest = agentapproval.Digest(approval)
	card, err := BuildPersonaApprovalCard(approval, "invocation-1", "user-1", now)
	if err != nil {
		t.Fatal(err)
	}
	return card
}

func renderPersonaApprovalTest(t *testing.T, model Model, card PersonaApprovalCard, now time.Time) string {
	t.Helper()
	markup, err := ui.RenderToString(RenderPersonaApprovalCard(model, card, now))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func TestTodo_AGENTP_014_ChatPersonaApprovalCard(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	card := personaApprovalCardFixture(t, now)
	markup := renderPersonaApprovalTest(t, Model{CurrentUser: "user-1"}, card, now.Add(time.Minute))
	for _, want := range []string{"agent-delivery=\"ephemeral\"", "agent-visibility=\"invoker\"", "agent-item-digest=\"" + card.Digest + "\"", "Open in task view", "promotion.submit"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("card missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "agent-approval-action") || strings.Contains(markup, "Approve") {
		t.Fatalf("chat card exposed an approval action without backend support: %s", markup)
	}
}

func TestTodo_AGENTP_014_ChatPersonaApprovalCardSecurity(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	card := personaApprovalCardFixture(t, now)
	if got := renderPersonaApprovalTest(t, Model{CurrentUser: "colleague"}, card, now); got != "" {
		t.Fatalf("non-invoker saw card: %s", got)
	}
	card.Card.Values["actions"] = `<script>alert("x")</script>`
	markup := renderPersonaApprovalTest(t, Model{CurrentUser: "user-1"}, card, now)
	if strings.Contains(markup, "<script>") || !strings.Contains(markup, "&lt;script&gt;") {
		t.Fatalf("untrusted card text was not escaped: %s", markup)
	}
	card.Card.Values["task_view"] = "https://attacker.example/collect"
	if got := renderPersonaApprovalTest(t, Model{CurrentUser: "user-1"}, card, now); strings.Contains(got, "attacker.example") {
		t.Fatalf("external task URL rendered: %s", got)
	}
}

func TestTodo_AGENTP_014_ChatPersonaApprovalCardExpiryAndTaskOnlyRisk(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	card := personaApprovalCardFixture(t, now)
	card.ExpiresAt = now.Add(15 * time.Minute)
	card.Card.Values["expires_at"] = card.ExpiresAt.Format(time.RFC3339)
	if got := renderPersonaApprovalTest(t, Model{CurrentUser: "user-1"}, card, card.ExpiresAt); !strings.Contains(got, "no longer available") || strings.Contains(got, "persona-approval-task-link") {
		t.Fatalf("expired card remained actionable: %s", got)
	}
	card.Card.Values["review_reason"] = "external_write_requires_task_view"
	markup := renderPersonaApprovalTest(t, Model{CurrentUser: "user-1"}, card, now.Add(time.Minute))
	if !strings.Contains(markup, "external_write_requires_task_view") || strings.Contains(markup, "agent-approval-action") {
		t.Fatalf("high-risk card offered chat approval: %s", markup)
	}
}
