package chatui

import (
	"strings"
	"testing"
	"time"
)

func issue3960Model() Model {
	now := time.Now()
	m := chatux021Model()
	m.Messages = []Message{
		{ID: "p1", Author: "Jake Sullivan", Body: "Deadline is 2026-03-01. Use https://old.example.com/form", Sequence: 4, SentAt: now.AddDate(0, 0, -30)},
		{ID: "m5", Author: "Loretta Haynes", Body: "Deadline moved to 2026-04-15, form is https://new.example.com/form", Sequence: 5, SentAt: now.AddDate(0, 0, -2)},
		{ID: "m6", Author: "Walt Brennan", Body: "Thanks all, lunch is at noon", Sequence: 6, SentAt: now.AddDate(0, 0, -1)},
		{ID: "m1", Author: "Walt Brennan", Body: "an earlier note", Sequence: 1, SentAt: now.AddDate(0, 0, -40)},
	}
	m.ChannelPins = []ChannelPin{{PostID: "p1", Author: "Jake Sullivan", Body: "Deadline is 2026-03-01. Use https://old.example.com/form", Sequence: 4, PinnedBy: "Walt Brennan", PinnedAt: now.AddDate(0, 0, -30)}}
	m.Callbacks.JumpToPin, m.Callbacks.CopyPinReference, m.Callbacks.Unpin = func(string, uint64) {}, func(string) {}, func(string) {}
	m.PinReview = PinReviewState{Open: func(string) {}, Close: func() {}, Toggle: func(string) {}, Keep: func(string) {}}
	return m
}

func TestIssue3960_ReviewButtonOnlyWhenCapabilityIsOn(t *testing.T) {
	m := issue3960Model()
	got := renderNode(t, chatux019PinnedSection(m))
	for _, want := range []string{`data-action="pinreview-open" data-id="p1"`, "Review pinned guidance", `aria-expanded="false"`} {
		if !strings.Contains(got, want) {
			t.Errorf("review button misses %q: %s", want, got)
		}
	}
	off := m
	off.PinReview = PinReviewState{}
	got = renderNode(t, chatux019PinnedSection(off))
	if strings.Contains(got, "pinreview") || !strings.Contains(got, `data-action="unpin" data-id="p1"`) || !strings.Contains(got, `data-action="pin-jump"`) {
		t.Errorf("ordinary pinned list must stay usable without the review: %s", got)
	}
}

func TestIssue3960_ReviewListsOnlyNewerMessagesAndStatesWhatWasChecked(t *testing.T) {
	m := issue3960Model()
	m.PinReview.PinID = "p1"
	got := renderNode(t, chatux019PinnedSection(m))
	for _, want := range []string{`data-action="pinreview-toggle" data-id="m5"`, `data-action="pinreview-toggle" data-id="m6"`, `aria-expanded="true"`,
		"Only you see this review", "Documents were not checked", "2 later messages are loaded"} {
		if !strings.Contains(got, want) {
			t.Errorf("review misses %q: %s", want, got)
		}
	}
	if strings.Contains(got, `data-id="m1"`) {
		t.Errorf("an older message is offered as later evidence: %s", got)
	}
	if strings.Contains(got, "obsolete") || strings.Contains(got, "outdated") {
		t.Errorf("the review labels a pin: %s", got)
	}
}

func TestIssue3960_SelectedSourceShowsSideBySideWithCitationAndConflict(t *testing.T) {
	m := issue3960Model()
	m.PinReview.PinID = "p1"
	m.PinReview.Selected = map[string]bool{"m5": true, "m6": true}
	got := renderNode(t, chatux019PinnedSection(m))
	for _, want := range []string{"Loretta Haynes", "2026-04-15", "https://new.example.com/form", `data-action="pinreview-inspect" data-id="m5"`,
		"Compared 2 of 2 later messages", "Possible conflict", "dates differ", "links differ", "Pinned", "Later message"} {
		if !strings.Contains(got, want) {
			t.Errorf("comparison misses %q: %s", want, got)
		}
	}
	if !strings.Contains(got, "No differing dates or links") {
		t.Errorf("a message with no dates or links is not reported plainly: %s", got)
	}
	for _, want := range []string{`data-action="pinreview-keep" data-id="p1"`, `data-action="unpin" data-id="p1"`} {
		if !strings.Contains(got, want) {
			t.Errorf("explicit action %q missing: %s", want, got)
		}
	}
}

