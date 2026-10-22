package chat

import (
	"os"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

func TestTodo_CHATMOD_001(t *testing.T) {
	ds := ModerationDecisions()
	if len(ds) != 7 {
		t.Fatal("the moderation record answers seven questions", len(ds))
	}
	for i, d := range ds {
		if d.Question != i+1 || len(d.Decision) < 200 {
			t.Fatal("question not answered", d.Question, d.Topic)
		}
		if d.Rejected == "" || d.Why == "" {
			t.Fatal("no rejected alternative recorded", d.Question, d.Topic)
		}
	}
	text := RenderModerationDecisions()
	for _, want := range []string{
		"not sent", "masked", "flagged for review", "silently dropped", "silently altered",
		"same retention", "author", "recorded reason",
		"thread replies", "display names", "voice transcripts", "agent output",
		"tenant's hard rules", "Removed by an administrator", "author is told",
		"Remove messages", "Review removed messages", "Manage filters", "Report",
		"Change channel status", "Post announcements", "appeal", "once",
		"never the text",
	} {
		if !strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
			t.Fatal("decision missing", want)
		}
	}
	// The recorded permissions are the ones the product enforces.
	for _, p := range []string{PermissionRemoveMessages, PermissionReviewRemovedMessages, PermissionManageFilters, PermissionReport} {
		if !ValidModerationPermission(p) {
			t.Fatal("recorded moderation permission not enforced", p)
		}
	}
	if !DefaultModerationPermission(WorkspaceAdministratorModerationRole, false, PermissionRemoveMessages) {
		t.Fatal("workspace administrators do not hold every moderation permission by default")
	}
	if !DefaultModerationPermission("member", true, PermissionReport) {
		t.Fatal("members cannot report by default")
	}
	if chatpolicy.PermissionChangeChannelStatus != "Change channel status" || chatpolicy.PermissionPostAnnouncements != "Post announcements" {
		t.Fatal("recorded status permissions are not the ones enforced")
	}
	restricted := chatpolicy.StatusPermissions{WorkspaceAdmin: true}
	if !chatpolicy.CanChangeChannelStatus(chatpolicy.StatusOpen, chatpolicy.StatusLocked, restricted) {
		t.Fatal("workspace administrators cannot lock a channel")
	}
}

func TestTodo_CHATMOD_001_Golden(t *testing.T) {
	const golden = "testdata/chatmod001_decisions.golden"
	got := RenderModerationDecisions()
	if os.Getenv("CHATMOD_GOLDEN_UPDATE") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != got {
		t.Fatal("decision record changed; review it and regenerate with CHATMOD_GOLDEN_UPDATE=1")
	}
}
