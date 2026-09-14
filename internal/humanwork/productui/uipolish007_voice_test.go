package productui

import (
	stdhtml "html"
	"strings"
	"testing"
)

func TestTodo_UIPOLISH_007_ProductVoiceCopy(t *testing.T) {
	cases := []struct {
		key       string
		want      map[string]string
		noEnglish bool
	}{
		{"journey.error_denied_title", map[string]string{"en-US": "You can't complete this action", "de-DE": "Diese Aktion kann nicht ausgeführt werden", "ar": "لا يمكنك إكمال هذا الإجراء"}, true},
		{"journey.error_not_found_title", map[string]string{"en-US": "Request not found", "de-DE": "Antrag nicht gefunden", "ar": "لم يُعثر على الطلب"}, true},
		{"journey.empty_title", map[string]string{"en-US": "No journeys to track", "de-DE": "Noch keine Anträge", "ar": "لا توجد طلبات بعد"}, true},
		{"context_switcher.failed", map[string]string{"en-US": "We couldn't switch workspaces. Try again.", "de-DE": "Arbeitsbereich konnte nicht gewechselt werden. Versuchen Sie es erneut.", "ar": "تعذر تبديل مساحة العمل. حاول مرة أخرى."}, true},
		{"people.no_workflows", map[string]string{"en-US": "No available workflows", "de-DE": "Keine verfügbaren Abläufe", "ar": "لا توجد مسارات عمل متاحة"}, true},
		{"journey.action_confirm_approve", map[string]string{"en-US": "Confirm approval", "de-DE": "Genehmigung bestätigen", "ar": "تأكيد الموافقة"}, true},
		{"journey.action_confirm_reject", map[string]string{"en-US": "Confirm rejection", "de-DE": "Ablehnung bestätigen", "ar": "تأكيد الرفض"}, true},
	}
	for _, tc := range cases {
		for locale, want := range tc.want {
			got := ResolveProductLocale(locale).Text(tc.key)
			if got != want {
				t.Errorf("%s/%s = %q, want %q", tc.key, locale, got, want)
			}
			if tc.noEnglish && locale != "en-US" && got == tc.want["en-US"] {
				t.Errorf("%s/%s fell back to English", tc.key, locale)
			}
		}
	}
}

func TestTodo_UIPOLISH_007_SettingsCatalog(t *testing.T) {
	keys := []string{
		"settings.session_title", "settings.organization_fact", "settings.account_group_title",
		"settings.account_group_description", "settings.preferences_group_title",
		"settings.preferences_group_description", "settings.signout_action", "settings.signout_description",
	}
	for _, locale := range SupportedProductLocales() {
		view := testView(PageSettings)
		view.Locale = ResolveProductLocale(locale)
		view.LogoutHref = "/workspace/logout"
		doc, err := Render(view)
		if err != nil {
			t.Fatalf("%s: %v", locale, err)
		}
		for _, key := range keys {
			message := view.Locale.Text(key)
			if message == "" || strings.Contains(message, "⟦") {
				t.Errorf("%s/%s has no translated message: %q", locale, key, message)
			}
			if locale != DefaultProductLocale {
				if _, ok := productMessages[locale][key]; !ok || message == ResolveProductLocale(DefaultProductLocale).Text(key) {
					t.Errorf("%s/%s fell back to English: %q", locale, key, message)
				}
			}
			if !strings.Contains(doc, stdhtml.EscapeString(message)) {
				t.Errorf("%s settings page omits %s: %q", locale, key, message)
			}
		}
	}
}

func TestTodo_UIPOLISH_007_SettingsIdentityFallback(t *testing.T) {
	view := testView(PageSettings)
	view.Viewer.Name = ""
	view.Principal = "principal:secret-123"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, view.Principal) || !strings.Contains(doc, stdhtml.EscapeString(view.Locale.Text("common.not_reported"))) {
		t.Fatal("settings exposed an unapproved principal or lost the safe fallback")
	}
}
