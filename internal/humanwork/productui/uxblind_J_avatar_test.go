package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_047(t *testing.T) {
	markup, err := ui.RenderToString(personAvatar("Arjun Singh", "AS", "", "small"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="avatar small"`, `aria-hidden="true"`, ">AS<"} {
		if !strings.Contains(markup, want) {
			t.Errorf("photo-less avatar missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "<img") {
		t.Fatal("photo-less avatar rendered an empty image instead of initials")
	}
}

func TestTodo_UXBLIND_047_Browser(t *testing.T) {
	markup, err := ui.RenderToString(personAvatar("Barb Jones", "BJ", "/workspace/assets/missing-photo.webp", "tiny"))
	if err != nil {
		t.Fatal(err)
	}
	// SSR starts with the photo when one is configured; the same node carries
	// an error listener that swaps it to the initials avatar after a failed load.
	for _, want := range []string{
		`class="avatar tiny"`,
		`src="/workspace/assets/missing-photo.webp"`,
		`loading="lazy"`,
		`decoding="async"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("photo avatar fallback contract missing %q: %s", want, markup)
		}
	}
}
