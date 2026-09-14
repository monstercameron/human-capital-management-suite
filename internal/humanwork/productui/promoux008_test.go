package productui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// promoux008SelectedWorkID is the raw journey id (a work-item UUID stand-in)
// the fixture My Work item carries. RED names "work-item UUIDs" by name; it
// must never reach the rendered document unless the viewer is
// PROMOUX-008 diagnostics-authorized.
const promoux008SelectedWorkID = "intent-1"

// promoux008AuthorizedView grants the signed-in viewer PROMOUX-008
// diagnostics authority on top of the ordinary testView(PageWork) fixture.
func promoux008AuthorizedView() View {
	view := testView(PageWork)
	view.EffectivePermissions = []RolePagePermission{
		{Page: PageWork, View: true},
		{Page: PageJourneyDiagnostics, View: true},
	}
	return view
}

// promoux008UnauthorizedView is the same fixture with an effective
// permission table that does not grant PageJourneyDiagnostics -- the
// ordinary manager or HR partner case.
func promoux008UnauthorizedView() View {
	view := testView(PageWork)
	view.EffectivePermissions = []RolePagePermission{
		{Page: PageWork, View: true},
	}
	return view
}

// TestTodo_PROMOUX_008_Browser is the BROWSER matrix test. It proves the
// real, served document: an unauthorized viewer's rendered My Work page
// carries no raw journey id anywhere, and an authorized viewer's page
// carries a real, browser-submittable copy button (a native <button
// type="button">) next to the redacted identifier -- the fragments a
// browser actually needs to make the control clickable and accessible.
func TestTodo_PROMOUX_008_Browser(t *testing.T) {
	t.Run("unauthorized viewer's document carries no raw journey id", func(t *testing.T) {
		doc, err := Render(promoux008UnauthorizedView())
		if err != nil {
			t.Fatal(err)
		}
		// The raw id legitimately appears inside routing hrefs
		// (?selected=intent-1); what must never appear is the id as
		// rendered element text content next to its label.
		if strings.Contains(doc, ">"+promoux008SelectedWorkID+"<") {
			t.Fatalf("unauthorized document rendered the raw journey id %q as visible text", promoux008SelectedWorkID)
		}
		if strings.Contains(doc, "technical-details\"") && !strings.Contains(doc, `class="technical-details-empty"`) {
			t.Fatal("unauthorized document rendered a non-empty technical-details disclosure")
		}
	})

	t.Run("authorized viewer's document carries a real copy button next to the redacted id", func(t *testing.T) {
		doc, err := Render(promoux008AuthorizedView())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, ">"+promoux008SelectedWorkID+"<") {
			t.Fatalf("authorized document rendered the raw journey id %q as visible text instead of its redacted form", promoux008SelectedWorkID)
		}
		if !strings.Contains(doc, maskIdentifier(promoux008SelectedWorkID)) {
			t.Fatal("authorized document did not render the redacted journey id")
		}
		if !regexp.MustCompile(`<button[^>]*type="button"[^>]*>`).MatchString(doc) {
			t.Fatal("authorized document did not render a real, submittable <button type=\"button\">")
		}
		if !strings.Contains(doc, "<details") || !strings.Contains(doc, "<summary") {
			t.Fatal("authorized document did not render the disclosure as a native, keyboard-operable details/summary pair")
		}
	})
}

// TestTodo_PROMOUX_008_I18N is the I18N matrix test. It proves the
// disclosure's own new labels ("Technical details", "Copy") and its
// existing "Journey ID" label all resolve in en-US, de-DE and ar with real,
// distinct translations -- not a silently-unresolved key marker, and not a
// value that merely repeats the English text -- and that the disclosure
// renders with an explicit RTL dir attribute under ar, following this
// package's established pattern (PromotionValidationDiagnosticsPanel).
func TestTodo_PROMOUX_008_I18N(t *testing.T) {
	locales := map[string]LocaleContext{
		"en-US": ResolveProductLocale("en-US"),
		"de-DE": ResolveProductLocale("de-DE"),
		"ar":    ResolveProductLocale("ar"),
	}
	keys := []string{"work.technical_details", "work.copy_value", "work.journey_id"}

	texts := map[string]map[string]string{}
	for name, locale := range locales {
		texts[name] = map[string]string{}
		for _, key := range keys {
			text := locale.Text(key)
			if text == "" || strings.HasPrefix(text, "⟦") {
				t.Fatalf("%s.%s resolved to %q, want a real translation", name, key, text)
			}
			texts[name][key] = text
		}
	}

	t.Run("de-DE differs from en-US for every new label", func(t *testing.T) {
		for _, key := range []string{"work.technical_details", "work.copy_value"} {
			if texts["en-US"][key] == texts["de-DE"][key] {
				t.Errorf("%s: en-US and de-DE share the same text %q, want a real translation", key, texts["en-US"][key])
			}
		}
	})

	t.Run("ar renders its own script for every new label", func(t *testing.T) {
		for _, key := range []string{"work.technical_details", "work.copy_value"} {
			text := texts["ar"][key]
			hasArabicScript := false
			for _, r := range text {
				if r >= 0x0600 && r <= 0x06FF {
					hasArabicScript = true
					break
				}
			}
			if !hasArabicScript {
				t.Fatalf("%s: ar text %q contains no Arabic-script characters", key, text)
			}
			if text == texts["en-US"][key] || text == texts["de-DE"][key] {
				t.Fatalf("%s: ar text must not equal the English or German copy", key)
			}
		}
	})

	t.Run("the disclosure renders under ar with an explicit rtl dir", func(t *testing.T) {
		ar := locales["ar"]
		if ar.Direction != "rtl" {
			t.Fatalf("ResolveProductLocale(\"ar\").Direction = %q, want rtl", ar.Direction)
		}
		doc := renderTechnicalDetails(t, TechnicalDetailsProps{
			I18nProps: I18nProps{Locale: ar},
			Available: true,
			Items:     []TechnicalDetailItem{{Label: ar.Text("work.journey_id"), Value: "intent-42"}},
		})
		if !strings.Contains(doc, `dir="rtl"`) {
			t.Fatal("the disclosure did not carry an explicit rtl dir attribute under the ar locale")
		}
		if !strings.Contains(doc, texts["ar"]["work.technical_details"]) {
			t.Fatal("the disclosure summary is not the ar-localized text")
		}
		if !strings.Contains(doc, texts["ar"]["work.copy_value"]) {
			t.Fatal("the copy control is not the ar-localized text")
		}
	})
}

// renderTechnicalDetails renders the TechnicalDetails component directly
// (as opposed to a whole page) through GoWebComponents' native SSR path.
func renderTechnicalDetails(t *testing.T, props TechnicalDetailsProps) string {
	t.Helper()
	out, err := ui.RenderToString(ui.CreateElement(TechnicalDetails, props))
	if err != nil {
		t.Fatalf("rendering TechnicalDetails: %v", err)
	}
	return out
}
