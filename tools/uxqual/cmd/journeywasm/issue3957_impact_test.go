package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func issue3957Browser(reads *int, state chatui.LinkedWorkState, err error) (*impactBrowser, *[]string) {
	focused := &[]string{}
	return &impactBrowser{
		refresh: func() {}, focus: func(s string) { *focused = append(*focused, s) },
		spawn: func(fn func()) { fn() }, post: func(fn func()) { fn() },
		read: func(context.Context, string) (chatui.LinkedWorkState, error) { *reads++; return state, err },
	}, focused
}

func issue3957Thread(conversation, thread string) chatui.Model {
	return chatui.Model{SelectedID: conversation, ThreadParentID: thread, Locale: "en-US"}
}

func issue3957Work() chatui.LinkedWorkState {
	return chatui.LinkedWorkState{State: "ready", AsOf: time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC), Items: []chatui.LinkedWorkItem{
		{Kind: "task", ID: "t1", Title: "Send welcome kits", State: "open", DueDate: "2026-12-01"},
		{Kind: "task", ID: "t2", Title: "Book the hall", State: "open", DueDate: "2026-12-02"},
		{Kind: "task", ID: "t3", Title: "Far away", State: "open", DueDate: "2027-05-01"},
	}}
}

func TestIssue3957_CheckIsOfferedOnlyInAnOpenThreadThatCanBeRead(t *testing.T) {
	reads := 0
	b, _ := issue3957Browser(&reads, issue3957Work(), nil)
	if m := b.project(issue3957Thread("c1", "")); m.Impact.Enabled {
		t.Error("offered with no thread open, where there is no reply box")
	}
	if m := b.project(issue3957Thread("c1", "root")); !m.Impact.Enabled || m.Impact.Open == nil || m.Impact.Draft == nil {
		t.Errorf("not offered in a thread: %+v", m.Impact)
	}
	b.read = nil
	if m := b.project(issue3957Thread("c1", "root")); m.Impact.Enabled {
		t.Error("offered where the work cannot be read")
	}
	if reads != 0 {
		t.Error("work was read before anyone asked")
	}
}

func TestIssue3957_RunReadsAsThePersonAndShowsFindingsThenDraftRechecks_Integration(t *testing.T) {
	reads := 0
	b, focused := issue3957Browser(&reads, issue3957Work(), nil)
	m := b.project(issue3957Thread("c1", "root"))
	m.Impact.Open("p1", "2026-12-01")
	m = b.project(issue3957Thread("c1", "root"))
	if m.Impact.PostID != "p1" || m.Impact.Date != "2026-12-01" || m.Impact.Stage != "" {
		t.Fatalf("after open = %+v", m.Impact)
	}
	m.Impact.Run("p1", "2026-12-01", "new hires")
	m = b.project(issue3957Thread("c1", "root"))
	if reads != 1 || m.Impact.Stage != "ready" || len(m.Impact.Result.Findings) != 2 || m.Impact.Population != "new hires" {
		t.Fatalf("after run reads=%d state=%+v", reads, m.Impact)
	}
	if len(*focused) == 0 || !strings.Contains((*focused)[len(*focused)-1], "impact-run") {
		t.Errorf("focus did not come back to the check button: %v", *focused)
	}
	m.Impact.Dismiss("t1")
	m = b.project(issue3957Thread("c1", "root"))
	if !m.Impact.Dismissed["t1"] || chatui.ImpactKept(m.Impact.Result, m.Impact.Dismissed) != 1 {
		t.Fatalf("dismiss = %+v", m.Impact)
	}
	// Drafting reads the work again, and says nothing about any task.
	var draft string
	m.Impact.Draft("p1", "2026-12-01", "new hires", func(text string) { draft = text })
	if reads != 2 {
		t.Errorf("the draft did not recheck the work: %d reads", reads)
	}
	if !strings.Contains(draft, "2026-12-01") || !strings.Contains(draft, "new hires") || strings.Contains(draft, "Book the hall") || strings.Contains(draft, "welcome kits") {
		t.Errorf("draft = %q", draft)
	}
	if m = b.project(issue3957Thread("c1", "root")); m.Impact.Notice != "draft_ready" {
		t.Errorf("notice = %q", m.Impact.Notice)
	}
	// With every finding dismissed there is nothing to ask and nothing is delivered.
	m.Impact.Dismiss("t2")
	m = b.project(issue3957Thread("c1", "root"))
	draft = ""
	m.Impact.Draft("p1", "2026-12-01", "", func(text string) { draft = text })
	if m = b.project(issue3957Thread("c1", "root")); draft != "" || m.Impact.Notice != "nothing_to_ask" {
		t.Errorf("draft=%q notice=%q", draft, m.Impact.Notice)
	}
}

