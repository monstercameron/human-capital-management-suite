package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// RED for WEB-055: delegation-context selector. When the projection holds
// delegated grants beyond the current context, the topbar must offer a
// selector listing exactly those grants — delegator, expiry, elevation —
// each closing over its inseparable server-projected pair through the same
// exchange contract as the switcher. Nothing selectable means no control.
func TestTodo_WEB_055(t *testing.T) {
	props := web055Fixture()
	selectable := delegationSelectorOptions(props)
	if len(selectable) != 2 {
		t.Fatalf("selectable delegations = %#v, want 2", selectable)
	}
	for _, option := range selectable {
		if !option.Delegated || option.Delegator == "" || option.ExpiresAt == "" {
			t.Fatalf("selectable option is not a complete delegation: %#v", option)
		}
		if sameContextSelection(ContextSelection{TenantID: option.TenantID, ActingContextID: option.ActingContextID}, props.Current) {
			t.Fatalf("current context listed as selectable: %#v", option)
		}
	}

	// An already-assumed delegation is not offered back, and malformed
	// grants never surface.
	assumed := props
	assumed.Current = authorityContextFromOption(selectable[0])
	if got := delegationSelectorOptions(assumed); len(got) != 1 {
		t.Fatalf("assumed-delegation options = %#v, want the other grant only", got)
	}
	withInvalid := props
	withInvalid.Options = append(withInvalid.Options,
		AuthorityContextOption{TenantID: "tenant-c", TenantName: "Broken", ActingContextID: "delegate-c", ActingContextName: "No delegator", Delegated: true},
	)
	if got := delegationSelectorOptions(withInvalid); len(got) != 2 {
		t.Fatalf("invalid grant surfaced: %#v", got)
	}

	// Selector selections travel the same exchange contract.
	selection := ContextSelection{TenantID: selectable[0].TenantID, ActingContextID: selectable[0].ActingContextID}
	seamless := props
	seamless.Exchange, seamless.Commit, seamless.Rollback, seamless.Controller = nil, nil, nil, nil
	if err := SwitchAuthorityContext(seamless, selection); !errors.Is(err, ErrContextExchangeUnavailable) {
		t.Fatalf("seamless selection error = %v, want exchange-unavailable", err)
	}
}

// Golden: the selector for a fixed projection.
func TestTodo_WEB_055_Golden(t *testing.T) {
	node, err := ui.RenderToString(DelegationSelector(web055Fixture()))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(node))
	got := hex.EncodeToString(digest[:])
	const want = "181fb64f1e9752f1b6da412102d225193d2b8993f063e656a1e80f6e2650b973"
	if got != want {
		t.Fatalf("delegation selector golden digest = %s, want %s", got, want)
	}
}

// Browser: topbar slot placement and absence.
func TestTodo_WEB_055_Browser(t *testing.T) {
	view := testView(PageHome)
	view.ContextSwitcher = web055Fixture()
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	selector := findElementByID(root, "delegation-selector")
	if selector == nil {
		t.Fatal("topbar renders no delegation selector despite selectable grants")
	}
	tools := findClassNode(root, "header-navigation-tools")
	switcher := findClassNode(root, "context-switcher")
	if tools == nil || switcher == nil {
		t.Fatal("topbar tools incomplete")
	}
	if !isBeforeIn(tools, switcher, selector) {
		t.Fatal("delegation selector is not placed after the context switcher")
	}
	body := textContent(selector)
	for _, want := range []string{"Maya Chen", "Jon Park", "2026-09-18"} {
		if !strings.Contains(body, want) {
			t.Fatalf("selector missing delegation evidence %q", want)
		}
	}
	for _, opaque := range []string{"delegate-a", "delegate-b", "tenant-a", "tenant-b"} {
		if strings.Contains(body, opaque) {
			t.Fatalf("selector leaks opaque identifier %q into the DOM", opaque)
		}
	}

	plainDoc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	plainRoot, err := xhtml.Parse(strings.NewReader(plainDoc))
	if err != nil {
		t.Fatal(err)
	}
	if findElementByID(plainRoot, "delegation-selector") != nil {
		t.Fatal("topbar renders a delegation selector with no delegations")
	}
}

// Conformance: locales, stylesheet, pair integrity.
func TestTodo_WEB_055_Conformance(t *testing.T) {
	for locale, label := range map[string]string{"en-US": "Acting on behalf", "de-DE": "Handeln im Auftrag", "ar": "التصرف بالنيابة"} {
		props := web055Fixture()
		props.I18nProps = I18nProps{Locale: ResolveProductLocale(locale)}
		node, err := ui.RenderToString(DelegationSelector(props))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(node, label) {
			t.Fatalf("%s selector missing label %q: %s", locale, label, node)
		}
		if strings.Contains(node, "⟦") {
			t.Fatalf("%s selector leaks an unresolved key: %s", locale, node)
		}
	}
	for _, option := range delegationSelectorOptions(web055Fixture()) {
		selection := ContextSelection{TenantID: option.TenantID, ActingContextID: option.ActingContextID}
		if !contextSelectionAllowed(normalizeContextSwitcherProps(web055Fixture()), selection) {
			t.Fatalf("selector offers a selection the contract rejects: %#v", selection)
		}
	}
	css := Stylesheet()
	for _, want := range []string{".delegation-selector", ".delegation-selector-options", ".delegation-selector-status"} {
		if !strings.Contains(css, want) {
			t.Fatalf("controls stylesheet missing %q", want)
		}
	}
}

func web055Fixture() ContextSwitcherProps {
	return ContextSwitcherProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Current: AuthorityContext{
			TenantID: "tenant-a", TenantName: "HarborCare", ActingContextID: "self-a", ActingContextName: "Your own authority",
		},
		Options: []AuthorityContextOption{
			{TenantID: "tenant-a", TenantName: "HarborCare", ActingContextID: "self-a", ActingContextName: "Your own authority"},
			{TenantID: "tenant-a", TenantName: "HarborCare", ActingContextID: "delegate-a", ActingContextName: "Covering HR", Delegated: true, Elevated: true, Delegator: "Maya Chen", ExpiresAt: "2026-09-18"},
			{TenantID: "tenant-b", TenantName: "Northwind", ActingContextID: "delegate-b", ActingContextName: "Covering payroll", Delegated: true, Delegator: "Jon Park", ExpiresAt: "2026-10-02"},
		},
		State:      ContextSwitcherReady,
		Controller: &ContextSwitchController{},
	}
}
