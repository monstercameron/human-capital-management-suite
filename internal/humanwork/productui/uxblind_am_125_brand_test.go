package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_125_BrandIdentityBreakpointsKeepLogoReadable(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`@media (max-width:430px){.topbar,.app-shell.nav-collapsed .topbar{gap:6px;grid-template-columns:minmax(0,1fr) auto auto;grid-template-rows:44px 44px;`,
		`@media (max-width:760px){.topbar,.app-shell.nav-collapsed .topbar{grid-template-columns:minmax(0,120px) minmax(0,1fr) auto auto auto;`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("header boundary contract missing %q", want)
		}
	}
	if strings.Contains(css, `grid-template-columns:82px minmax(0,1fr) auto auto auto`) {
		t.Fatal("390px identity column still uses the unreadable 82px geometry")
	}

	markup, err := ui.RenderToString(ui.CreateElement(BrandLogo, BrandLogoProps{
		Name: "Ironridge Builders", AccessibleName: "Ironridge Builders company home", LogoURL: "/workspace/assets/logo.svg",
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="brand-logo-image"`, `class="sr-only"`, `>Ironridge Builders company home<`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("brand accessible-name contract missing %q: %s", want, markup)
		}
	}
}
