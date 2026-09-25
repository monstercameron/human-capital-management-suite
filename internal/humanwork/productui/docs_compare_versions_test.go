package productui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_DOCS_01 proves the compare dialog is built from two From/To
// pickers, not a bare version-ID text box: it lists the document's
// versions (ListVersions), leaves redacted entries out of the pickers
// (they carry no title to show), and marks the current version in its
// option label.
func TestTodo_DOCS_01(t *testing.T) {
	view := View{Locale: LocaleContext{Resolved: "en-US"}}
	versions := []DocumentVersionSummary{
		{VersionID: "v1", Title: "First draft", CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
		{VersionID: "v2", Redacted: true},
		{VersionID: "v3", Title: "Handbook", CreatedAt: time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC), IsCurrent: true},
	}
	selectable := docsCompareSelectable(versions)
	if len(selectable) != 2 || selectable[0].VersionID != "v1" || selectable[1].VersionID != "v3" {
		t.Fatalf("redacted entry leaked into the picker: %+v", selectable)
	}
	label := docsCompareOptionLabel(view.Locale, func(key string) string { return docsText("en-US", key) }, versions[2], false, docsCompareTitlesDiffer(selectable))
	if !strings.Contains(label, "Handbook") || !strings.Contains(label, "current") {
		t.Fatalf("current version option is not marked current: %q", label)
	}
	if !strings.Contains(label, "Mar 4, 2026") {
		t.Fatalf("option label did not use the locale-aware month-day-year date: %q", label)
	}

	out, err := ui.RenderToString(docsCompareDialog(docsCompareDialogProps{
		Locale: "en-US", DocumentID: "doc-1",
		Base:  DocumentVersionProjection{DocumentID: "doc-1", VersionID: "v3", Title: "Handbook", Markdown: "# Handbook", Readable: true},
		Close: func() {},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id="docs-compare-from"`, `id="docs-compare-to"`, `for="docs-compare-from"`, `for="docs-compare-to"`, "<select"} {
		if !strings.Contains(out, want) {
			t.Fatalf("compare dialog missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `id="docs-compare-input"`) {
		t.Fatal("compare dialog still exposes the old bare version-ID input")
	}
}

// TestTodo_DOCS_01_Localized proves every supported locale carries real
// copy for the new picker labels, not a leaked placeholder.
func TestTodo_DOCS_01_Localized(t *testing.T) {
	for _, locale := range SupportedProductLocales() {
		resolved := ResolveProductLocale(locale).Resolved
		for _, key := range []string{"compare_from", "compare_to", "compare_choose", "compare_current"} {
			if docsText(resolved, key) == "" {
				t.Fatalf("%s missing copy for %s", resolved, key)
			}
		}
	}
}

// TestDocsCompareDateLabel proves the compare pickers use the same
// locale-aware date vocabulary as the rest of Docs (docsWhen /
// docsShortDateLabel) instead of the shared localizer's generic form, and
// that time is added only when two versions would otherwise share a date.
func TestDocsCompareDateLabel(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 5, 0, 0, time.UTC)
	cases := []struct {
		locale string
		want   string
	}{
		{"en-US", "Sep 19, 2026"},
		{"de-DE", "19. Sep. 2026"},
	}
	for _, tc := range cases {
		got := docsCompareDateLabel(ResolveProductLocale(tc.locale), at, false)
		if got != tc.want {
			t.Fatalf("%s date label = %q, want %q", tc.locale, got, tc.want)
		}
	}
	// Arabic gets Arabic-Indic digits and the Arabic month name, not the
	// English fallback.
	arLabel := docsCompareDateLabel(ResolveProductLocale("ar"), at, false)
	if strings.Contains(arLabel, "Sep") || !strings.Contains(arLabel, "سبتمبر") {
		t.Fatalf("ar date label did not use the Arabic month name: %q", arLabel)
	}

	// Two versions on the same day: the picker adds the time so they read
	// as distinct options instead of two identical dates.
	locale := ResolveProductLocale("en-US")
	same := docsCompareDateLabel(locale, at, true)
	if !strings.Contains(same, "Sep 19, 2026") || !strings.Contains(same, "2:05 PM") {
		t.Fatalf("same-day label did not include the time: %q", same)
	}
	view := View{Locale: locale}
	versions := []DocumentVersionSummary{
		{VersionID: "v1", Title: "Morning edit", CreatedAt: at},
		{VersionID: "v2", Title: "Afternoon edit", CreatedAt: at.Add(3 * time.Hour)},
	}
	dateCounts := map[string]int{}
	for _, v := range versions {
		dateCounts[docsCompareDateLabel(view.Locale, v.CreatedAt, false)]++
	}
	for _, v := range versions {
		label := docsCompareOptionLabel(view.Locale, func(key string) string { return docsText("en-US", key) }, v, dateCounts[docsCompareDateLabel(view.Locale, v.CreatedAt, false)] > 1, true)
		if !strings.Contains(label, "PM") {
			t.Fatalf("same-day option did not disambiguate with a time: %q", label)
		}
	}
}
