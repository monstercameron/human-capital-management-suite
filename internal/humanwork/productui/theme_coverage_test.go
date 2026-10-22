package productui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// finalThemeCoverageLayer renders the terminal cascade boundary exactly as
// the production sheet assembles it: theme coverage base, then the Journey
// focus bridge, then the platform focus boundary (the dark-mode tail).
func finalThemeCoverageLayer() string {
	return buildTypedSheet(func() {
		declareThemeCoverageBaseStylesStyles()
		declareJourneyFocusBridge()
		declareFocusStyles()
	})
}

func TestEveryRegisteredPageReceivesTheCompleteThemeContract(t *testing.T) {
	appearance := CustomerTheme{
		BrandName: "Northstar People", BrandMark: "NP", ColorMode: "dark", Palette: "plum", Shape: "rounded",
		Density: "compact", Glyphs: "bold-line", Typeface: "classic", Navigation: "brand", Motion: "brisk",
	}
	wants := []string{
		`data-hcm-color-mode="dark"`,
		`data-hcm-palette="plum"`, `data-hcm-shape="rounded"`, `data-hcm-density="compact"`,
		`data-hcm-glyphs="bold-line"`, `data-hcm-typeface="classic"`, `data-hcm-navigation="brand"`,
		`data-hcm-motion="brisk"`, `data-hcm-brand-name`, `>Northstar People</span>`,
	}
	for _, definition := range PageDefinitions() {
		t.Run(string(definition.ID), func(t *testing.T) {
			view := testView(definition.ID)
			view.Appearance = appearance
			document, err := Render(view)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range wants {
				if !strings.Contains(document, want) {
					t.Errorf("page %q dropped theme contract %q", definition.ID, want)
				}
			}
			if !strings.Contains(document, `<title>`+escapeTitle(ResolveDocumentPageTitle(view))+` · Northstar People</title>`) {
				t.Errorf("page %q did not brand its document title", definition.ID)
			}
			if !strings.Contains(document, `data-hcm-brand-name="">Northstar People</span>`) {
				t.Errorf("page %q did not brand its footer", definition.ID)
			}
		})
	}
}

func TestEveryProductionPageFamilyConsumesSemanticThemeTokens(t *testing.T) {
	css := Stylesheet()
	if !strings.HasSuffix(css, finalThemeCoverageLayer()) {
		t.Fatal("theme coverage layer is not the final cascade boundary")
	}
	for _, selector := range []string{
		".home-grid", ".workbench", ".history-row", ".people-workspace", ".person-page", ".org-node",
		".insights-grid", ".admin-grid", ".appearance-page", ".studio-shell", ".support-request", ".settings-shell", ".jn-embedded",
	} {
		if !strings.Contains(finalThemeCoverageLayer(), selector) {
			t.Errorf("production page family %q is absent from the final theme coverage layer", selector)
		}
	}
	for _, token := range []string{
		"--surface-subtle:", "--control-border:", "--theme-section-gap:", "var(--hcm-font-sans)",
		"var(--hcm-radius-control)", "var(--hcm-radius-surface)", "var(--hcm-motion-fast)",
	} {
		if !strings.Contains(finalThemeCoverageLayer(), token) {
			t.Errorf("cross-component theme semantic %q is missing", token)
		}
	}
}

