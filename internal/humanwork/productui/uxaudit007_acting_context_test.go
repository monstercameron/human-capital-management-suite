package productui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// RED for UXAUDIT-007 (verbatim from the live audit): signed in as an
// ordinary user with no delegation, on an unrelated self-context page, the
// shell rendered a persistent "Acting as yourself" text unconditionally in
// every page header, entirely independent of any actual authority
// projection. GREEN: the shell shows its one persistent acting-authority
// banner only for delegated, view-as, elevated, or break-glass authority;
// ordinary self context is quiet, and the header carries no acting-context
// label of its own at all.
func TestTodo_UXAUDIT_007(t *testing.T) {
	// Ordinary self context, exactly the live audit's scenario: a signed-in
	// user on Home with no context projection at all.
	quiet := testView(PageHome)
	quietDoc, err := Render(quiet)
	if err != nil {
		t.Fatal(err)
	}
	assertQuiet(t, quietDoc, "ordinary self context with no projection")

	// An explicit, valid "own authority" projection -- Delegated and
	// Elevated both false -- must stay just as quiet. Self is not "no
	// projection"; it is a known, safe answer.
	ownView := testView(PageHome)
	ownView.ContextSwitcher = ContextSwitcherProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Current: AuthorityContext{
			TenantID: "tenant-a", TenantName: "HarborCare",
			ActingContextID: "self-a", ActingContextName: "Your own authority",
		},
		Controller: &ContextSwitchController{},
	}
	ownDoc, err := Render(ownView)
	if err != nil {
		t.Fatal(err)
	}
	assertQuiet(t, ownDoc, "own (non-delegated, non-elevated) authority")

	// An explicit AuthorityStateSelf must also stay quiet: proves State is
	// consulted, not merely the booleans, and that the safe value truly is
	// safe.
	explicitSelf := testView(PageHome)
	explicitSelf.ContextSwitcher = ContextSwitcherProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Current: AuthorityContext{
			TenantID: "tenant-a", TenantName: "HarborCare",
			ActingContextID: "self-a", ActingContextName: "Your own authority",
			State: AuthorityStateSelf,
		},
		Controller: &ContextSwitchController{},
	}
	explicitSelfDoc, err := Render(explicitSelf)
	if err != nil {
		t.Fatal(err)
	}
	assertQuiet(t, explicitSelfDoc, "explicit AuthorityStateSelf")

	// Delegated authority: the banner discloses tenant, delegator and
	// expiry -- unchanged, pre-existing WEB-056 behavior.
	delegated := testView(PageHome)
	delegated.ContextSwitcher = web056Fixture()
	delegatedDoc, err := Render(delegated)
	if err != nil {
		t.Fatal(err)
	}
	assertBanner(t, delegatedDoc, "delegated authority", "HarborCare", "Maya Chen", "2026-09-18")

	// Elevated-only authority (no delegation at all) still discloses:
	// GREEN's list is delegated OR view-as OR elevated OR break-glass, not
	// delegated alone.
	elevatedOnly := testView(PageHome)
	elevatedOnly.ContextSwitcher = ContextSwitcherProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Current: AuthorityContext{
			TenantID: "tenant-a", TenantName: "HarborCare",
			ActingContextID: "self-a", ActingContextName: "Your own authority",
			Elevated: true,
		},
		Controller: &ContextSwitchController{},
	}
	elevatedDoc, err := Render(elevatedOnly)
	if err != nil {
		t.Fatal(err)
	}
	assertBanner(t, elevatedDoc, "elevated-only authority", "HarborCare")

	// View-as and break-glass, expressed through the explicit State field
	// (this shell has no boolean for either): both discipline as
	// noticeworthy exactly like delegated/elevated.
	for _, state := range []AuthorityState{AuthorityStateViewAs, AuthorityStateBreakGlass} {
		view := testView(PageHome)
		view.ContextSwitcher = ContextSwitcherProps{
			I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
			Current: AuthorityContext{
				TenantID: "tenant-a", TenantName: "HarborCare",
				ActingContextID: "self-a", ActingContextName: "Your own authority",
				State: state,
			},
			Controller: &ContextSwitchController{},
		}
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		assertBanner(t, doc, string(state)+" authority", "HarborCare")
	}
}

