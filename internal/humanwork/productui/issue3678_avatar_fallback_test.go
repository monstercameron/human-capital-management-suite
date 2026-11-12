package productui

import (
	"strings"
	"testing"
)

// Issue 3678: a viewer whose stored initials are one letter still gets the two
// letters of their name, and a portrait known to have failed renders initials
// instead of an <img>.
func TestIssue3678_HeaderAvatarTwoLettersAndBrokenPortraitFallsBack(t *testing.T) {
	const photo = "/workspace/assets/issue3678-missing.jpg"
	markPortraitBroken(photo)
	view := testView(PageWork)
	view.Viewer = ViewerProfile{Name: "Rafael Torres", Initials: "R", PhotoURL: photo}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, photo) {
		t.Fatal("header still requests a portrait known to have failed")
	}
	if !strings.Contains(doc, ">RT<") || strings.Contains(doc, ">R<") {
		t.Fatal("header avatar must show the two-letter initials RT")
	}
}

func TestIssue3678_ViewerInitials(t *testing.T) {
	for name, want := range map[string]string{"Rafael Torres": "RT", "Rafael": "RA", "Ana María Ruiz": "AR"} {
		if got := viewerInitials(name); got != want {
			t.Fatalf("viewerInitials(%q) = %q, want %q", name, got, want)
		}
	}
}
