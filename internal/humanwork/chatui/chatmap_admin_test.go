package chatui

import (
	"strings"
	"testing"
)

func chatmapAdminLoaded() ChatmapAdminModel {
	m := ChatmapAdminModel{Locale: "en-US", Loaded: true}
	m.Data.Policy.SharingEnabled, m.Data.Policy.LiveEnabled, m.Data.Policy.CanAdminister = true, true, true
	m.Data.Policy.MaxLiveSeconds, m.Data.Policy.MaxRetentionSeconds = 28800, 86400
	m.Data.Jurisdictions = []ChatmapCountry{{Country: "*", Enabled: true, Basis: "default"}, {Country: "DE", Enabled: false, Basis: "works council agreement pending"}}
	return m
}

// The workspace's location settings: every state drawn, in all three languages,
// with one primary button.
func TestTodo_CHATMAP_006_WorkspaceSettings(t *testing.T) {
	m := chatmapAdminLoaded()
	markup := renderNode(t, ChatmapWorkspaceSettings(m))
	for _, want := range []string{
		"Location sharing", "Allow people to share a location", "Allow live sharing", "Allow exact positions", "Longest live share", "Longest time a location is kept",
		"nothing is kept longer than 24 hours", "By country", "DE: location sharing Off. Basis: works council agreement pending", "Everywhere else: location sharing On. Basis: default",
		"Country code or name", "Basis for this choice", "Save country", `name="maxlive"`, `name="retention"`, `value="86400"`, `value="28800"`, "chatlangadmin-page",
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("settings lack %q: %s", want, markup)
		}
	}
	// Only shorter and the stated maximum are offered; nothing past 24 hours.
	if strings.Contains(markup, `value="172800"`) || strings.Contains(markup, "48 hours") {
		t.Fatal("a retention longer than 24 hours was offered")
	}
	// One primary action: the country form's button. The switches save by themselves.
	if n := strings.Count(markup, `type="submit"`); n != 1 {
		t.Fatalf("%d submit buttons", n)
	}
	// A value the list does not hold is shown, not lost.
	odd := chatmapAdminLoaded()
	odd.Data.Policy.MaxRetentionSeconds = 1800
	if markup = renderNode(t, ChatmapWorkspaceSettings(odd)); !strings.Contains(markup, "30 minutes") {
		t.Fatal("a stored limit that is not an option was dropped", markup)
	}
	// Empty country list says what that means.
	none := chatmapAdminLoaded()
	none.Data.Jurisdictions = nil
	if markup = renderNode(t, ChatmapWorkspaceSettings(none)); !strings.Contains(markup, "No country is set apart. Location sharing follows the settings above everywhere.") {
		t.Fatal("empty country list", markup)
	}
	// Saved stands beside the setting that was changed.
	saved := chatmapAdminLoaded()
	saved.Saved, saved.SavedField = true, "retention"
	if markup = renderNode(t, ChatmapWorkspaceSettings(saved)); !strings.Contains(markup, `chatlangadmin-saved`) {
		t.Fatal("no Saved mark", markup)
	}
	// Loading, refusal, failure with Try again, and a failed save over good data.
	if markup = renderNode(t, ChatmapWorkspaceSettings(ChatmapAdminModel{Locale: "en-US"})); !strings.Contains(markup, "Loading location settings. Please wait.") || !strings.Contains(markup, `aria-busy="true"`) {
		t.Fatal("loading", markup)
	}
	if markup = renderNode(t, ChatmapWorkspaceSettings(ChatmapAdminModel{Locale: "en-US", Loaded: true, Denied: true})); !strings.Contains(markup, "Only a workspace administrator can change location sharing for the workspace.") || strings.Contains(markup, "<form") {
		t.Fatal("denied", markup)
	}
	notAdmin := chatmapAdminLoaded()
	notAdmin.Data.Policy.CanAdminister = false
	if markup = renderNode(t, ChatmapWorkspaceSettings(notAdmin)); strings.Contains(markup, "<form") {
		t.Fatal("a non-administrator was shown the form", markup)
	}
	if markup = renderNode(t, ChatmapWorkspaceSettings(ChatmapAdminModel{Locale: "en-US", Failed: true})); !strings.Contains(markup, "Location settings could not load or save. Try again.") || !strings.Contains(markup, ">Try again<") {
		t.Fatal("failed", markup)
	}
	out := chatmapAdminLoaded()
	out.Failed, out.SignedOut = true, true
	if markup = renderNode(t, ChatmapWorkspaceSettings(out)); !strings.Contains(markup, "You were signed out") || !strings.Contains(markup, ">Try again<") || !strings.Contains(markup, "Location sharing") || !strings.Contains(markup, "works council agreement pending") {
		t.Fatal("a failed save blanked the settings", markup)
	}
	bad := chatmapAdminLoaded()
	bad.Invalid = true
	if markup = renderNode(t, ChatmapWorkspaceSettings(bad)); !strings.Contains(markup, "Enter a country and the basis for the choice.") {
		t.Fatal("invalid country form", markup)
	}
	// Every string is in English, de-DE and ar, and none is shown as its key.
	for key, values := range chatmapAdminCopy {
		for i, locale := range []string{"en-US", "de-DE", "ar"} {
			got := ChatmapAdminText(locale, key)
			if got == "" || got == key || strings.HasPrefix(got, "admin_") {
				t.Fatalf("%s in %s prints %q", key, locale, got)
			}
			if i > 0 && values[i] == values[0] && !strings.HasPrefix(key, "admin_d") && key != "admin_retry" {
				t.Fatalf("%s is not translated for %s", key, locale)
			}
		}
	}
	for _, locale := range []string{"de-DE", "ar"} {
		l := chatmapAdminLoaded()
		l.Locale = locale
		markup = renderNode(t, ChatmapWorkspaceSettings(l))
		if strings.Contains(markup, "Allow people to share a location") || !strings.Contains(markup, ChatmapAdminText(locale, "admin_title")) {
			t.Fatal(locale, "settings are not in the language")
		}
		if locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
			t.Fatal("Arabic settings are not right to left")
		}
	}
}