func TestIssue3960_MissingContextIsSaid(t *testing.T) {
	m := issue3960Model()
	m.PinReview.PinID = "p1"
	// The pinned message is not in the loaded page, and nothing newer is loaded.
	m.Messages = []Message{{ID: "m1", Author: "Walt Brennan", Body: "old", Sequence: 1}}
	got := renderNode(t, chatux019PinnedSection(m))
	for _, want := range []string{"No later messages are loaded", "The date of this pin is not available"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing context not said (%q): %s", want, got)
		}
	}
	// A pin whose position is unknown cannot be compared by time.
	m.ChannelPins[0].Sequence = 0
	got = renderNode(t, chatux019PinnedSection(m))
	if !strings.Contains(got, "cannot tell which messages are newer") {
		t.Errorf("unknown position not said: %s", got)
	}
}

func TestIssue3960_UntrustedTextStaysText(t *testing.T) {
	m := issue3960Model()
	m.Messages[1].Body = `<img src=x onerror=alert(1)> 2026-04-15`
	m.PinReview.PinID = "p1"
	m.PinReview.Selected = map[string]bool{"m5": true}
	got := renderNode(t, chatux019PinnedSection(m))
	if strings.Contains(got, "<img") {
		t.Errorf("message text rendered as markup: %s", got)
	}
}

func TestIssue3960_KeptPinSaysSoAndActionsDispatch(t *testing.T) {
	m := issue3960Model()
	var opened, closed, toggled, kept string
	var jumped uint64
	m.PinReview = PinReviewState{Open: func(id string) { opened = id }, Close: func() { closed = "x" }, Toggle: func(id string) { toggled = id }, Keep: func(id string) { kept = id }}
	m.Callbacks.JumpToPin = func(_ string, seq uint64) { jumped = seq }
	m.act("pinreview-open", "p1")
	m.PinReview.PinID = "p1"
	m.act("pinreview-toggle", "m5")
	m.PinReview.PinID = ""
	m.act("pinreview-keep", "p1")
	m.act("pinreview-inspect", "m5")
	m.act("pinreview-close", "p1")
	if opened != "p1" || toggled != "m5" || kept != "p1" || jumped != 5 || closed == "" {
		t.Errorf("dispatch: %q %q %q %d %q", opened, toggled, kept, jumped, closed)
	}
	// Opening an open review closes it.
	closed = ""
	m.PinReview.PinID = "p1"
	m.act("pinreview-open", "p1")
	if closed == "" {
		t.Errorf("pressing the open review's button did not close it")
	}
	m.PinReview.PinID = ""
	m.PinReview.Kept = map[string]bool{"p1": true}
	got := renderNode(t, chatux019PinnedSection(m))
	if !strings.Contains(got, "Kept after your review") {
		t.Errorf("kept pin not marked: %s", got)
	}
}

func TestIssue3960_CopyInEveryLocaleAndRTL(t *testing.T) {
	for _, loc := range []string{"de-DE", "ar"} {
		for key, en := range issue3960Copy["en-US"] {
			got := issue3960Copy[loc][key]
			if got == "" {
				t.Errorf("%s misses %s", loc, key)
			} else if got == en && !strings.HasPrefix(key, "chat.issue3960.same_") {
				t.Errorf("%s leaves %s in English", loc, key)
			}
		}
	}
	m := issue3960Model()
	m.Locale = "ar"
	m.PinReview.PinID = "p1"
	m.PinReview.Selected = map[string]bool{"m5": true}
	got := renderNode(t, chatux019PinnedSection(m))
	if !strings.Contains(got, issue3960Copy["ar"][keyIssue3960Button]) {
		t.Errorf("Arabic button missing: %s", got)
	}
	if strings.Contains(Issue3960Styles, "margin-left") || strings.Contains(Issue3960Styles, "padding-left") || strings.Contains(Issue3960Styles, "text-align:left") {
		t.Errorf("styles must use logical properties")
	}
}

func TestIssue3960_ValueExtraction(t *testing.T) {
	d, l := issue3960Facts("Due 2026-03-01 or 3/4/2026 or March 5, see https://a.example/x.")
	if len(d) != 3 || len(l) != 1 || l[0] != "https://a.example/x" {
		t.Errorf("dates %v links %v", d, l)
	}
}

func TestIssue3960_ReviewPanelCanTakeFocusAndHasAName(t *testing.T) {
	m := issue3960Model()
	m.PinReview.PinID = "p1"
	got := renderNode(t, chatux019PinnedSection(m))
	for _, want := range []string{`id="pin-review-p1"`, `role="region"`, `tabIndex="-1"`, `aria-label="Review pinned guidance"`} {
		if !strings.Contains(got, want) {
			t.Errorf("panel misses %q: %s", want, got)
		}
	}
}
