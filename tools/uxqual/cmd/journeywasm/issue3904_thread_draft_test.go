package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestIssue3904_ThreadDraftSurvivesConversationSwitch: an unsent thread reply
// (and its "Also send to" choice) is still there after another channel is
// opened and the author comes back to the same thread.
func TestIssue3904_ThreadDraftSurvivesConversationSwitch(t *testing.T) {
	s := &chatState{drafts: map[string]string{}}
	s.mutate(func(m *chatui.Model) { m.SelectedID, m.ShowThread, m.ThreadParentID = "people-ops", true, "root" })
	s.setThreadDraft("root", "QA thread navigation recovery")
	s.setThreadAlso("root", true)

	s.selectChatConversation("benefits")
	away := s.snapshot()
	if away.ShowThread || away.ThreadParentID != "" {
		t.Fatalf("the thread pane followed the switch: %v %q", away.ShowThread, away.ThreadParentID)
	}
	s.selectChatConversation("people-ops")
	s.mutate(func(m *chatui.Model) { m.ShowThread, m.ThreadParentID = true, "root" })
	back := s.snapshot()
	if back.ThreadDrafts["root"] != "QA thread navigation recovery" {
		t.Fatalf("the reply did not survive: %v", back.ThreadDrafts)
	}
	if !back.ThreadAlso["root"] {
		t.Fatalf("the reply's channel choice did not survive: %v", back.ThreadAlso)
	}
	if _, elsewhere := away.ThreadDrafts["other"]; elsewhere {
		t.Fatal("an unrelated thread gained a draft")
	}
}

// TestIssue3901_SetThreadAlsoIsPerThread: the choice is kept per root message and
// cleared when unticked.
func TestIssue3901_SetThreadAlsoIsPerThread(t *testing.T) {
	s := &chatState{}
	s.setThreadAlso("a", true)
	s.setThreadAlso("b", true)
	s.setThreadAlso("a", false)
	got := s.snapshot().ThreadAlso
	if len(got) != 1 || !got["b"] {
		t.Fatalf("choices = %v", got)
	}
	s.setThreadAlso("b", false)
	if s.snapshot().ThreadAlso != nil {
		t.Fatal("an empty set of choices is not nil")
	}
	s.setThreadAlso("", true)
	if s.snapshot().ThreadAlso != nil {
		t.Fatal("a choice with no thread was kept")
	}
}
