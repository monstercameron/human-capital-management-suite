package productui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UIPOLISH_004_MobileDrawerHasVisibleCloseControl(t *testing.T) {
	view := testView(PageHome)
	props := navigationSidebarPropsForQuery(view)
	props.Open = true
	props.OnClose = func() {}
	markup, err := ui.RenderToString(ui.CreateElement(NavigationSidebar, props))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="nav-drawer-close"`,
		`type="button"`,
		`aria-label="Close navigation menu"`,
		`class="nav-drawer-close-glyph"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("open drawer markup missing %q: %s", want, markup)
		}
	}
}

func TestTodo_UIPOLISH_004_MobileDrawerCloseCSSContract(t *testing.T) {
	css := Stylesheet()
	if !strings.Contains(css, `.nav-drawer-close{display:none;}`) {
		t.Fatal("drawer close is not hidden by default")
	}
	for _, want := range []string{`display:grid`, `align-self:flex-end`, `height:44px`, `min-height:44px`} {
		if !regexp.MustCompile(`\.nav-drawer-close\{[^}]*` + regexp.QuoteMeta(want)).MatchString(css) {
			t.Fatalf("mobile drawer close CSS missing %q", want)
		}
	}
	if !regexp.MustCompile(`\.nav-drawer-close-glyph\{[^}]*height:20px[^}]*width:20px`).MatchString(css) {
		t.Fatal("drawer close glyph has no compact size")
	}
}

func TestTodo_UIPOLISH_006_MobileDrawerCloseIsGovernedIcon(t *testing.T) {
	for _, definition := range IconDefinitions() {
		if definition.Name == "close" {
			if definition.Path != "M6 6l12 12M18 6 6 18" {
				t.Fatalf("close icon path = %q", definition.Path)
			}
			return
		}
	}
	t.Fatal("close icon is not registered")
}