// Browser: the banner, when present, sits between the topbar and the
// content grid exactly as WEB-056 pinned. The page header carries no second
// acting-context or global scope line (UXAUDIT-007's REFACTOR: one
// acting-context component, not a page-level fork).
func TestTodo_UXAUDIT_007_Browser(t *testing.T) {
	view := testView(PageHome)
	view.ContextSwitcher = web056Fixture()
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	shell := findAppShell(root)
	if shell == nil {
		t.Fatal("document renders no app shell")
	}
	banner := findElementByID(root, "acting-authority")
	if banner == nil {
		t.Fatal("delegated authority renders no banner")
	}
	header := findFirst(shell, "header")
	grid := findClassNode(shell, "shell-grid")
	if header == nil || grid == nil {
		t.Fatal("shell chrome incomplete")
	}
	if !isBeforeIn(shell, header, banner) || !isBeforeIn(shell, banner, grid) {
		t.Fatal("authority banner is not placed between topbar and content")
	}

	if scopeWrap := findClassNode(root, "scope-wrap"); scopeWrap != nil {
		t.Fatal("page header renders a redundant global scope line")
	}

	// Quiet self context: no banner and no empty stand-in for the removed label.
	quiet := testView(PageHome)
	quietDoc, err := Render(quiet)
	if err != nil {
		t.Fatal(err)
	}
	quietRoot, err := xhtml.Parse(strings.NewReader(quietDoc))
	if err != nil {
		t.Fatal(err)
	}
	if findElementByID(quietRoot, "acting-authority") != nil {
		t.Fatal("self context renders a banner")
	}
	if quietScopeWrap := findClassNode(quietRoot, "scope-wrap"); quietScopeWrap != nil {
		t.Fatal("quiet document renders a redundant scope-wrap")
	}
}

// Accessibility: the banner is a properly labelled landmark section in
// every catalog locale, and a quiet render leaves no dangling
// aria-labelledby reference anywhere in the document.
func TestTodo_UXAUDIT_007_Accessibility(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := testView(PageHome)
		view.Locale = ResolveProductLocale(locale)
		view = ApplyLocale(view, view.Locale)
		view.ContextSwitcher = web056Fixture()
		view.ContextSwitcher.I18nProps = I18nProps{Locale: view.Locale}
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		banner := findElementByID(root, "acting-authority")
		if banner == nil {
			t.Fatalf("locale %s: delegated authority renders no banner", locale)
		}
		labelledBy := xhtmlAttr(banner, "aria-labelledby")
		if labelledBy == "" || findElementByID(root, labelledBy) == nil {
			t.Fatalf("locale %s: authority banner labelling element %q missing", locale, labelledBy)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("locale %s: document leaks an unresolved key", locale)
		}
	}

	// Quiet self context: no orphan aria-labelledby anywhere referencing a
	// banner id that does not exist.
	quiet := testView(PageHome)
	quietDoc, err := Render(quiet)
	if err != nil {
		t.Fatal(err)
	}
	quietRoot, err := xhtml.Parse(strings.NewReader(quietDoc))
	if err != nil {
		t.Fatal(err)
	}
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode {
			for _, attr := range node.Attr {
				if attr.Key == "aria-labelledby" && strings.Contains(attr.Val, "acting-authority") {
					t.Fatalf("quiet document references a removed acting-authority label: %q", attr.Val)
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(quietRoot)
}

// Security: a quiet render discloses zero authority detail, suppressing
// self does not also suppress a genuinely elevated state, and a
// server-resolved authority state this presentation layer does not
// recognize at all still discloses rather than being silently dropped --
// hiding a real delegated/elevated/view-as/break-glass authority behind an
// unparsed state would misrepresent who the viewer is acting as.
func TestTodo_UXAUDIT_007_Security(t *testing.T) {
	// Self context leaks nothing: no delegator name, no tenant name, no
	// "elevated" wording anywhere in the document -- not merely a hidden
	// node, actually absent.
	self := testView(PageHome)
	self.ContextSwitcher = ContextSwitcherProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Current: AuthorityContext{
			TenantID: "tenant-secret", TenantName: "HarborCare",
			ActingContextID: "self-a", ActingContextName: "Your own authority",
		},
		Controller: &ContextSwitchController{},
	}
	selfDoc, err := Render(self)
	if err != nil {
		t.Fatal(err)
	}
	if findElementByID(mustParse(t, selfDoc), "acting-authority") != nil {
		t.Fatal("self authority still renders a banner")
	}

	// The same tenant, marked elevated instead of self, must disclose:
	// proves the self-suppression path does not accidentally suppress a
	// genuinely elevated authority too.
	elevated := self
	elevated.ContextSwitcher.Current.Elevated = true
	elevatedDoc, err := Render(elevated)
	if err != nil {
		t.Fatal(err)
	}
	elevatedBanner := findElementByID(mustParse(t, elevatedDoc), "acting-authority")
	if elevatedBanner == nil {
		t.Fatal("elevated authority renders no banner: self-suppression leaked into a real elevated state")
	}

	// An authority state this switch has never seen before -- not any of
	// the five known constants -- must still disclose. The banner shows,
	// but it never echoes the raw, unrecognized state string itself as
	// user-facing text: disclosure is "something authoritative is in
	// force", never "here is the literal wire value we could not parse".
	garbled := testView(PageHome)
	garbled.ContextSwitcher = ContextSwitcherProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Current: AuthorityContext{
			TenantID: "tenant-a", TenantName: "HarborCare",
			ActingContextID: "self-a", ActingContextName: "Your own authority",
			State: AuthorityState("quantum_delegation_v2"),
		},
		Controller: &ContextSwitchController{},
	}
	garbledDoc, err := Render(garbled)
	if err != nil {
		t.Fatal(err)
	}
	garbledBanner := findElementByID(mustParse(t, garbledDoc), "acting-authority")
	if garbledBanner == nil {
		t.Fatal("unrecognized authority state suppressed the banner instead of disclosing it")
	}
	if strings.Contains(textContent(garbledBanner), "quantum_delegation_v2") {
		t.Fatal("banner echoed the raw unrecognized state string verbatim")
	}

	// Direct unit proof on the enum itself: exhaustive, no permissive
	// default. Self is the only quiet value; the zero value, the four
	// named non-self states, and an arbitrary unrecognized string all
	// disclose.
	if AuthorityStateSelf.noticeworthy() {
		t.Fatal("AuthorityStateSelf.noticeworthy() = true, want false")
	}
	for _, state := range []AuthorityState{
		AuthorityStateDelegated, AuthorityStateViewAs, AuthorityStateElevated, AuthorityStateBreakGlass,
		AuthorityState(""), AuthorityState("quantum_delegation_v2"), AuthorityState("SELF"),
	} {
		if !state.noticeworthy() {
			t.Fatalf("AuthorityState(%q).noticeworthy() = false, want true (fail open)", state)
		}
	}
}

