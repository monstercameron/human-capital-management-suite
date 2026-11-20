package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func issue3960Page(conv string, pins ...string) chatui.Model {
	m := chatui.Model{SelectedID: conv}
	for _, id := range pins {
		m.ChannelPins = append(m.ChannelPins, chatui.ChannelPin{PostID: id, Sequence: 1})
	}
	return m
}

func TestIssue3960_PinReviewStateIsPrivateAndPerConversation(t *testing.T) {
	refreshes := 0
	s := &pinReviewBrowser{refresh: func() { refreshes++ }}
	m := s.project(issue3960Page("c1", "p1", "p2"))
	if m.PinReview.Open == nil || m.PinReview.PinID != "" {
		t.Fatalf("review not offered closed: %+v", m.PinReview)
	}
	m.PinReview.Open("p1")
	m = s.project(issue3960Page("c1", "p1", "p2"))
	if m.PinReview.PinID != "p1" {
		t.Fatalf("open: %+v", m.PinReview)
	}
	m.PinReview.Toggle("m5")
	m.PinReview.Toggle("m6")
	m.PinReview.Toggle("m6")
	m = s.project(issue3960Page("c1", "p1", "p2"))
	if !m.PinReview.Selected["m5"] || m.PinReview.Selected["m6"] {
		t.Fatalf("toggle: %+v", m.PinReview.Selected)
	}
	// Opening another pin starts with no sources chosen.
	m.PinReview.Open("p2")
	m = s.project(issue3960Page("c1", "p1", "p2"))
	if m.PinReview.PinID != "p2" || len(m.PinReview.Selected) != 0 {
		t.Fatalf("second pin: %+v", m.PinReview)
	}
	// Keep marks the pin kept and closes the review.
	m.PinReview.Keep("p2")
	m = s.project(issue3960Page("c1", "p1", "p2"))
	if m.PinReview.PinID != "" || !m.PinReview.Kept["p2"] || m.PinReview.Kept["p1"] {
		t.Fatalf("keep: %+v", m.PinReview)
	}
	// Another conversation sees none of it.
	other := s.project(issue3960Page("c2", "p2"))
	if other.PinReview.Kept["p2"] {
		t.Fatalf("kept leaked into another conversation")
	}
	if refreshes == 0 {
		t.Fatalf("the page was never asked to redraw")
	}
}

func TestIssue3960_PinReviewClosesWhenItsPinIsGoneOrConversationChanges(t *testing.T) {
	s := &pinReviewBrowser{refresh: func() {}}
	m := s.project(issue3960Page("c1", "p1"))
	m.PinReview.Open("p1")
	m.PinReview.Toggle("m5")
	// The pin was unpinned: the review closes and forgets its sources.
	if m = s.project(issue3960Page("c1")); m.PinReview.PinID != "" || len(m.PinReview.Selected) != 0 {
		t.Fatalf("review stayed open for a pin that is gone: %+v", m.PinReview)
	}
	m = s.project(issue3960Page("c1", "p1"))
	m.PinReview.Open("p1")
	if m = s.project(issue3960Page("c2", "p1")); m.PinReview.PinID != "" {
		t.Fatalf("review followed the reader into another conversation")
	}
	// Nothing is offered without a conversation.
	if m = s.project(issue3960Page("", "p1")); m.PinReview.Open != nil {
		t.Fatalf("offered without a conversation")
	}
}

func TestIssue3960_PinReviewMovesFocusForKeyboardReaders(t *testing.T) {
	var focused []string
	s := &pinReviewBrowser{refresh: func() {}, focus: func(selector string) { focused = append(focused, selector) }}
	m := s.project(issue3960Page("c1", "p1"))
	m.PinReview.Open("p1")
	m.PinReview.Toggle("m5")
	m.PinReview.Close()
	m.PinReview.Open("p1")
	m.PinReview.Keep("p1")
	want := []string{
		`[id="pin-review-p1"]`,
		`[data-action="pinreview-toggle"][data-id="m5"]`,
		`[data-action="pinreview-open"][data-id="p1"]`,
		`[id="pin-review-p1"]`,
		`[data-action="pinreview-open"][data-id="p1"]`,
	}
	if len(focused) != len(want) {
		t.Fatalf("focus moves %q, want %q", focused, want)
	}
	for i := range want {
		if focused[i] != want[i] {
			t.Errorf("focus move %d = %q, want %q", i, focused[i], want[i])
		}
	}
	if got := pinReviewSelector("pinreview-open", `a"]x`); got != `[data-action="pinreview-open"][data-id="a\"]x"]` {
		t.Errorf("an id widened the selector: %s", got)
	}
}
