package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

func TestTodo_UXSCAN_009_Golden(t *testing.T) {
	settings := testView(PageSettings)
	settings.LogoutHref = "/workspace/logout"
	visibility := uxscanVisibilityView()
	admin := testView(PageAdmin)
	pages := []struct {
		name string
		node ui.Node
	}{
		{"settings", settingsPage(settings)},
		{"visibility", organizationVisibilityPage(visibility)},
		{"admin", adminPage(admin)},
	}
	var combined strings.Builder
	for _, page := range pages {
		markup, err := ui.RenderToString(page.node)
		if err != nil {
			t.Fatalf("render %s: %v", page.name, err)
		}
		fmt.Fprintf(&combined, "%s\x00%s\x00", page.name, markup)
	}
	digest := sha256.Sum256([]byte(combined.String()))
	if got, want := hex.EncodeToString(digest[:]), "467d425a6c85e4938c3dce49b7bf872faad4a43fabe9a9a13f955aee78c7ae36"; got != want {
		t.Fatalf("settings, visibility and admin copy/layout digest = %s, want %s", got, want)
	}
}

func TestTodo_UXSCAN_009_Accessibility(t *testing.T) {
	view := uxscanVisibilityView()
	markup, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatalf("parse visibility document: %v", err)
	}
	legend := findHTMLNode(doc, func(n *xhtml.Node) bool { return n.Data == "legend" })
	if legend == nil || !strings.Contains(htmlText(legend), "Which people can members of this role find?") {
		t.Fatal("role visibility choices lack a descriptive legend")
	}
	if findHTMLNode(doc, func(n *xhtml.Node) bool { return n.Data == "fieldset" }) == nil {
		t.Fatal("visibility choices are not grouped in a native fieldset")
	}

	settings := testView(PageSettings)
	settings.LogoutHref = "/workspace/logout"
	settingsMarkup, err := ui.RenderToString(settingsPage(settings))
	if err != nil {
		t.Fatal(err)
	}
	settingsDoc, err := xhtml.Parse(strings.NewReader(settingsMarkup))
	if err != nil {
		t.Fatalf("parse settings document: %v", err)
	}
	if findHTMLNode(settingsDoc, func(n *xhtml.Node) bool {
		return n.Data == "h2" && strings.Contains(htmlText(n), "Organization settings")
	}) == nil {
		t.Fatal("organization-wide appearance is not exposed under its own heading")
	}
}

func TestTodo_UXSCAN_009_Browser(t *testing.T) {
	runUXScanLiveBrowser(t, "UXSCAN-009")
}

func TestTodo_UXSCAN_010_Browser(t *testing.T) {
	runUXScanLiveBrowser(t, "UXSCAN-010")
}

func runUXScanLiveBrowser(t *testing.T, id string) {
	t.Helper()
	if os.Getenv("HCMNEXT_DEV_URL") == "" {
		t.Skip("set HCMNEXT_DEV_URL to run this live Chromium scenario against an authenticated development server")
	}
	command := exec.Command("npx", "playwright", "test", "--config=tools/uxqual/browser/playwright.config.mjs", "--grep="+id)
	command.Dir = repositoryRoot(t)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("live Chromium %s scenario failed: %v\n%s", id, err, output)
	}
	t.Log(string(output))
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for dir := wd; ; {
		if _, err := os.Stat(dir + string(os.PathSeparator) + "go.mod"); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repository root from %s", wd)
		}
		dir = parent
	}
}

func TestTodo_UXSCAN_010_I18N(t *testing.T) {
	for _, tc := range []struct {
		locale, adminLabel, searchLabel, supportLabel string
	}{
		{"en-US", "Admin", "Search Human Capital Management Suite", "Main"},
		{"de-DE", "Administration", "Human Capital Management Suite durchsuchen", "Hauptnavigation"},
		{"ar", "الإدارة", "البحث في Human Capital Management Suite", "الرئيسية"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			view := ApplyLocale(testView(PageAdmin), ResolveProductLocale(tc.locale))
			doc, err := Render(view)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := xhtml.Parse(strings.NewReader(doc))
			if err != nil {
				t.Fatalf("parse %s navigation: %v", tc.locale, err)
			}
			if findHTMLNode(parsed, func(n *xhtml.Node) bool { return n.Data == "nav" && attrHTML(n, "id") == "primary-nav" }) == nil {
				t.Fatal("primary navigation scroll region is missing")
			}
			if !strings.Contains(htmlText(parsed), tc.adminLabel) {
				t.Errorf("%s navigation missing localized Admin destination %q", tc.locale, tc.adminLabel)
			}
			search := findHTMLNode(parsed, func(n *xhtml.Node) bool {
				return n.Type == xhtml.ElementNode && attrHTML(n, "id") == "global-search-input"
			})
			if search == nil || attrHTML(search, "aria-label") != tc.searchLabel {
				t.Errorf("%s global search accessible name = %q, want %q", tc.locale, attrHTML(search, "aria-label"), tc.searchLabel)
			}
			nav := findHTMLNode(parsed, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && attrHTML(n, "id") == "primary-nav" })
			if nav == nil || attrHTML(nav, "aria-label") != tc.supportLabel {
				t.Errorf("%s primary navigation label = %q, want %q", tc.locale, attrHTML(nav, "aria-label"), tc.supportLabel)
			}
		})
	}
}

func TestTodo_UXSCAN_010_Regression(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		".primary-nav{", "overflow-y:auto", "scrollbar-width:thin",
		"scroll-padding-block:12px 24px;", ".primary-nav>ul{padding-block-end:20px;}",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("shared navigation scroll behavior missing %q", want)
		}
	}
	for _, selector := range []string{".subnav", ".nav-group", ".nav-bottom"} {
		if strings.Contains(css, selector+"{overflow-y:auto") || strings.Contains(css, selector+"{overflow-y:scroll") {
			t.Errorf("%s creates a second vertical scroll owner", selector)
		}
	}
}

func findHTMLNode(n *xhtml.Node, match func(*xhtml.Node) bool) *xhtml.Node {
	if n.Type == xhtml.ElementNode && match(n) {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := findHTMLNode(child, match); found != nil {
			return found
		}
	}
	return nil
}

func htmlText(n *xhtml.Node) string {
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.TextNode {
			b.WriteString(node.Data)
			b.WriteByte(' ')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return b.String()
}

func attrHTML(n *xhtml.Node, key string) string {
	if n == nil {
		return ""
	}
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func uxscanVisibilityView() View {
	view := testView(PageOrganizationVisibility)
	view.AccessRoles = []AccessRole{{ID: "team_lead", Name: "Team lead", Active: true}}
	view.RoleVisibilityPolicies = []OrganizationVisibilityPolicy{{RoleID: "team_lead", Mode: "OWN_UNIT"}}
	return view
}
