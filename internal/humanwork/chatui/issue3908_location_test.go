package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// #3908: the Location sheet names the conversation it will post to.
func TestIssue3908_SheetNamesItsDestination(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup, err := ui.RenderToString(ChatmapShareSheetFor(locale, "composer", "t", "benefits", "#benefits", false))
		if err != nil {
			t.Fatal(err)
		}
		line := ChatmapAudienceText(locale, "#benefits")
		if !strings.Contains(line, "#benefits") || line == ChatmapText(locale, "explain") {
			t.Fatalf("%s audience line %q does not name the destination", locale, line)
		}
		if !strings.Contains(markup, `data-chatmap-destination="true"`) || !strings.Contains(markup, line) {
			t.Fatalf("%s sheet does not show %q: %s", locale, line, markup)
		}
	}
}

func TestIssue3908_ComposerSheetUsesSelectedConversationName(t *testing.T) {
	markup := render(t, Model{State: StateReady, Locale: "en-US", CurrentTenantID: "t", CurrentUser: "alice", SelectedID: "benefits", Conversations: []Conversation{{ID: "benefits", Name: "benefits", Kind: PublicChannel, Joined: true}}, Callbacks: Callbacks{SendMessage: func(string, string) {}}})
	if !strings.Contains(markup, ChatmapAudienceText("en-US", "#benefits")) {
		t.Fatal("the open composer's location sheet does not name #benefits")
	}
}

func TestIssue3908_DirectConversationDoesNotSayEveryone(t *testing.T) {
	m := Model{State: StateReady, Locale: "en-US", CurrentTenantID: "t", CurrentUser: "alice", SelectedID: "dm", Conversations: []Conversation{{ID: "dm", Name: "Sam Lee", Kind: DirectMessage, Joined: true}}, Callbacks: Callbacks{SendMessage: func(string, string) {}}}
	got := chatmapAudience(m)
	if !strings.Contains(got, "Sam Lee") || strings.Contains(got, "Everyone") {
		t.Fatalf("direct conversation audience %q must name the person and not say everyone", got)
	}
	for _, locale := range []string{"de-DE", "ar"} {
		m.Locale = locale
		if line := chatmapAudience(m); !strings.Contains(line, "Sam Lee") || line == ChatmapAudienceText(locale, "Sam Lee") {
			t.Fatalf("%s direct audience %q", locale, line)
		}
	}
}

// #3863: the duration hint follows the selection and the configured maximum.
func TestIssue3863_DurationHintFollowsSelectionAndPolicy(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		keep := ChatmapDurationHint(locale, "keep", 86400)
		timed := ChatmapDurationHint(locale, "hour", 86400)
		if keep == "" || timed == "" || keep == timed {
			t.Fatalf("%s: keep %q and timed %q must differ and be set", locale, keep, timed)
		}
		if !strings.Contains(keep, chatmapAdminDuration(locale, 86400)) {
			t.Fatalf("%s: keep hint %q lacks the 24 hour maximum", locale, keep)
		}
		short := ChatmapDurationHint(locale, "keep", 3600)
		if !strings.Contains(short, chatmapAdminDuration(locale, 3600)) || strings.Contains(short, chatmapAdminDuration(locale, 86400)) {
			t.Fatalf("%s: keep hint %q does not follow the shortened maximum", locale, short)
		}
		if ChatmapDurationHint(locale, "keep", 0) != keep {
			t.Fatalf("%s: an unknown policy must fall back to the 24 hour limit", locale)
		}
		if ChatmapDurationHint(locale, "live15", 86400) != "" {
			t.Fatalf("%s: live choices explain themselves in the live note", locale)
		}
	}
	if keep := ChatmapDurationHint("en-US", "keep", 86400); !strings.Contains(keep, "even if the message") {
		t.Fatalf("keep hint %q does not relate message lifetime to the maximum", keep)
	}
}

func TestIssue3863_SheetTiesHintToDuration(t *testing.T) {
	markup, err := ui.RenderToString(ChatmapShareSheet("en-US", "composer", "t", "c", false))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `id="composer-location-retention"`) || !strings.Contains(markup, `data-chatmap-retention="true"`) {
		t.Fatal("retention hint has no stable id")
	}
	if strings.Count(markup, `aria-describedby="composer-location-retention"`) != 1 {
		t.Fatal("the duration select is not described by the retention hint")
	}
}

// #3862: when address lookup is unavailable the sheet says what to do instead.
func TestIssue3862_LookupUnavailableExplainsAlternative(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		full := ChatmapText(locale, "lookup_full")
		for _, key := range []string{"lookup", "where", "site"} {
			if !strings.Contains(full, ChatmapText(locale, key)) {
				t.Fatalf("%s: %q lacks %q", locale, full, ChatmapText(locale, key))
			}
		}
		unresolved := ChatmapText(locale, "unresolved")
		if unresolved == "" || unresolved == "unresolved" {
			t.Fatalf("%s: unresolved copy missing", locale)
		}
		markup, err := ui.RenderToString(ChatmapShareSheet(locale, "composer", "t", "c", false))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, full) {
			t.Fatalf("%s: sheet hint does not carry the alternative", locale)
		}
	}
}
