package productui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UIPOLISH_006(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{"min-height:44px", ":focus-visible", ":disabled", ":not(:disabled):active", "var(--hcm-radius-control)"} {
		if !strings.Contains(css, want) {
			t.Fatalf("control contract missing %q: %s", want, css)
		}
	}
}

func TestTodo_UIPOLISH_006_StyleContract(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{"var(--accent-hover)", "var(--hcm-color-focus)", "var(--danger)", "var(--success)", "var(--muted)", "var(--surface-subtle)"} {
		if !strings.Contains(css, want) {
			t.Fatalf("browser controls bypass semantic token %q", want)
		}
	}
}

func TestTodo_UIPOLISH_006_Accessibility(t *testing.T) {
	css := Stylesheet()
	if !strings.Contains(css, `data-hcm-density="compact"`) || !strings.Contains(css, "--hcm-control-height-compact:44px") || !strings.Contains(css, `:root[data-hcm-density="compact"] :where(.app-shell,.jn-embedded) :is(button,input,select,textarea){min-height:var(--hcm-control-height-compact)`) {
		t.Fatal("compact controls lack an accessible target")
	}
	if !strings.Contains(css, ":where(.app-shell) .button.primary") || !strings.Contains(css, "color:var(--on-brand)") {
		t.Fatal("primary control does not consume semantic on-brand foreground")
	}
}

// controlTargetPx is the height every inline control in the product now
// resolves to. The 44px target UIPOLISH-006 established is expressed as a
// token rather than repeated as a literal, so this reads the token and
// fails if it were ever set below the minimum -- which grepping for "44px"
// could not have caught.
func controlTargetPx(t *testing.T, css string) int {
	t.Helper()
	match := regexp.MustCompile(`--hcm-control-height:(\d+)px`).FindStringSubmatch(css)
	if len(match) != 2 {
		t.Fatal("the shared control height token is not declared")
	}
	px, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatalf("control height token is not a pixel value: %v", err)
	}
	if px < 44 {
		t.Fatalf("the shared control height is %dpx, below the 44px target", px)
	}
	return px
}

// targetHeight matches a declaration that reaches the target, whether it
// names the pixels or the token that carries them.
func targetHeight(prop string) string {
	return prop + `:(?:44px|var\(--hcm-control-height\))`
}

func TestTodo_UIPOLISH_006_ProductionTargetSize(t *testing.T) {
	css := Stylesheet()
	controlTargetPx(t, css)
	for _, selector := range []string{
		`.history-sort`, `.studio-back-link`, `.page-size-control select`, `.page-size-apply`,
		`.organization-view-option`, `.permission-check`, `.ownership-person-disclosure>summary`, `.subnav-link`,
		`.subnav .nav-link`, `.locale-option`, `.people-pager .button`, `.menu-filter-clear`,
		`.button.compact`, `.context-switcher-trigger`, `.popover-root>summary`,
	} {
		if !regexp.MustCompile(regexp.QuoteMeta(selector) + `\{[^}]*` + targetHeight("min-height")).MatchString(css) {
			t.Errorf("%s has no 44px target height", selector)
		}
		// A later responsive or more-specific rule must not silently reduce
		// a link or control below the minimum after this base rule wins.
		for _, rule := range regexp.MustCompile(regexp.QuoteMeta(selector)+`\{([^}]*)\}`).FindAllStringSubmatch(css, -1) {
			for _, declaration := range regexp.MustCompile(`(?:^|;)min-height:(\d+)px`).FindAllStringSubmatch(rule[1], -1) {
				px, _ := strconv.Atoi(declaration[1])
				if px < 44 {
					t.Errorf("%s has a later %dpx target override", selector, px)
				}
			}
		}
	}
	for _, selector := range []string{`.history-navigation-button`, `.header-nav-toggle`} {
		match := regexp.MustCompile(regexp.QuoteMeta(selector) + `\{([^}]*)\}`).FindStringSubmatch(css)
		if len(match) != 2 || !regexp.MustCompile(targetHeight("width")).MatchString(match[1]) ||
			!regexp.MustCompile(targetHeight("height")).MatchString(match[1]) {
			t.Errorf("%s has no 44px target", selector)
		}
	}
	if !strings.Contains(css, `.nav-drawer-trigger{background:transparent;border:0;border-radius:var(--hcm-radius-control,var(--radius));color:var(--ink);display:grid;height:44px;`) {
		t.Error("mobile navigation drawer trigger has no 44px target")
	}
	if !strings.Contains(css, `.nav-favorite{min-height:44px;min-width:44px;}`) {
		t.Error("favorite action has no 44px target")
	}
	if !strings.Contains(css, `.menu-filter-submit{height:44px;width:44px;}`) {
		t.Error("menu search action has no 44px target")
	}
	if strings.Contains(css, `@media (max-width:760px){.people-sort{`) && strings.Contains(css, `@media (max-width:760px){.people-sort{background:var(--surface);border:1px solid var(--line);border-radius:var(--hcm-radius-control);min-height:36px;`) {
		t.Error("mobile people sort overrides its accessible target height")
	}
	if !strings.Contains(css, `@media (max-width:760px){.jn-embedded .jn-journey-technical>summary{align-items:center;display:flex;min-height:44px;}`) {
		t.Error("mobile journey technical disclosure has no 44px target")
	}
	for _, item := range []struct {
		selector string
		checks   []string
	}{
		{`.global-search .global-search-input`, []string{`width:44px`, `min-height:44px`}},
		{`.action-launcher-trigger,.utility-drawer-trigger`, []string{`width:44px`, `height:44px`, `min-height:44px`}},
	} {
		match := regexp.MustCompile(`@media \(max-width:430px\)\{` + regexp.QuoteMeta(item.selector) + `\{([^}]*)\}`).FindStringSubmatch(css)
		if len(match) != 2 {
			t.Errorf("mobile %s has no target rule", item.selector)
			continue
		}
		for _, check := range item.checks {
			if !strings.Contains(match[1], check) {
				t.Errorf("mobile %s lacks %s", item.selector, check)
			}
		}
	}
}

