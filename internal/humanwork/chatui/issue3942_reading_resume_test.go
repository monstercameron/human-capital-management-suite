package chatui

import (
	"strings"
	"testing"
)

func issue3942Model(locale, status string) Model {
	return Model{
		Locale:        locale,
		SelectedID:    "design",
		Conversations: []Conversation{{ID: "design", Name: "design", Kind: PublicChannel}},
		ReadingResume: ReadingResumeView{ConversationID: "design", Status: status},
		Callbacks:     Callbacks{JumpFromReadingResume: func() {}, DismissReadingResume: func() {}},
	}
}

func TestIssue3942_ResumedBarOffersJumpToNewestAndDismiss(t *testing.T) {
	markup := renderNode(t, issue3942Bar(issue3942Model("en-US", ReadingResumed)))
	for _, want := range []string{`id="chat-reading-resume"`, `role="status"`, `aria-live="polite"`, `data-action="reading-resume-newest"`, "Jump to newest", `data-action="reading-resume-dismiss"`, "Back where you were reading"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("resumed bar lacks %q:\n%s", want, markup)
		}
	}
}

func TestIssue3942_NearbyBarExplainsTheChange(t *testing.T) {
	markup := renderNode(t, issue3942Bar(issue3942Model("en-US", ReadingNearby)))
	if !strings.Contains(markup, "no longer available") || !strings.Contains(markup, "reading-resume-newest") {
		t.Fatalf("a moved anchor is not explained, or has no way to the newest:\n%s", markup)
	}
}

func TestIssue3942_UnavailableBarHasNothingToJumpTo(t *testing.T) {
	markup := renderNode(t, issue3942Bar(issue3942Model("en-US", ReadingUnavailable)))
	if strings.Contains(markup, "reading-resume-newest") || !strings.Contains(markup, "newest messages") || !strings.Contains(markup, "reading-resume-dismiss") {
		t.Fatalf("unavailable bar wrong:\n%s", markup)
	}
}

func TestIssue3942_BarBelongsToItsConversationOnly(t *testing.T) {
	m := issue3942Model("en-US", ReadingResumed)
	m.SelectedID = "other"
	if issue3942Bar(m) != nil {
		t.Fatal("another conversation's reading position leaked into this one")
	}
	m = issue3942Model("en-US", "")
	if issue3942Bar(m) != nil {
		t.Fatal("a bar is drawn with nothing to say")
	}
}

func TestIssue3942_ButtonsAreDisabledWithoutCallbacks(t *testing.T) {
	m := issue3942Model("en-US", ReadingResumed)
	m.Callbacks = Callbacks{}
	if got := renderNode(t, issue3942Bar(m)); strings.Count(got, "disabled") < 2 {
		t.Fatalf("live buttons without callbacks:\n%s", got)
	}
}

func TestIssue3942_CopyInEveryLanguage(t *testing.T) {
	for _, status := range []string{ReadingResumed, ReadingNearby, ReadingUnavailable} {
		seen := map[string]string{}
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			markup := renderNode(t, issue3942Bar(issue3942Model(locale, status)))
			text := issue3942Text(issue3942Model(locale, status), status)
			if text == "" || !strings.Contains(markup, text) {
				t.Fatalf("%s/%s has no copy", locale, status)
			}
			for other, prior := range seen {
				if prior == text {
					t.Fatalf("%s/%s repeats %s's copy", locale, status, other)
				}
			}
			seen[locale] = text
		}
	}
	ar := renderNode(t, issue3942Bar(issue3942Model("ar", ReadingResumed)))
	if !strings.Contains(ar, `dir="rtl"`) || !strings.Contains(ar, "الانتقال إلى الأحدث") {
		t.Fatalf("Arabic bar is not right-to-left or not translated:\n%s", ar)
	}
	de := renderNode(t, issue3942Bar(issue3942Model("de-DE", ReadingResumed)))
	if !strings.Contains(de, "Zu den neuesten springen") {
		t.Fatalf("German button missing:\n%s", de)
	}
}

func TestIssue3942_StylesUseLogicalPropertiesAndPhoneTargets(t *testing.T) {
	if strings.Contains(issue3942Styles, "margin-left") || strings.Contains(issue3942Styles, "padding-left") || !strings.Contains(issue3942Styles, "min-height:44px") {
		t.Fatalf("styles are not RTL/phone safe: %s", issue3942Styles)
	}
}
