package productui

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The two sheets together are the whole sheet, in its old order: taking Chat's
// rules out of the whole leaves exactly the control sheet.
func TestStylesheetSplitReassemblesTheWholeSheet(t *testing.T) {
	chat := ChatStylesheet()
	if chat == "" || !strings.HasPrefix(chat, "@scope (.chat-workspace){") {
		t.Fatalf("the Chat sheet does not start with Chat's scoped rules: %.60q", chat)
	}
	whole := Stylesheet()
	if strings.Count(whole, chat) != 1 {
		t.Fatalf("the whole sheet holds the Chat sheet %d times, want once", strings.Count(whole, chat))
	}
	if got := strings.Replace(whole, chat, "", 1); got != ControlStylesheet() {
		t.Fatalf("the whole sheet without Chat's rules (%d bytes) is not the control sheet (%d bytes)", len(got), len(ControlStylesheet()))
	}
	if strings.Contains(ControlStylesheet(), "@scope (.chat-workspace)") {
		t.Fatal("the control sheet carries Chat's workspace rules")
	}
	if len(ControlStylesheet()) >= len(whole) || len(chat) < 100_000 {
		t.Fatalf("the split moved nothing: control %d, Chat %d, whole %d", len(ControlStylesheet()), len(chat), len(whole))
	}
}

func TestStylesheetSplitForACustomPalette(t *testing.T) {
	theme := CustomerTheme{Palette: "custom", TokenOverrides: map[string]string{
		"color.brand.primary": "#4d1f78", "color.brand.hover": "#371455", "color.brand.soft": "#f3eafb",
		"color.text.primary": "#20142b", "color.text.muted": "#5e5167", "color.canvas": "#fbf9fd",
		"color.surface": "#ffffff", "color.border": "#d9cfdf",
	}}
	if err := ValidateCustomerTheme(theme); err != nil {
		t.Fatal(err)
	}
	whole, err := StylesheetForCustomerTheme(theme)
	if err != nil {
		t.Fatal(err)
	}
	control, err := ControlStylesheetForCustomerTheme(theme)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Replace(whole, ChatStylesheet(), "", 1); got != control {
		t.Fatalf("a custom palette's control sheet (%d bytes) is not its whole sheet without Chat's rules (%d bytes)", len(control), len(got))
	}
	if !strings.Contains(control, "#4d1f78") || strings.Contains(control, "@scope (.chat-workspace)") {
		t.Fatal("the custom control sheet lost the tenant's colours or kept Chat's rules")
	}
	defaultControl, err := ControlStylesheetForCustomerTheme(DefaultCustomerTheme())
	if err != nil || defaultControl != ControlStylesheet() {
		t.Fatalf("the default palette's control sheet is not ControlStylesheet (%v)", err)
	}
}

func TestStylesheetAssetNamesCarryTheDigest(t *testing.T) {
	a := NewStylesheetAsset("chat-", "a{b:c}")
	b := NewStylesheetAsset("chat-", "a{b:d}")
	if a.Name == b.Name || a.Integrity == b.Integrity {
		t.Fatal("different bytes share an address")
	}
	if !regexp.MustCompile(`^chat-[0-9a-f]{64}\.css$`).MatchString(a.Name) || a.Href != "/workspace/assets/"+a.Name || !strings.HasPrefix(a.Integrity, "sha256-") {
		t.Fatalf("asset = %+v", a)
	}
	if got := ChatStylesheetAsset(); got != NewStylesheetAsset(ChatStylesheetAssetPrefix, ChatStylesheet()) {
		t.Fatalf("ChatStylesheetAsset = %+v", got)
	}
}

