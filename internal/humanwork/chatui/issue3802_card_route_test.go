package chatui

import (
	"strings"
	"testing"
	"time"
)

// Issue 3802: the approval card links to the served Agents page, keeps the same
// strict same-origin check, and never offers the old address.
func TestIssue3802_ApprovalCardLinksToServedAgentsPage(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	card := personaApprovalCardFixture(t, now)
	markup := renderPersonaApprovalTest(t, Model{CurrentUser: "user-1"}, card, now.Add(time.Minute))
	if !strings.Contains(markup, `href="/workspace/app/chat/agents?task=task-1"`) || strings.Contains(markup, `href="/workspace/app/agents`) {
		t.Fatalf("card link is not the served Agents page: %s", markup)
	}
	for _, href := range []string{
		"/workspace/app/agents?task=task-1",
		"/workspace/app/chat/agents",
		"/workspace/app/chat/agents?task=",
		"/workspace/app/chat/agents?task=a&other=b",
		"//evil.example/workspace/app/chat/agents?task=a",
		"https://evil.example/workspace/app/chat/agents?task=a",
		"/workspace/app/chat/agents/../admin?task=a",
	} {
		if _, ok := safePersonaApprovalTaskHref(href, href); ok {
			t.Fatalf("accepted unsafe or stale link %q", href)
		}
	}
	if _, ok := safePersonaApprovalTaskHref("/workspace/app/chat/agents?task=task-1", "/workspace/app/chat/agents?task=task-2"); ok {
		t.Fatal("accepted a link that differs from the encoded card value")
	}
}

// Stale and other-viewer cases still show no link and no card.
func TestIssue3802_ApprovalCardStaleAndOtherViewer_Security(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	card := personaApprovalCardFixture(t, now)
	if markup := renderPersonaApprovalTest(t, Model{CurrentUser: "someone-else"}, card, now.Add(time.Minute)); strings.Contains(markup, "chat/agents") {
		t.Fatalf("another viewer saw the link: %s", markup)
	}
	stale := renderPersonaApprovalTest(t, Model{CurrentUser: "user-1"}, card, card.ExpiresAt.Add(time.Second))
	if strings.Contains(stale, "persona-approval-task-link") || !strings.Contains(stale, "no longer available") {
		t.Fatalf("expired card still links: %s", stale)
	}
}
