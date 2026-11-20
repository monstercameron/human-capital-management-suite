package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestIssue3859_ReversedRangeIsRecognisedFromTheControls: the dates the form
// would apply are judged together with the ones already in the query, and the
// controls' own query is refused so the page can name the fields.
func TestIssue3859_ReversedRangeIsRecognisedFromTheControls(t *testing.T) {
	query := `Portland in:general kind:message lang:en after:2026-11-01 before:2026-12-01`
	values := map[string]string{"before": "2026-11-01", "after": "2026-11-10"}
	if _, err := chatsearchControlQuery(query, values, nil); err == nil {
		t.Fatal("a reversed range was accepted by the controls")
	}
	before, after, reversed := chatsearchReversedRange(query, values)
	if !reversed || before != "2026-11-01" || after != "2026-11-10" {
		t.Fatalf("reversed=%v before=%q after=%q", reversed, before, after)
	}
	// Only After edited: Before comes from the query.
	if _, _, reversed := chatsearchReversedRange(query, map[string]string{"after": "2026-12-05"}); !reversed {
		t.Fatal("an After date past the query's Before date was not recognised")
	}
	// The valid range is not flagged, and an unrelated refusal is not a range.
	if _, _, reversed := chatsearchReversedRange(query, map[string]string{"before": "2026-12-01", "after": "2026-11-01"}); reversed {
		t.Fatal("the valid range was flagged")
	}
	if _, _, reversed := chatsearchReversedRange("Portland", map[string]string{"after": "2026-11-01"}); reversed {
		t.Fatal("a single date was flagged")
	}
}

// TestIssue3859_SameFiltersAfterARefusalSendAgain: after the controls are
// refused, applying the same filters that were applied before must search
// again; it used to be taken for a repeat of the answered search, so the error
// stayed and the results never came back.
func TestIssue3859_SameFiltersAfterARefusalSendAgain(t *testing.T) {
	var f chatsearchFlow
	query := `Portland after:2026-11-01 before:2026-12-01`
	generation, send := f.begin(query, query, true, false)
	if !send {
		t.Fatal("first search not sent")
	}
	f.complete(generation, false)
	if _, send := f.begin(query, query, true, false); send {
		t.Fatal("a repeat of an answered search was sent without a refusal in between")
	}
	f.reject()
	if _, send := f.begin(query, query, true, false); !send {
		t.Fatal("the same filters after a refusal were not sent again, so the error would stay")
	}
}

// TestIssue3691_RemovingANameChipTakesTheWholeName: the chip of from:@Julian
// Miller removes both words, not just the first.
func TestIssue3691_RemovingANameChipTakesTheWholeName(t *testing.T) {
	model := chatui.Model{SearchDirectory: []chatui.SearchPerson{{ID: "hc-026-julian-miller", Name: "Julian Miller"}}}
	got, err := chatsearchRemoveChip(model, `from:@Julian Miller payroll`, "from:hc-026-julian-miller")
	if err != nil || got != "payroll" {
		t.Fatalf("removed to %q (%v), want payroll", got, err)
	}
	got, err = chatsearchRemoveChip(model, `payroll in:general`, "in:general")
	if err != nil || got != "payroll" {
		t.Fatalf("typed chip removed to %q (%v)", got, err)
	}
}
