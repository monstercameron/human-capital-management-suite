package chatui

import (
	"os"
	"strings"
	"testing"
)

// Issue 61: on a phone the Conversations drawer is a modal dialog and the
// workspace main column is made inert behind it. Search results live inside
// that main column, so an inert main hid the Results region from assistive
// technology and swallowed taps (they fell through to the list underneath).
func TestIssue61_MobileSearchResultsAreNotInertBehindDrawer(t *testing.T) {
	cases := []struct {
		name                 string
		modal, results       bool
		wantMain, wantSide   bool
		wantSkip, wantDialog bool
	}{
		{"drawer open, no results", true, false, true, true, true, true},
		{"drawer open, results mounted", true, true, false, true, true, true},
		{"drawer closed", false, false, false, false, false, false},
		{"drawer closed, results mounted", false, true, false, false, false, false},
	}
	for _, c := range cases {
		gotMain, gotSide, gotSkip := mobileRailBackgroundInert(c.modal, c.results)
		if gotMain != c.wantMain || gotSide != c.wantSide || gotSkip != c.wantSkip {
			t.Errorf("%s: inert main/side/skip = %v/%v/%v, want %v/%v/%v", c.name, gotMain, gotSide, gotSkip, c.wantMain, c.wantSide, c.wantSkip)
		}
	}
}

// The browser half cannot run natively, so pin that it asks the shared rule
// with the live results state, and that the results panel is a labelled region
// that is announced.
func TestIssue61_MobileRailUsesResultsAwareInertRule(t *testing.T) {
	src, err := os.ReadFile("mobile_rail_focus_js.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	if !strings.Contains(text, "mobileRailBackgroundInert(") || !strings.Contains(text, ".chat-search-results") {
		t.Fatal("the browser rail code does not exempt a mounted results region from the inert background")
	}
	model := Model{
		State: StateReady, Locale: "en-US", SidebarOpen: true, SelectedID: "general", Search: "meeting",
		Conversations:  []Conversation{{ID: "general", Name: "general", Kind: PublicChannel, Joined: true}},
		SearchMessages: []SearchMessage{{ConversationID: "general", ConversationName: "general", Message: Message{ID: "post-1", Author: "Theodore Wright", Body: "Meeting notes"}}},
		Callbacks:      Callbacks{Search: func(string) {}, OpenSearchMessage: func(string, string, uint64) {}},
	}
	markup := renderAgentUXChat3Node(t, Build(model), 320)
	for _, want := range []string{`id="chat-search-results"`, `role="region"`, `aria-label="Results for`, `class="chat-main"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("mobile results markup is missing %q: %s", want, markup)
		}
	}
	// The results region must sit inside the main column, the one the inert rule governs.
	if main, res := strings.Index(markup, `class="chat-main"`), strings.Index(markup, `id="chat-search-results"`); main < 0 || res < main {
		t.Fatal("results region is not inside the main column")
	}
}

// aria-modal on the drawer hides the results (outside the dialog) from
// assistive technology and the Tab trap strands keyboard focus in the rail, so
// both must give way while results are mounted.
func TestIssue61_MobileDrawerIsNotModalWhileResultsAreMounted(t *testing.T) {
	if mobileRailAdvertisesModal(true) {
		t.Fatal("drawer still claims aria-modal while results are lifted over it")
	}
	if !mobileRailAdvertisesModal(false) {
		t.Fatal("drawer lost aria-modal with no results mounted")
	}
	src, err := os.ReadFile("mobile_rail_focus_js.go")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(src), "mobileRailAdvertisesModal("); got < 2 {
		t.Fatalf("aria-modal and the Tab trap must both ask the rule, found %d uses", got)
	}
}