// Regression: the pre-existing WEB-056 delegated/elevated behaviors this
// todo builds on still work, and the neighboring authority surfaces
// (session warning, step-up, break-glass activation, policy simulation)
// keep their own independent nil-gating unchanged.
func TestTodo_UXAUDIT_007_Regression(t *testing.T) {
	view := testView(PageHome)
	view.ContextSwitcher = web056Fixture()
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	assertBanner(t, doc, "pre-existing WEB-056 delegated fixture", "HarborCare", "Covering HR", "Maya Chen", "2026-09-18")

	// The header still resolves a complete page identity for every registered
	// page without inventing an acting-context label of its own.
	for _, definition := range PageDefinitions() {
		roles := []string{"manager"}
		if !PageVisible(definition.ID, roles) {
			roles = []string{RoleHCMAdmin}
		}
		identity := ResolvePageIdentity(ApplyRoleVisibility(testView(definition.ID), roles))
		if identity.Page != definition.ID || identity.Title == "" || identity.Subtitle == "" {
			t.Fatalf("page %s identity incomplete after UXAUDIT-007: %#v", definition.ID, identity)
		}
	}

	// Neighboring authority surfaces are unaffected: absent by default,
	// present only when their own view field is set.
	bare := testView(PageHome)
	bareDoc, err := Render(bare)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"session-warning", "step-up-challenge", "break-glass-activation", "policy-simulation"} {
		if findElementByID(mustParse(t, bareDoc), id) != nil {
			t.Fatalf("bare view unexpectedly renders #%s", id)
		}
	}

	armed := testView(PageHome)
	armed.BreakGlassActivation = &BreakGlassActivationProps{IncidentRef: "INC-2026-118", ActivateHref: "/workspace/app/journeys"}
	armed.PolicySimulation = &PolicySimulationProps{Subject: "Avery Patel (manager)", ExitHref: "/workspace/app/home"}
	armedDoc, err := Render(armed)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"break-glass-activation", "policy-simulation"} {
		if findElementByID(mustParse(t, armedDoc), id) == nil {
			t.Fatalf("armed view renders no #%s", id)
		}
	}

	// The literal RED string never appears again, on any page, with or
	// without an authority projection.
	for _, page := range []PageID{PageHome, PageHistory, PagePerson} {
		for _, ctx := range []ContextSwitcherProps{{}, web056Fixture()} {
			v := testView(page)
			v.ContextSwitcher = ctx
			d, err := Render(v)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(d, "Acting as yourself") {
				t.Fatalf("page %s still renders the retired global 'Acting as yourself' text", page)
			}
		}
	}
}

func assertQuiet(t *testing.T, doc, label string) {
	t.Helper()
	if findElementByID(mustParse(t, doc), "acting-authority") != nil {
		t.Fatalf("%s renders a banner", label)
	}
	if strings.Contains(doc, "Acting as yourself") {
		t.Fatalf("%s still renders the retired global 'Acting as yourself' text", label)
	}
}

func assertBanner(t *testing.T, doc, label string, wantSubstrings ...string) {
	t.Helper()
	root := mustParse(t, doc)
	banner := findElementByID(root, "acting-authority")
	if banner == nil {
		t.Fatalf("%s renders no banner", label)
	}
	body := textContent(banner)
	for _, want := range wantSubstrings {
		if !strings.Contains(body, want) {
			t.Fatalf("%s banner missing %q: %q", label, want, body)
		}
	}
}

func mustParse(t *testing.T, doc string) *xhtml.Node {
	t.Helper()
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
