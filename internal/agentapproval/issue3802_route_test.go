package agentapproval

import (
	"strings"
	"testing"
	"time"
)

// Issue 3802: the card's review link is the Agents page that is served, with the
// task named by its exact id; the old /workspace/app/agents address is not a page.
func TestIssue3802_CardLinkOpensTheServedAgentsPage(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	service, err := New(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	approval := b2Create(t, service, b2Item("item-1", TierSubmitGoverned, "low"))
	card, err := BuildPersonaChatCard(approval, "invocation-1", approval.AssignedUserID, now)
	if err != nil {
		t.Fatal(err)
	}
	want := "/workspace/app/chat/agents?task=" + approval.TaskID
	if card.TaskViewHref != want || card.Card.Values["task_view"] != want {
		t.Fatalf("task view href = %q / %q, want %q", card.TaskViewHref, card.Card.Values["task_view"], want)
	}
	if strings.HasPrefix(card.TaskViewHref, "/workspace/app/agents") {
		t.Fatalf("card still links to an address that is not a page: %q", card.TaskViewHref)
	}
}
