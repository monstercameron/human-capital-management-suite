package productui

import (
	"strings"
	"testing"
)

// Issue 900003: the header portrait carries the initials it falls back to, so
// a page-wide guard can replace an image that already failed before any client
// code attached a handler. The Chat page used to show the broken-image glyph.
func TestIssue900003_HeaderPortraitCarriesFallbackInitials(t *testing.T) {
	for _, page := range []PageID{PageChat, PageAgents, PageWork} {
		view := testView(page)
		view.Viewer = ViewerProfile{Name: "Rafael Torres", Initials: "R", PhotoURL: "/workspace/assets/person-hc-050-small.jpg"}
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(doc, `data-portrait-initials="RT"`) {
			t.Fatalf("page %s: header portrait lacks its fallback initials", page)
		}
	}
}

func TestIssue900003_PortraitFallbackLabel(t *testing.T) {
	for _, c := range []struct{ name, initials, want string }{
		{"Rafael Torres", "RT", "RT"}, {"Rafael Torres", "", "RT"}, {"Rafael", " ", "RA"},
	} {
		if got := portraitFallbackLabel(c.name, c.initials); got != c.want {
			t.Fatalf("portraitFallbackLabel(%q,%q)=%q want %q", c.name, c.initials, got, c.want)
		}
	}
}

// Issue 900003: once the guard records a failed portrait (it does, before the
// client hydrates), the client's own render of the header avatar is the same
// initials badge the guard put in the page, with no <img> left to break.
func TestIssue900003_RecordedFailureRendersMatchingInitialsBadge(t *testing.T) {
	const url = "/workspace/assets/person-issue900003-missing.jpg"
	markPortraitBroken(url)
	t.Cleanup(func() {
		brokenPortraits.mu.Lock()
		delete(brokenPortraits.failed, url)
		brokenPortraits.mu.Unlock()
	})
	view := testView(PageChat)
	view.Viewer = ViewerProfile{Name: "Rafael Torres", PhotoURL: url}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, url) {
		t.Fatal("a portrait recorded as failed is still requested by the header")
	}
	if !strings.Contains(doc, `<span aria-hidden="true" class="avatar viewer">RT</span>`) {
		i := strings.Index(doc, "avatar viewer")
		t.Fatalf("header does not draw the initials badge for a failed portrait: %s", doc[max(0, i-60):min(len(doc), i+160)])
	}
}
