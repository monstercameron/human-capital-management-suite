package chatui

import (
	"strings"
	"testing"
)

func TestIssue3792_MarkAsDecisionRowAndDialog(t *testing.T) {
	m, _ := issue3792Model("en-US")
	var gotID, gotText string
	m.Decisions.Mark = func(id, text string) { gotID, gotText = id, text }
	m.Messages = []Message{{ID: "p1", AuthorID: "someone", Body: "We freeze promotion\n changes until after calibration."}, {ID: "p2", AuthorID: "someone", Body: "   "}}
	row := m.Messages[0]
	markup := renderNode(t, spanOf(chatux022MenuItems(m, row)))
	if !strings.Contains(markup, `data-action="decision-mark"`) || !strings.Contains(markup, "Mark as decision") {
		t.Fatalf("menu lacks the row: %s", markup)
	}
	if len(decisionsMenuItem(m, m.Messages[1])) != 0 {
		t.Error("an empty message can be marked")
	}
	m.decisionsMark("p1")
	if gotID != "p1" || gotText != "We freeze promotion changes until after calibration." {
		t.Errorf("mark gave %q %q", gotID, gotText)
	}
	gotID = ""
	m.decisionsMark("missing")
	if gotID != "" {
		t.Error("an unknown message was marked")
	}
	off := m
	off.Decisions.Mark = nil
	if len(decisionsMenuItem(off, row)) != 0 {
		t.Error("a Mark row with nothing behind it")
	}
	off = m
	off.Decisions.Enabled = false
	if len(decisionsMenuItem(off, row)) != 0 {
		t.Error("a Mark row while the server cannot record")
	}
	long := DecisionsMarkText(strings.Repeat("é", 700))
	if n := len([]rune(long)); n > 500 {
		t.Errorf("marked text is %d characters", n)
	}
	v := issue3792View("de-DE")
	v.FromMessage = true
	if out := renderNode(t, RenderDecisionsDialog(v)); !strings.Contains(out, "verweist auf die gewählte Nachricht") {
		t.Errorf("no German note: %s", out)
	}
	v.CanRecord, v.Locale = false, "ar"
	if out := renderNode(t, RenderDecisionsDialog(v)); strings.Contains(out, "اخترتها") {
		t.Error("the note shows to someone who cannot record")
	}
}
