package chatui

import (
	"strings"
	"testing"
)

// TestIssue3892_SkipLinkDoesNotRewriteTheAddress pins that the Skip to
// conversation link is handled in place: it carries an action the click handler
// turns into a focus move, so the address keeps the open conversation's
// #channel=... fragment and the document keeps its title.
func TestIssue3892_SkipLinkDoesNotRewriteTheAddress(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		model := Model{
			State: StateReady, Locale: locale, SelectedID: "engineering",
			Conversations: []Conversation{{ID: "engineering", Name: "Engineering", Kind: PublicChannel, MemberCount: 3}},
			Messages:      []Message{{ID: "m1", Author: "Ari Chen", Body: "Hello"}},
			Callbacks:     Callbacks{SelectConversation: func(string) {}},
		}
		markup := render(t, model)
		start := strings.Index(markup, `class="chat-skip"`)
		if start < 0 {
			t.Fatalf("%s: skip link missing", locale)
		}
		end := strings.Index(markup[start:], "</a>")
		link := markup[start : start+end]
		if !strings.Contains(link, `data-action="`+issue3892SkipAction+`"`) {
			t.Errorf("%s: skip link must carry the in-place action so activating it never navigates: %s", locale, link)
		}
		if !strings.Contains(link, model.t(KeySkip)) {
			t.Errorf("%s: skip link lost its text: %s", locale, link)
		}
	}
}

func TestIssue3892_SkipActionIsRecognised(t *testing.T) {
	if !issue3892Skips(issue3892SkipAction) || issue3892Skips("") || issue3892Skips("header-details") {
		t.Fatal("only the skip action moves focus to the conversation")
	}
}