func TestIssue3957_FailedReadKeepsNothingAndMakesNoDraft(t *testing.T) {
	reads := 0
	b, _ := issue3957Browser(&reads, chatui.LinkedWorkState{}, chatworkError("unavailable"))
	m := b.project(issue3957Thread("c1", "root"))
	m.Impact.Open("p1", "")
	m = b.project(issue3957Thread("c1", "root"))
	delivered := false
	m.Impact.Draft("p1", "2026-12-01", "", func(string) { delivered = true })
	m = b.project(issue3957Thread("c1", "root"))
	if delivered || m.Impact.Stage != "error" || m.Impact.Notice != "draft_failed" || len(m.Impact.Result.Findings) != 0 {
		t.Errorf("failed read: delivered=%v state=%+v", delivered, m.Impact)
	}
}

func TestIssue3957_InvalidDateReadsNothing(t *testing.T) {
	reads := 0
	b, _ := issue3957Browser(&reads, issue3957Work(), nil)
	m := b.project(issue3957Thread("c1", "root"))
	m.Impact.Open("p1", "")
	m = b.project(issue3957Thread("c1", "root"))
	m.Impact.Run("p1", "", "")
	m.Impact.Draft("p1", "soon", "", func(string) { t.Error("a draft was made without a date") })
	if m = b.project(issue3957Thread("c1", "root")); reads != 0 || m.Impact.Error != "invalid_date" {
		t.Errorf("reads=%d state=%+v", reads, m.Impact)
	}
}

func TestIssue3957_CheckIsPrivateToTheThreadAndNeverRestored_Security(t *testing.T) {
	reads := 0
	b, _ := issue3957Browser(&reads, issue3957Work(), nil)
	m := b.project(issue3957Thread("c1", "root"))
	m.Impact.Open("p1", "2026-12-01")
	m = b.project(issue3957Thread("c1", "root"))
	m.Impact.Run("p1", "2026-12-01", "")
	for name, other := range map[string]chatui.Model{"another thread": issue3957Thread("c1", "root2"), "another conversation": issue3957Thread("c2", "root")} {
		if got := b.project(other); got.Impact.PostID != "" || got.Impact.Stage != "" || len(got.Impact.Result.Findings) != 0 {
			t.Errorf("%s: the check leaked: %+v", name, got.Impact)
		}
		if back := b.project(issue3957Thread("c1", "root")); back.Impact.PostID != "" || back.Impact.Stage != "" {
			t.Errorf("%s: the check came back after leaving: %+v", name, back.Impact)
		}
		m = b.project(issue3957Thread("c1", "root"))
		m.Impact.Open("p1", "")
		m = b.project(issue3957Thread("c1", "root"))
		m.Impact.Run("p1", "2026-12-01", "")
	}
	// A read that finishes after the person left the thread is dropped.
	b2, _ := issue3957Browser(&reads, issue3957Work(), nil)
	var later func()
	b2.post = func(fn func()) { later = fn }
	m = b2.project(issue3957Thread("c1", "root"))
	m.Impact.Open("p1", "")
	m = b2.project(issue3957Thread("c1", "root"))
	m.Impact.Run("p1", "2026-12-01", "")
	b2.project(issue3957Thread("c1", "other"))
	later()
	if got := b2.project(issue3957Thread("c1", "root")); got.Impact.Stage != "" || got.Impact.PostID != "" {
		t.Errorf("a late read was applied: %+v", got.Impact)
	}
	// Close forgets everything.
	m = b.project(issue3957Thread("c1", "root"))
	m.Impact.Close()
	if closed := b.project(issue3957Thread("c1", "root")); closed.Impact.PostID != "" || closed.Impact.Stage != "" {
		t.Errorf("close: %+v", closed.Impact)
	}
}
