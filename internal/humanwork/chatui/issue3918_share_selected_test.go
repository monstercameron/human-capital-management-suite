package chatui

import (
	"strings"
	"testing"
)

func issue3918Model(locale string) Model {
	return Model{State: StateReady, Locale: locale, SelectedID: "source", Conversations: []Conversation{{ID: "source", Name: "Source"}},
		Messages:           []Message{{ID: "post", Author: "Ari", Body: "Review this"}},
		SharePostID:        "post",
		ShareSource:        &Message{ID: "post", Author: "Ari", Body: "Review this"},
		ShareDestinations:  []Conversation{{ID: "eng", Name: "engineering", Kind: PublicChannel}, {ID: "ben", Name: "benefits", Kind: PublicChannel}},
		ShareDestinationID: "eng", ShareQuery: "benefits",
		Callbacks: Callbacks{OpenMenu: func(string) {}, OpenShare: func(string) {}, CloseShare: func() {}, SelectShareDestination: func(string) {}, ShareMessage: func() {}}}
}

// A checked destination that the filter hides stays the audience: it is named
// in the dialog, can be removed, and Share stays available.
func TestIssue3918_FilteredOutDestinationStaysVisibleAndRemovable(t *testing.T) {
	markup := render(t, issue3918Model("en-US"))
	if strings.Contains(markup, `data-action="share-destination" data-id="eng"`) {
		t.Fatal("filtered-out row should not be listed")
	}
	for _, want := range []string{`class="share-selected"`, `role="status"`, "Sharing to engineering", `data-action="share-clear"`, `Remove engineering`} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %q", want)
		}
	}
	i := strings.Index(markup, `data-action="share-submit"`)
	if i < 0 || strings.Contains(markup[i:i+80], "disabled") {
		t.Fatalf("Share must stay enabled while a hidden destination is selected: %s", markup[max(0, i-20):i+120])
	}
}

func TestIssue3918_NoSelectionShowsNoSummaryAndDisablesShare(t *testing.T) {
	m := issue3918Model("en-US")
	m.ShareDestinationID = ""
	markup := render(t, m)
	if strings.Contains(markup, `class="share-selected"`) || strings.Contains(markup, `data-action="share-clear"`) {
		t.Fatal("summary shown without a selection")
	}
	if i := strings.Index(markup, `data-action="share-submit"`); i < 0 || !strings.Contains(markup[i:i+80], "disabled") {
		t.Fatal("Share must be disabled without a destination")
	}
}

func TestIssue3918_ClearDispatchesEmptyDestination(t *testing.T) {
	var got = "unset"
	m := Model{Callbacks: Callbacks{SelectShareDestination: func(id string) { got = id }}}
	m.act("share-clear", "")
	if got != "" {
		t.Fatalf("share-clear selected %q", got)
	}
}

func TestIssue3918_SelectedSummaryLocalized(t *testing.T) {
	for locale, want := range map[string][2]string{"de-DE": {"Teilen mit engineering", "engineering entfernen"}, "ar": {"المشاركة مع engineering", "إزالة engineering"}} {
		markup := render(t, issue3918Model(locale))
		for _, w := range want {
			if !strings.Contains(markup, w) {
				t.Errorf("%s: missing %q", locale, w)
			}
		}
	}
}
