package main

import (
	"testing"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func issue3973Candidates() []chatui.HandoverCandidate {
	return []chatui.HandoverCandidate{
		{Key: "p1|t1", ProjectID: "p1", TaskID: "t1", Title: "Ship", Readable: true},
		{Key: "p2|t2", ProjectID: "p2", TaskID: "t2", Readable: false},
	}
}

var issue3973People = []chatui.Member{{ID: "kim", Name: "Kim"}}

func TestIssue3973_RecipientNeedsAnActiveMembership_Security(t *testing.T) {
	members := []*projectv1.ProjectMember{{UserId: "kim", State: "INVITED"}, {UserId: "ana", State: "ACTIVE"}, {UserId: "bob", State: "REVOKED"}}
	for recipient, want := range map[string]string{"kim": "no", "ana": "yes", "bob": "no", "zed": "no", "": "unknown"} {
		if got := handoverRecipientAccess(members, recipient); got != want {
			t.Errorf("access of %q = %q, want %q", recipient, got, want)
		}
	}
}

func TestIssue3973_SelectionIsLimitedToTasksTheCardsShow_Security(t *testing.T) {
	view, _ := handoverApply(chatui.HandoverView{}, "ops", chatui.HandoverCommand{Kind: "open"}, issue3973Candidates(), issue3973People)
	for _, key := range []string{"p2|t2", "p9|t9", "forged"} {
		view, _ = handoverApply(view, "ops", chatui.HandoverCommand{Kind: "toggle", Key: key}, issue3973Candidates(), issue3973People)
	}
	if len(view.Selected) != 0 {
		t.Fatalf("a task the person cannot open, or a forged id, was selected: %#v", view.Selected)
	}
	view, _ = handoverApply(view, "ops", chatui.HandoverCommand{Kind: "toggle", Key: "p1|t1"}, issue3973Candidates(), issue3973People)
	if !view.Selected["p1|t1"] {
		t.Fatalf("a readable task was not selected: %#v", view.Selected)
	}
	view, _ = handoverApply(view, "ops", chatui.HandoverCommand{Kind: "toggle", Key: "p1|t1"}, issue3973Candidates(), issue3973People)
	if view.Selected["p1|t1"] {
		t.Fatal("a second press did not untick")
	}
}

func TestIssue3973_PrepareRefusesWhatIsMissingBeforeReadingAnything(t *testing.T) {
	cands := issue3973Candidates()
	view, _ := handoverApply(chatui.HandoverView{}, "ops", chatui.HandoverCommand{Kind: "open"}, cands, issue3973People)
	if _, run := handoverApply(view, "ops", chatui.HandoverCommand{Kind: "prepare", Recipient: "kim"}, cands, issue3973People); run {
		t.Fatal("prepared with nothing selected")
	}
	view, _ = handoverApply(view, "ops", chatui.HandoverCommand{Kind: "toggle", Key: "p1|t1", Recipient: "kim"}, cands, issue3973People)
	for name, tc := range map[string]struct {
		cmd  chatui.HandoverCommand
		want string
	}{
		"none":     {chatui.HandoverCommand{Kind: "prepare", Recipient: ""}, "recipient"},
		"stranger": {chatui.HandoverCommand{Kind: "prepare", Recipient: "eve"}, "recipient"},
		"window":   {chatui.HandoverCommand{Kind: "prepare", Recipient: "kim", From: "2026-12-09", To: "2026-12-01"}, "window"},
	} {
		next, run := handoverApply(view, "ops", tc.cmd, cands, issue3973People)
		if run || next.Status != tc.want || next.Stage != "pick" {
			t.Fatalf("%s: run=%v status=%q stage=%q", name, run, next.Status, next.Stage)
		}
	}
	next, run := handoverApply(view, "ops", chatui.HandoverCommand{Kind: "prepare", Recipient: "kim", From: "2026-12-01", To: "2026-12-09"}, cands, issue3973People)
	if !run || next.Stage != "preparing" || next.RecipientName != "Kim" || next.From != "2026-12-01" {
		t.Fatalf("a complete request did not start: run=%v %#v", run, next)
	}
}

func TestIssue3973_StateBelongsToOneConversationAndKeepsEditsOnClose(t *testing.T) {
	cands := issue3973Candidates()
	view := chatui.HandoverView{ConversationID: "ops", Open: true, Stage: "draft", Text: "old", Selected: map[string]bool{"p1|t1": true}}
	view, _ = handoverApply(view, "ops", chatui.HandoverCommand{Kind: "close", Text: "edited"}, cands, issue3973People)
	if view.Open || view.Text != "edited" {
		t.Fatalf("closing lost the person's edits: %#v", view)
	}
	other, _ := handoverApply(view, "design", chatui.HandoverCommand{Kind: "open"}, cands, issue3973People)
	if other.ConversationID != "design" || len(other.Selected) != 0 || other.Text != "" {
		t.Fatalf("another conversation inherited the handover: %#v", other)
	}
}
