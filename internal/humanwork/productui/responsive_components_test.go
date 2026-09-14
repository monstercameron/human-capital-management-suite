package productui

import (
	"strings"
	"testing"
)

func TestSharedComponentsOwnResponsiveSizingContracts(t *testing.T) {
	css := Stylesheet()
	contracts := []string{
		`:where(.main,.page-stack,.surface,.panel,.section-head`,
		`:where(input,select,textarea,button){max-width:100%;}`,
		`.section-head,.panel-foot,.directory-tools,.toolbar{flex-wrap:wrap;}`,
		`.tabs{max-width:100%;overflow-x:auto`,
		`.notifications .popover,.locale-menu .popover{max-width:calc(100vw - 24px);}`,
		`.jn-embedded .jn-tablewrap{overflow-x:auto`,
	}
	for _, contract := range contracts {
		if !strings.Contains(css, contract) {
			t.Errorf("shared responsive contract missing %q", contract)
		}
	}
}

// TestMobileShellKeepsOneBoundedNavigableTree pins the UXAUDIT-001 contract:
// at narrow widths the sidebar is a fixed, off-canvas overlay drawer hidden
// by default (fail-closed — nothing about NavCollapsed's value changes
// that), the header accounts for every child (using two rows below 360px),
// the persistent desktop toggle and the drawer trigger are mutually
// exclusive so brand-cluster never has to size a third visible item, and a
// closed drawer's own nav contributes no scrollable region — only an open
// one may scroll internally.
func TestMobileShellKeepsOneBoundedNavigableTree(t *testing.T) {
	css := Stylesheet()
	contracts := []string{
		`.shell-grid,.app-shell.nav-collapsed .shell-grid{grid-template-columns:minmax(0,1fr);grid-template-rows:auto minmax(0,1fr);}`,
		`.brand-cluster,.app-shell.nav-collapsed .brand-cluster{align-items:center;border-right:0;display:grid!important;grid-template-columns:minmax(0,1fr) 44px!important;`,
		`@media (max-width:760px){.topbar,.app-shell.nav-collapsed .topbar{grid-template-columns:minmax(0,120px) minmax(0,1fr) auto auto auto;}`,
		`.header-nav-toggle,.app-shell.nav-collapsed .header-nav-toggle{display:none;}`,
		`.nav-drawer-trigger{display:none;}`,
		`.nav-drawer-trigger{background:transparent;border:0;border-radius:var(--hcm-radius-control,var(--radius));color:var(--ink);display:grid;height:44px;margin:0 4px 0 0;padding:0;place-items:center;width:44px;}`,
		`.sidebar,.sidebar.collapsed{border-inline-end:1px solid var(--line);border-right:0;box-shadow:0 18px 48px color-mix(in srgb,var(--ink) 22%,transparent);display:flex;flex-direction:column;height:100dvh;inset-block:0;inset-inline-start:-336px;max-width:100%;overflow:hidden;`,
		`visibility:hidden;width:min(86vw,320px);z-index:55;}`,
		`.sidebar.nav-drawer-open,.sidebar.collapsed.nav-drawer-open{inset-inline-start:0!important;visibility:visible!important;}`,
		`.nav-drawer-backdrop{display:none;}`,
		`.nav-drawer-backdrop.nav-drawer-open{background:color-mix(in srgb,var(--ink) 42%,transparent);display:block!important;inset:0;position:fixed;z-index:54;}`,
		`.primary-nav,.sidebar nav:first-of-type{flex:1;max-width:100%;min-width:0;overflow:hidden;width:100%;}`,
		`.sidebar.nav-drawer-open .primary-nav,.sidebar.nav-drawer-open nav:first-of-type{overflow-x:hidden;overflow-y:auto;`,
		`.topbar>.header-navigation-tools{flex:1 1 auto;flex-wrap:nowrap;grid-column:auto;grid-row:auto;min-width:0;overflow-x:auto;overscroll-behavior-inline:contain;padding:0;}`,
		`.primary-nav>ul,.sidebar nav:first-of-type>ul{display:grid!important;max-width:100%!important;width:100%!important;}`,
	}
	for _, contract := range contracts {
		if !strings.Contains(css, contract) {
			t.Errorf("mobile navigation contract missing %q", contract)
		}
	}
	if strings.Count(css, `id="workspace-navigation"`) != 0 {
		t.Fatal("stylesheet unexpectedly contains markup")
	}
}

func TestNarrowContentReflowsInsteadOfClipping(t *testing.T) {
	css := Stylesheet()
	contracts := []string{
		`@media (max-width:760px){.work-row{column-gap:12px;display:grid;grid-template-columns:auto minmax(0,1fr) auto;`,
		`.facts>div{display:grid;grid-template-columns:minmax(0,0.8fr) minmax(0,1.2fr);}`,
		`.settings-nav{flex-wrap:nowrap;overflow-x:auto;overscroll-behavior-inline:contain;}`,
		`.people-row-actions{flex-direction:column;}`,
		`.topbar>.avatar{display:none;}`,
	}
	for _, contract := range contracts {
		if !strings.Contains(css, contract) {
			t.Errorf("narrow component contract missing %q", contract)
		}
	}
}

func TestPeopleHeadersStickToTheMainScrollport(t *testing.T) {
	css := Stylesheet()
	for _, contract := range []string{
		`.people-directory{isolation:isolate;overflow:visible;position:relative;}`,
		`@supports(overflow:clip){.people-directory{overflow:clip;}}`,
		`.people-directory .people-columns{background-color:var(--surface-subtle);box-shadow:0 1px 0 var(--line),0 10px 18px color-mix(in srgb,var(--ink) 8%,transparent);position:sticky;top:0;z-index:4;}`,
		`@media (print){.people-directory .people-columns{box-shadow:none;position:static;}`,
	} {
		if !strings.Contains(css, contract) {
			t.Errorf("sticky people header contract missing %q", contract)
		}
	}
}
