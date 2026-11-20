package chatui

import (
	"strings"
	"testing"
)

func issue3996Model(locale, body string, attachments []Attachment) Model {
	m := issue3988Model(locale)
	m.FocusMessageID = ""
	root := Message{ID: "root", AuthorID: "alice", Author: "Priyanka", Body: body, TimeLabel: "9:30", Replies: 2, Attachments: attachments}
	m.Messages = []Message{root}
	m.ThreadParent = &root
	return m
}

const issue3996Long = "First paragraph of a long clarification.\n\nSecond paragraph explains the rollout order.\n\nThird paragraph lists the open risks.\n\nFourth paragraph asks for a decision."

func TestIssue3996_TallRootOpensCollapsedWithExpandControl(t *testing.T) {
	for locale, want := range map[string]string{"en-US": "Show full message", "de-DE": "Ganze Nachricht anzeigen", "ar": "عرض الرسالة كاملة"} {
		got := renderNode(t, threadPane(issue3996Model(locale, issue3996Long, nil), handlers{}))
		for _, need := range []string{`thread-root-content collapsed`, `id="thread-root-toggle"`, `aria-expanded="false"`, `aria-controls="thread-root-content"`, want} {
			if !strings.Contains(got, need) {
				t.Fatalf("%s: tall root lacks %q: %s", locale, need, got)
			}
		}
		// The whole root stays in the page, and the replies follow it.
		if !strings.Contains(got, "Fourth paragraph asks for a decision.") || !strings.Contains(got, "Regions can go earlier") {
			t.Fatalf("%s: root text or replies missing", locale)
		}
	}
}

func TestIssue3996_ExpandedRootShowsShowLess(t *testing.T) {
	h := handlers{}
	h.local.setDetailGroup("thread-root:root", true)
	got := renderNode(t, threadPane(issue3996Model("en-US", issue3996Long, nil), h))
	if strings.Contains(got, "thread-root-content collapsed") || !strings.Contains(got, `aria-expanded="true"`) || !strings.Contains(got, "Show less") {
		t.Fatalf("expanded root not shown expanded: %s", got)
	}
}

func TestIssue3996_ShortRootIsUnchanged(t *testing.T) {
	got := renderNode(t, threadPane(issue3996Model("en-US", "Launch plan", nil), handlers{}))
	if strings.Contains(got, "thread-root-toggle") || strings.Contains(got, "thread-root-content") {
		t.Fatalf("short root got a collapse control: %s", got)
	}
}

func TestIssue3996_AttachmentsAreNamedWhileCollapsed(t *testing.T) {
	m := issue3996Model("de-DE", "See picture", []Attachment{{ID: "a1", Name: "plan.png", ContentType: "image/png"}})
	got := renderNode(t, threadPane(m, handlers{}))
	if !strings.Contains(got, "thread-root-note") || !strings.Contains(got, "enthält Anhänge") {
		t.Fatalf("collapsed root with an image does not say it has attachments: %s", got)
	}
}

func TestIssue3996_StylesClipAndKeepTouchTarget(t *testing.T) {
	for _, need := range []string{".thread-root-content.collapsed{max-height", "overflow:hidden", "min-height:44px"} {
		if !strings.Contains(Stylesheet, need) {
			t.Fatalf("stylesheet lacks %q", need)
		}
	}
}