func TestTodo_UIPOLISH_006_GenericPopoverTargetAndFocus(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(TransientPopover, TransientPopoverProps{
		Kind: "generic", Label: "Open options", Trigger: []ui.Node{ui.Text("Options")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `class="popover-root"`) || !strings.Contains(markup, `<summary aria-label="Open options"`) {
		t.Fatalf("generic popover lost its summary trigger: %s", markup)
	}
	css := Stylesheet()
	if !regexp.MustCompile(`\.popover-root>summary\{[^}]*`+targetHeight("min-height")).MatchString(css) ||
		!regexp.MustCompile(`:focus-visible\{[^}]*outline:[^;}]+`).MatchString(css) {
		t.Fatal("generic popover trigger lacks a touch target or visible keyboard focus")
	}
}

func TestTodo_UIPOLISH_006_Regression_ScopeUsesSharedControlGeometry(t *testing.T) {
	css := Stylesheet()
	match := regexp.MustCompile(`\.scope\{([^}]*)\}`).FindStringSubmatch(css)
	if len(match) != 2 {
		t.Fatalf("scope control rule missing: %s", css)
	}
	declarations := match[1]
	for _, want := range []string{
		"min-height:var(--hcm-control-height)",
		"border-radius:var(--hcm-radius-control)",
		"background-color:var(--surface)",
	} {
		if !strings.Contains(declarations, want) {
			t.Errorf("scope control does not use shared semantic geometry %q: %s", want, declarations)
		}
	}
	if strings.Contains(declarations, "min-height:42px") {
		t.Fatal("scope control regressed below the shared 44px target")
	}
}

func TestTodo_UIPOLISH_006_Regression_OpenDrawerKeepsCloseActionVisible(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`:where(.app-shell):has(.sidebar.nav-drawer-open) .nav-drawer-trigger{`,
		`inset-block-start:12px`,
		`inset-inline-start:calc(min(86vw,320px) - 52px)`,
		`z-index:60`,
		`:where(.app-shell):has(.sidebar.nav-drawer-open) .nav-drawer-trigger .nav-icon{display:none;}`,
		`:where(.app-shell):has(.sidebar.nav-drawer-open) .nav-drawer-trigger::after{content:"×";`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("open drawer close affordance missing %q", want)
		}
	}
}

func TestTodo_UIPOLISH_006_Regression_SummaryControlsKeepTargetAndFocus(t *testing.T) {
	css := Stylesheet()
	if !regexp.MustCompile(`\.context-switcher-trigger,\.popover-root>summary\{` + targetHeight("min-height")).MatchString(css) {
		t.Error("summary controls do not share the 44px target")
	}
	focus := regexp.MustCompile(`\.popover-root>summary:focus-visible\{([^}]*)\}`).FindStringSubmatch(css)
	if len(focus) != 2 {
		t.Fatal("popover summary has no explicit focus-visible rule")
	}
	for _, want := range []string{"outline:2px solid var(--hcm-color-focus)", "outline-offset:2px", "box-shadow:var(--hcm-focus-ring)"} {
		if !strings.Contains(focus[1], want) {
			t.Errorf("popover summary focus rule missing %q: %s", want, focus[1])
		}
	}
}