// The pages that load the Chat sheet are the pages that draw its rules. This
// renders every page and fails when one that is not listed uses a class only
// the Chat sheet defines (it would be unstyled), and when a listed page uses
// none (it would load a sheet for nothing).
func TestChatStylesheetIsLoadedByExactlyThePagesThatUseIt(t *testing.T) {
	control := stylesheetClasses(ControlStylesheet())
	chatOnly := map[string]bool{}
	for class := range stylesheetClasses(ChatStylesheet()) {
		if !control[class] {
			chatOnly[class] = true
		}
	}
	attribute := regexp.MustCompile(`class="([^"]*)"`)
	for _, definition := range PageDefinitions() {
		doc, err := Render(testView(definition.ID))
		if err != nil {
			t.Fatalf("render %s: %v", definition.ID, err)
		}
		var used []string
		for _, match := range attribute.FindAllStringSubmatch(doc, -1) {
			for _, class := range strings.Fields(match[1]) {
				if chatOnly[class] {
					used = append(used, class)
				}
			}
		}
		sort.Strings(used)
		if len(used) > 5 {
			used = used[:5]
		}
		if uses, listed := len(used) > 0, PageNeedsChatStylesheet(definition.ID); uses != listed {
			t.Errorf("%s: draws Chat-only classes %v but PageNeedsChatStylesheet is %v", definition.ID, used, listed)
		}
	}
}

func TestStylesheetClassInventoryIgnoresNegatedClasses(t *testing.T) {
	classes := stylesheetClasses(`.topbar>:not(.notifications,.account-menu){display:none}`)
	if !classes["topbar"] {
		t.Fatal("positive selector class .topbar was omitted")
	}
	for _, excluded := range []string{"notifications", "account-menu"} {
		if classes[excluded] {
			t.Errorf("negated selector class .%s was counted as styled", excluded)
		}
	}
}

func stylesheetClasses(css string) map[string]bool {
	// A class inside :not() excludes matching elements; it does not identify a
	// class that this rule styles. Ignore the simple selector list in :not().
	css = regexp.MustCompile(`:not\([^)]*\)`).ReplaceAllString(css, "")
	classPattern := regexp.MustCompile(`\.([A-Za-z_][A-Za-z0-9_-]*)`)
	out := map[string]bool{}
	for _, match := range classPattern.FindAllStringSubmatch(css, -1) {
		out[match[1]] = true
	}
	return out
}

func TestRenderLinkedLinksTheSheets(t *testing.T) {
	for _, test := range []struct {
		page     PageID
		wantChat bool
	}{{PageChat, true}, {PageChatSettings, true}, {PageHome, false}, {PageAgents, false}} {
		doc, err := RenderLinked(testView(test.page))
		if err != nil {
			t.Fatalf("%s: %v", test.page, err)
		}
		if strings.Contains(doc, "<style>") {
			t.Errorf("%s: a linked document carries an inline sheet", test.page)
		}
		if got := strings.Count(doc, `<link rel="stylesheet" href="/workspace/assets/product-`); got != 1 {
			t.Errorf("%s: %d control sheet links, want 1", test.page, got)
		}
		chatLink := `<link rel="stylesheet" href="` + ChatStylesheetAsset().Href + `" integrity="` + ChatStylesheetAsset().Integrity + `">`
		chatMeta := `<meta name="hcm-chat-stylesheet" content="` + ChatStylesheetAsset().Href + `" data-integrity="` + ChatStylesheetAsset().Integrity + `">`
		if strings.Contains(doc, chatLink) != test.wantChat || strings.Contains(doc, chatMeta) == test.wantChat {
			t.Errorf("%s: Chat link present %v, Chat note present %v; want link %v and note %v", test.page, strings.Contains(doc, chatLink), strings.Contains(doc, chatMeta), test.wantChat, !test.wantChat)
		}
		if len(doc) > 400_000 {
			t.Errorf("%s: the linked document is %d bytes; the sheet is back in it", test.page, len(doc))
		}
	}
}

// Render keeps carrying the whole sheet in the document, for the browser and
// golden tests that load a document with nothing to serve a sheet.
func TestRenderStaysSelfContained(t *testing.T) {
	doc, err := Render(testView(PageChat))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "<style>"+Stylesheet()+"</style>") || strings.Contains(doc, "<link rel=\"stylesheet\"") {
		t.Fatal("Render no longer carries the whole sheet inline")
	}
}
