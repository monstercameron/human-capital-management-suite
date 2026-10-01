package productui

import (
	"strings"
	"testing"
)

func TestHeaderViewerPhotoLinksToMyself(t *testing.T) {
	view := testView(PageWork)
	view.Viewer = ViewerProfile{Name: "Taylor Morgan", Initials: "TM", PhotoURL: "/workspace/assets/person-hc-050-small.jpg", Role: "People Partner"}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		// UXBLIND-064: the avatar is the account disclosure trigger; Myself is its first link.
		`class="popover-root account-menu network-slot network-slot-ready"`, `class="viewer-profile-link"`, `href="/workspace/app/myself`,
		`aria-label="Account menu"`,
		`src="/workspace/assets/person-hc-050-small.jpg"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("viewer profile link missing %q", want)
		}
	}
}

func TestSettingsContainsTheViewerProfileSection(t *testing.T) {
	view := testView(PageSettings)
	view.Viewer = ViewerProfile{Name: "Taylor Morgan", Initials: "TM", PhotoURL: "/workspace/assets/person-hc-050-small.jpg", Role: "People Partner"}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id="user-profile"`, `id="user-profile-name"`, "User profile", "Taylor Morgan", "People Partner"} {
		if !strings.Contains(doc, want) {
			t.Errorf("settings profile section missing %q", want)
		}
	}
}
