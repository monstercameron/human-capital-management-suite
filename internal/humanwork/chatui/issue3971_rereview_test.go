package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentapproval"
)

func issue3971Cards(t *testing.T, now time.Time, mutate func(*agentapproval.AgentActionApproval), source *agentapproval.SourceMessageInput) (PersonaApprovalCard, agentapproval.AgentActionApproval) {
	t.Helper()
	build := func(id string) agentapproval.AgentActionApproval {
		approval := agentapproval.AgentActionApproval{
			ID: id, TenantID: "tenant-1", TaskID: "task-1", AssignedUserID: "user-1", State: agentapproval.StatePending,
			Items:         []agentapproval.ActionItem{{ID: "item-1", Kind: agentapproval.ItemGovernedIntent, Tier: agentapproval.TierSubmitGoverned, IntentDefinitionID: "promotion.submit", IntentDefinitionVersion: "v1", Subjects: []agentapproval.Subject{{ID: "worker-7", Kind: "worker"}}, MaterialFields: []agentapproval.MaterialField{{Path: "job.level", Before: "L3", After: "L4"}}, Sources: []agentapproval.Source{{Ref: "plan-1", Taint: "TRUSTED_INTERNAL"}}, Uncertainty: "source is current", RiskClass: "low"}},
			TaskExpiresAt: now.Add(time.Hour), ExpiresAt: now.Add(time.Hour),
		}
		approval.Digest = agentapproval.Digest(approval)
		return approval
	}
	previous, current := build("approval-1"), build("approval-2")
	if mutate != nil {
		mutate(&current)
		current.Digest = agentapproval.Digest(current)
	}
	card, err := agentapproval.BuildPersonaChatCardWithReReview(current, "invocation-1", "user-1", now, agentapproval.ReReviewInput{
		ViewerID: "user-1", Previous: &previous, Source: source,
		CanSee: func(_ string, field string) bool { return !strings.HasPrefix(field, "pay.") },
	})
	if err != nil {
		t.Fatal(err)
	}
	return card, previous
}

func TestIssue3971_ReReviewSectionShowsFieldByFieldAndFreshReviewLink(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	card, _ := issue3971Cards(t, now, func(a *agentapproval.AgentActionApproval) {
		a.Items[0].MaterialFields = []agentapproval.MaterialField{{Path: "job.level", Before: "L3", After: "L6"}, {Path: "pay.base_salary", Before: "1", After: "987654"}}
	}, nil)
	markup := renderPersonaApprovalTest(t, Model{CurrentUser: "user-1"}, card, now.Add(time.Minute))
	for _, want := range []string{
		"What changed since I last reviewed?", "job.level", "L4", "L6", "Previously reviewed", "Open fresh review",
		"data-agent-rereview-state=\"changed\"", "no longer applies", "1 more change", "Nothing is approved, run or sent from here",
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("section missing %q: %s", want, markup)
		}
	}
	for _, leak := range []string{"pay.base_salary", "987654", "agent-approval-action", "Approve"} {
		if strings.Contains(markup, leak) {
			t.Fatalf("section leaked or offered %q: %s", leak, markup)
		}
	}
	if strings.Count(markup, "persona-approval-task-link") != 1 || !strings.Contains(markup, `href="/workspace/app/chat/agents?task=task-1"`) {
		t.Fatalf("exactly one governed task-view link expected: %s", markup)
	}
}

