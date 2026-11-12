package productui

import (
	"strings"
	"testing"
)

// Issue 3678: while the signed-in identity is still loading, the account
// trigger must be a neutral skeleton, never the initials of the localized
// "Not reported" placeholder (NR in en-US, NG in de-DE).
func TestIssue3678_AccountTriggerIsSkeletonWhileIdentityLoads(t *testing.T) {
	for _, tc := range []struct {
		locale   string
		wrong    string
		wantText string
	}{
		{locale: "en-US", wrong: ">NR<", wantText: "Not reported"},
		{locale: "de-DE", wrong: ">NG<", wantText: "Nicht gemeldet"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			view := testView(PageWork)
			view.Locale = ResolveProductLocale(tc.locale)
			view.Viewer = ViewerProfile{}
			doc, err := Render(view)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(doc, tc.wrong) {
				t.Fatalf("account trigger rendered placeholder initials %q while identity loads", tc.wrong)
			}
			if !strings.Contains(doc, `class="avatar viewer avatar-loading"`) {
				t.Fatal("account trigger missing neutral loading skeleton")
			}
		})
	}
}

// A signed-in identity with a name keeps the two-letter initials fallback.
func TestIssue3678_AccountTriggerKeepsInitialsWhenNameLoaded(t *testing.T) {
	view := testView(PageWork)
	view.Viewer = ViewerProfile{Name: "Rafael Torres"}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, ">RT<") {
		t.Fatal("loaded account trigger lost its RT initials")
	}
	if strings.Contains(doc, "avatar-loading") {
		t.Fatal("loaded account trigger still shows the loading skeleton")
	}
}