func TestDarkThemeMapsHCMColorsUsedByChatSettingsAndSearch(t *testing.T) {
	view := testView(PageChatSettings)
	view.Appearance = CustomerTheme{ColorMode: "dark", Palette: "plum", Shape: "rounded", Density: "comfortable", Glyphs: "rounded-line", Typeface: "system", Navigation: "light", Motion: "standard"}
	controlCSS, err := ControlStylesheetForCustomerTheme(view.Appearance)
	if err != nil {
		t.Fatal(err)
	}
	chatCSS := ChatStylesheet()
	darkDeclarations := cssDeclarations(controlCSS, `:root[data-hcm-color-mode="dark"]`)
	if darkDeclarations == "" {
		t.Fatalf("production control stylesheet has no dark root declaration block: %s", controlCSS)
	}
	for _, want := range []string{
		"--hcm-color-text:var(--ink);",
		"--hcm-color-text-muted:var(--muted);",
		"--hcm-color-surface:var(--surface);",
		"--hcm-color-canvas:var(--canvas);",
		"--hcm-color-border:var(--line);",
	} {
		if !strings.Contains(darkDeclarations, want) {
			t.Errorf("dark root theme mappings lack %q: %s", want, darkDeclarations)
		}
	}

	document, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(document))
	if err != nil {
		t.Fatal(err)
	}
	htmlEl := firstElement(root, "html")
	if htmlEl == nil || attr(htmlEl, "data-hcm-color-mode") != "dark" {
		t.Fatalf("Chat settings document did not render the requested dark theme: %s", document)
	}
	settingsPages := findElementsByClassContains(root, "chat-settings-page")
	if len(settingsPages) != 1 {
		t.Fatalf("Chat settings page wrapper is missing: %s", document)
	}
	settings := settingsPages[0]
	for _, class := range []string{"chatmapadmin", "chatvoiceadmin"} {
		panels := findElementsByClassContains(settings, class)
		if len(panels) != 1 || !strings.Contains(" "+attr(panels[0], "class")+" ", " chatlangadmin ") {
			t.Errorf("dark Chat settings page is missing the themed .chatlangadmin.%s panel", class)
		}
	}

	if !PageNeedsChatStylesheet(PageChatSettings) || !strings.Contains(ChatStylesheetElement(PageChatSettings), ChatStylesheetAsset().Href) {
		t.Error("Chat settings page is not linked to the production Chat stylesheet")
	}
	panelDeclarations := cssDeclarations(chatCSS, ".chatlangadmin")
	for _, want := range []string{
		"color:var(--hcm-color-text)",
		"background:var(--hcm-color-surface)",
		"border:1px solid var(--hcm-color-border)",
	} {
		if !strings.Contains(panelDeclarations, want) {
			t.Errorf("Chat stylesheet's .chatlangadmin rule does not consume %q: %s", want, panelDeclarations)
		}
	}
	searchDeclarations := cssDeclarations(chatCSS, "#chatsearch-results")
	for _, want := range []string{"color:var(--hcm-color-text)", "background:var(--hcm-color-surface)"} {
		if !strings.Contains(searchDeclarations, want) {
			t.Errorf("Chat stylesheet's #chatsearch-results rule does not consume %q: %s", want, searchDeclarations)
		}
	}
}

func TestEmbeddedJourneysUseTheCompleteProductThemeVocabulary(t *testing.T) {
	for _, token := range []string{
		"--jn-canvas:var(--canvas)", "--jn-surface:var(--surface)", "--jn-control-border:var(--control-border)",
		"--jn-masthead:var(--accent)", "--jn-accent-ink:var(--on-brand)", "--jn-info:var(--info)",
		"--jn-success:var(--success)", "--jn-warning:var(--warning)", "--jn-danger:var(--danger)",
		"--jn-font:var(--hcm-font-sans)", "--jn-mono:var(--hcm-font-mono)",
		"--jn-s2:calc(var(--hcm-space-2) * var(--hcm-density))", "--jn-r3:var(--hcm-radius-surface)",
	} {
		if !strings.Contains(finalThemeCoverageLayer(), token) {
			t.Errorf("embedded journey theme bridge missing %q", token)
		}
	}
}

func TestCustomerThemeCanRestyleEverySharedSurfaceWithoutRawCSS(t *testing.T) {
	css, err := StylesheetForTheme(map[string]string{
		"color.brand.primary": "#4d1f78", "color.brand.hover": "#371455", "color.brand.soft": "#f3eafb",
		"color.text.primary": "#20142b", "color.text.muted": "#5e5167", "color.canvas": "#fbf9fd",
		"color.surface": "#ffffff", "color.border": "#d9cfdf", "radius.control": "6px", "radius.surface": "14px",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"--hcm-color-brand-primary:#4d1f78", "--hcm-color-canvas:#fbf9fd", "--hcm-color-surface:#ffffff",
		"--hcm-color-border:#d9cfdf", "--hcm-radius-control:6px", "--hcm-radius-surface:14px", finalThemeCoverageLayer(),
	} {
		if !strings.Contains(css, want) {
			t.Errorf("complete themed stylesheet missing %q", want)
		}
	}
	if _, err := StylesheetForTheme(map[string]string{"color.status.success": "#ff00ff"}); err == nil {
		t.Fatal("customer theme changed a protected semantic status color")
	}
}
