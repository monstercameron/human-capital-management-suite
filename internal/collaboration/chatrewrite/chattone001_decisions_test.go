package chatrewrite

import (
	"strings"
	"testing"
)

// TestTodo_CHATTONE_001_Record holds the product to the CHATTONE-001 decision
// record: ten answers, each with its rejected alternative, and the code facts
// that enforce the ones this package owns.
func TestTodo_CHATTONE_001_Record(t *testing.T) {
	ds := RewordDecisions()
	if len(ds) != 10 {
		t.Fatal("the reword record answers ten questions", len(ds))
	}
	for i, d := range ds {
		if d.Question != i+1 || len(d.Decision) < 200 {
			t.Fatal("question not answered", d.Question, d.Topic)
		}
		if d.Rejected == "" || d.Why == "" {
			t.Fatal("no rejected alternative recorded", d.Question, d.Topic)
		}
	}
	text := RenderRewordDecisions()
	for _, want := range []string{
		"off unless", "per channel", "tone only", "no\" stays a \"no",
		"never reworded into acceptability", "reported", "never blocked",
		"edit that wording", "send as written", "told a message was reworded",
		"Members may view messages as written", "original is the record",
		"which readers saw which rendering", "agent", "cannot leak",
		"direct messages", "never imposed", "deliver as written",
		"not shown a failed rewrite",
	} {
		if !strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
			t.Fatal("decision missing", want)
		}
	}
	// The record's enforceable points are enforced here.
	if RewordHardFilter == "" || RewordFallback == "" {
		t.Fatal("the hard-filter and fallback outcomes are unnamed")
	}
	if !strings.Contains(PreservationQuestion, "without adding") {
		t.Fatal("the meaning check does not forbid additions")
	}
	if len(DefaultStyles()) != 3 || TaskProfileID == "" {
		t.Fatal("the style controls are not the three registered ones")
	}
}
