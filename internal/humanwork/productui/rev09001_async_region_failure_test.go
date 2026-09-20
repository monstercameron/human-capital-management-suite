package productui

import (
	stdhtml "html"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_REV_090_01_Region covers the productui half of REV-090-01: the
// covered set is exactly the four regions the todo names, and the content
// failure is BuildFailure's region without the shell -- the same bytes from
// "loading-progress" on, announced assertively in the reader's locale.
func TestTodo_REV_090_01_Region(t *testing.T) {
	for _, page := range []PageID{PagePeople, PageWork, PageHistory, PageOrganization} {
		if !AsyncRegionFailureCovered(page) {
			t.Fatalf("%s is not covered by the async-region failure contract", page)
		}
	}
	for _, page := range []PageID{PageHome, PageAdmin, PageInsights, PageJourneys, PagePerson} {
		if AsyncRegionFailureCovered(page) {
			t.Fatalf("%s moved off its own LoadError degradation", page)
		}
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := testView(PageOrganization)
		view.Locale = ResolveProductLocale(locale)
		retried := 0
		content, err := ui.RenderToString(BuildContentFailure(view, func() { retried++ }))
		if err != nil {
			t.Fatal(err)
		}
		loading, err := ui.RenderToString(ui.CreateElement(LoadingProxy, LoadingProxyProps{Page: PageOrganization}))
		if err != nil {
			t.Fatal(err)
		}
		// Same body, byte for byte, from the progress bar on: no layout shift.
		if content[strings.Index(content, "loading-progress"):] != loading[strings.Index(loading, "loading-progress"):] {
			t.Fatalf("%s: failed region's body differs from the loading body", locale)
		}
		// Visibly distinct, localized, announced assertively, with a real,
		// focusable Retry button (a live retry is a button, not a link).
		for _, want := range []string{
			`class="loading-proxy loading-proxy-organization loading-proxy-failed"`,
			`class="async-region-failure"`, `role="alert"`, `aria-live="assertive"`,
			`<button class="button secondary async-region-retry" type="button">` + stdhtml.EscapeString(view.Locale.Text("shell.load_retry")) + `</button>`,
			`<strong class="async-region-failure-title">` + stdhtml.EscapeString(view.Locale.Text("shell.live_unavailable")) + `</strong>`,
			stdhtml.EscapeString(view.Locale.Text("shell.load_recovery")),
		} {
			if !strings.Contains(content, want) {
				t.Fatalf("%s: failure region missing %q: %s", locale, want, content)
			}
		}
		if strings.Contains(content, `aria-hidden="true"><div class="loading-progress"`) || strings.Contains(content, `sr-only`) {
			t.Fatalf("%s: the failure message is hidden rather than shown", locale)
		}
		if strings.Contains(content, `id="main-content"`) {
			t.Fatalf("%s: content failure rendered a second shell", locale)
		}
	}
	// Without a live router the retry is the page's own address.
	view := testView(PagePeople)
	link, err := ui.RenderToString(BuildContentFailure(view, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(link, `class="button secondary async-region-retry" href="/workspace/app/people`) {
		t.Fatalf("no-router retry is not a link to the failed page: %s", link)
	}
	// The overlay adds no height: it is absolutely positioned inside the
	// positioned, min-height loading box, and the loading motion stops.
	css := Stylesheet()
	rule := func(selector string) string {
		at := strings.Index(css, selector+"{")
		if at < 0 {
			t.Fatalf("stylesheet has no %q rule", selector)
		}
		body := css[at+len(selector)+1:]
		return body[:strings.Index(body, "}")]
	}
	for selector, want := range map[string]string{
		".async-region-failure":                      "position:absolute",
		".loading-proxy-failed .loading-progress":    "display:none",
		".loading-proxy-failed .loading-block:after": "display:none !important",
		".loading-proxy":                             "position:relative",
	} {
		if !strings.Contains(rule(selector), want) {
			t.Fatalf("%s lacks %q: %s", selector, want, rule(selector))
		}
	}
	if !strings.Contains(rule(".loading-proxy"), "min-height:") {
		t.Fatal("the loading proxy no longer declares the min-height the failed region keeps")
	}
}