func TestIssue3971_ReReviewIncompleteKeepsSafeRoute_Security(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	card, _ := issue3971Cards(t, now, func(a *agentapproval.AgentActionApproval) { a.Items[0].Uncertainty = "secret reason" }, nil)
	card.ReReview = &agentapproval.ReReview{State: agentapproval.ReReviewIncomplete, Unknown: []agentapproval.ReReviewUnknown{agentapproval.ReReviewUnknownPrevious}}
	markup := renderPersonaApprovalTest(t, Model{CurrentUser: "user-1"}, card, now.Add(time.Minute))
	for _, want := range []string{"data-agent-rereview-state=\"incomplete\"", "incomplete", "not available", "Open in task view"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("incomplete section missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "Open fresh review") || strings.Contains(markup, "Previously reviewed") {
		t.Fatalf("incomplete comparison must not speculate: %s", markup)
	}
	// A comparison for a different digest than the card is stale and renders as incomplete.
	stale, _ := issue3971Cards(t, now, func(a *agentapproval.AgentActionApproval) { a.Items[0].Uncertainty = "other" }, nil)
	stale.ReReview.CurrentDigest = "sha256:old"
	got := renderPersonaApprovalTest(t, Model{CurrentUser: "user-1"}, stale, now.Add(time.Minute))
	if !strings.Contains(got, "data-agent-rereview-state=\"incomplete\"") || strings.Contains(got, "Previously reviewed") {
		t.Fatalf("stale comparison rendered details: %s", got)
	}
	// Another member never sees the card or the comparison.
	if hidden := renderPersonaApprovalTest(t, Model{CurrentUser: "colleague"}, card, now); hidden != "" {
		t.Fatalf("colleague saw comparison: %s", hidden)
	}
}

func TestIssue3971_ReReviewSourceMessageStates(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	edit := func(a *agentapproval.AgentActionApproval) { a.Items[0].Uncertainty = "changed" }
	edited, _ := issue3971Cards(t, now, edit, &agentapproval.SourceMessageInput{CapturedRevision: 1, CurrentRevision: 2, Accessible: true, CapturedText: "Promote Sam to L4", CurrentText: "Promote Sam to L6"})
	markup := renderPersonaApprovalTest(t, Model{CurrentUser: "user-1"}, edited, now.Add(time.Minute))
	for _, want := range []string{"Promote Sam to L4", "Promote Sam to L6", "was edited", "still matches what you meant"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("edited source missing %q: %s", want, markup)
		}
	}
	deleted, _ := issue3971Cards(t, now, edit, &agentapproval.SourceMessageInput{CapturedRevision: 1, CurrentRevision: 2, Accessible: true, Deleted: true, CapturedText: "retained secret", CurrentText: "retained secret"})
	got := renderPersonaApprovalTest(t, Model{CurrentUser: "user-1"}, deleted, now.Add(time.Minute))
	if !strings.Contains(got, "was deleted") || strings.Contains(got, "retained secret") {
		t.Fatalf("deleted source: %s", got)
	}
	lost, _ := issue3971Cards(t, now, edit, &agentapproval.SourceMessageInput{CapturedRevision: 1, CurrentRevision: 2, Accessible: false, CapturedText: "private words", CurrentText: "private words"})
	got = renderPersonaApprovalTest(t, Model{CurrentUser: "user-1"}, lost, now.Add(time.Minute))
	if !strings.Contains(got, "can no longer open") || strings.Contains(got, "private words") {
		t.Fatalf("lost access source: %s", got)
	}
}

func TestIssue3971_ReReviewUnchangedRendersNothing(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	card, _ := issue3971Cards(t, now, nil, nil)
	markup := renderPersonaApprovalTest(t, Model{CurrentUser: "user-1"}, card, now.Add(time.Minute))
	if strings.Contains(markup, "persona-rereview") || strings.Contains(markup, "Open fresh review") || !strings.Contains(markup, "Open in task view") {
		t.Fatalf("unchanged revision must keep the plain card: %s", markup)
	}
}

func TestIssue3971_ReReviewCopyInGermanAndArabic(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	card, _ := issue3971Cards(t, now, func(a *agentapproval.AgentActionApproval) { a.Items[0].Uncertainty = "changed" },
		&agentapproval.SourceMessageInput{CapturedRevision: 1, CurrentRevision: 2, Accessible: true, CapturedText: "a", CurrentText: "b"})
	for locale, want := range map[string][]string{"de-DE": {"Was hat sich seit", "Neue Prüfung öffnen"}, "ar": {"ما الذي تغيّر", "فتح مراجعة جديدة"}} {
		markup := renderPersonaApprovalTest(t, Model{CurrentUser: "user-1", Locale: locale}, card, now.Add(time.Minute))
		for _, text := range want {
			if !strings.Contains(markup, text) {
				t.Fatalf("%s missing %q: %s", locale, text, markup)
			}
		}
		for _, english := range []string{"What changed since", "Open fresh review", "Previously reviewed", "was edited"} {
			if strings.Contains(markup, english) {
				t.Fatalf("%s leaked English %q: %s", locale, english, markup)
			}
		}
	}
	for key, row := range issue3971Copy {
		for i, text := range row {
			if strings.TrimSpace(text) == "" {
				t.Fatalf("copy %s missing language %d", key, i)
			}
		}
	}
}
