package productui

import (
	"strings"
	"testing"
)

// uxaudit006BannedVocabulary is the fixed, documented set of internal
// architecture and protocol terms UXAUDIT-006's RED forbids in user-facing
// product copy: a proper backend service or RPC identifier (JourneyService,
// ListWorkers, gRPC), and phrases that name the internal enforcement
// mechanism instead of describing the task in plain language ("a cell", "a
// worker projection", "a credential role fallback", "a server-enforced
// boundary", "tenant appearance"). Matching is case-insensitive and applies
// to the full rendered document -- markup, attributes and text alike --
// because a banned term leaking into an aria-label or a translated sentence
// is exactly as much a defect as one sitting in plain text.
//
// Extend this slice, never a second copy of it, when a live audit finds
// another leak: every renderer and locale in this package is scanned
// against the same list by TestTodo_UXAUDIT_006 and TestTodo_UXAUDIT_006_I18N.
var uxaudit006BannedVocabulary = []string{
	"JourneyService",
	"ListWorkers",
	"gRPC",
	"authenticated cell",
	"live cell",
	"server-enforced boundary",
	"tenant appearance",
	"credential role fallback",
	"worker projection",
	"governed journey service",
}

// uxaudit006ScanVocabulary returns every banned term present in doc,
// case-insensitively. An empty result means the document is clean; it does
// not mean the document was inspected -- callers that also need to rule out
// a silently empty render should check that independently (see the "zero
// value" assertions in TestTodo_UXAUDIT_006), since an empty string trivially
// contains none of these terms without proving anything.
func uxaudit006ScanVocabulary(doc string) []string {
	lower := strings.ToLower(doc)
	var found []string
	for _, term := range uxaudit006BannedVocabulary {
		if strings.Contains(lower, strings.ToLower(term)) {
			found = append(found, term)
		}
	}
	return found
}

// PRIMARY: the vocabulary guard itself. RED (verbatim from the 2026-09-12
// live audit): user-facing copy included JourneyService, canonical gRPC
// service, authenticated cell, server-enforced boundary, tenant appearance,
// credential role fallback and worker projection language -- in the People
// and Organization subtitles, the organization metadata boundary notice, the
// organization empty state, the roles-assignment help text, the
// organization-visibility policy explanation, the appearance page's tenant
// badge, the Admin capability cards and the shell's own footer and loading
// copy. GREEN: every registered page, rendered in every supported locale,
// is free of that vocabulary. This is deliberately a scan of rendered output
// against a named banned list rather than a set of assertions pinned to the
// specific strings that happened to be wrong on audit day: the next
// regression -- a new page, a new empty state, a copy-pasted description --
// is caught the same way this one was, without anyone having to remember to
// add a bespoke assertion for it.
func TestTodo_UXAUDIT_006(t *testing.T) {
	// The guard must not be vacuous: prove it actually catches a
	// deliberately seeded banned term before trusting it to clear every
	// page below.
	seeded := `<p>Promotion journeys are loaded through the canonical gRPC service by JourneyService.</p>`
	if found := uxaudit006ScanVocabulary(seeded); len(found) == 0 {
		t.Fatal("vocabulary guard did not catch a deliberately seeded banned term; the guard is vacuous")
	}
	if found := uxaudit006ScanVocabulary(""); len(found) != 0 {
		t.Fatalf("vocabulary guard reported findings %v against an empty document", found)
	}

	for _, definition := range PageDefinitions() {
		definition := definition
		for _, code := range SupportedProductLocales() {
			code := code
			t.Run(string(definition.ID)+"/"+code, func(t *testing.T) {
				view := ApplyLocale(testView(definition.ID), ResolveProductLocale(code))
				doc, err := Render(view)
				if err != nil {
					t.Fatal(err)
				}
				// A page that rendered nothing would trivially "pass" the
				// scan below without proving anything; a real page always
				// carries the shell chrome and an unresolved key would show
				// up as a literal "⟦key⟧" marker rather than a silent empty
				// string, so guard against both zero-value escapes.
				if strings.TrimSpace(doc) == "" {
					t.Fatal("page rendered an empty document")
				}
				if strings.Contains(doc, "⟦") {
					t.Fatal("page exposes an unresolved message key")
				}
				if found := uxaudit006ScanVocabulary(doc); len(found) > 0 {
					t.Errorf("%s/%s: banned implementation vocabulary present: %v", definition.ID, code, found)
				}
			})
		}
	}

	// The Admin home's capability cards render conditionally on the
	// authorization projection; sweep it again for every admitted role so a
	// role-scoped subset of cards cannot hide a leak from the unrestricted
	// preview pass above.
	for _, roles := range [][]string{{RoleHCMAdmin}, {"manager"}, {"worker_self"}, nil} {
		for _, code := range SupportedProductLocales() {
			view := ApplyLocale(ApplyRoleVisibility(testView(PageAdmin), roles), ResolveProductLocale(code))
			doc, err := Render(view)
			if err != nil {
				t.Fatal(err)
			}
			if found := uxaudit006ScanVocabulary(doc); len(found) > 0 {
				t.Errorf("admin home (%v/%s): banned implementation vocabulary present: %v", roles, code, found)
			}
		}
	}
}

// Golden: pins the exact bytes of the copy this todo rewrote. A future edit
// that reworks one of these keys will fail here first, with a byte-for-byte
// diff, rather than only surfacing as a vocabulary-guard miss once the
// wording drifts back toward jargon.
func TestTodo_UXAUDIT_006_Golden(t *testing.T) {
	cases := []struct{ locale, key, want string }{
		{"en-US", "shell.authenticated_scope", "Workspace access"},
		{"en-US", "shell.loading_authorized", "Loading your workspace data…"},
		{"en-US", "page.people.subtitle", "People you're authorized to view across the organization."},
		{"en-US", "page.organization.subtitle", "Review the organization visible to you."},
		{"en-US", "organization.metadata_boundary", "Counts reflect the people visible in your current access; legal-entity details and reporting lines are not inferred."},
		{"en-US", "organization.empty_title", "No organization members to show"},
		{"en-US", "organization.empty_description", "No employees are visible to you in this organization right now. If you expect to see people here, ask your administrator to check your access."},
		{"en-US", "admin.journeys_unavailable_reason", "Journey actions aren't available right now because the connection didn't respond. Try refreshing the page."},
		{"en-US", "admin.studio_unavailable_reason", "Page configuration isn't available for your organization yet."},
		{"en-US", "admin.hero_eyebrow", "Live"},
		{"en-US", "admin.hero_description", "This page only shows the capabilities available to your organization right now."},
		{"en-US", "admin.journey_card_description", "Promotion journeys and the workers you can see are loaded live from your organization's data."},
		{"en-US", "admin.studio_card_description", "Experience configuration isn't available for your organization yet."},
		{"en-US", "roles.assignments_help", "Assigning a role here overrides the default role used for organization visibility."},
		{"en-US", "organization_visibility.boundary_title", "Applied automatically"},
		{"en-US", "organization_visibility.boundary_detail", "Role grants are additive. Hidden workers, unit names, and reporting links are removed before worker records reach the browser; a worker can always receive their own record."},
		{"en-US", "appearance.tenant", "Organization appearance"},
		{"en-US", "insights.attention_description", "This summary covers promotion journeys you can view. Broader workforce reporting is not available yet."},
		{"de-DE", "shell.authenticated_scope", "Arbeitsbereichszugriff"},
		{"de-DE", "appearance.tenant", "Unternehmensweite Darstellung"},
		{"de-DE", "organization_visibility.boundary_title", "Automatisch angewendet"},
		{"ar", "organization_visibility.boundary_title", "يُطبَّق تلقائياً"},
		{"ar", "admin.studio_unavailable_reason", "تصميم الصفحات المخصصة غير متاح بعد."},
	}
	for _, c := range cases {
		got := ResolveProductLocale(c.locale).Text(c.key)
		if got != c.want {
			t.Errorf("%s/%s = %q, want %q", c.locale, c.key, got, c.want)
		}
	}
}

// Browser: the corrected copy lands in the DOM nodes a browser actually
// paints, not merely somewhere in the response body -- the footer's live-
// source span, the Admin hero's eyebrow and description, and the Settings
// page's data-source fact. This is a static-render DOM assertion against
// Go's own SSR output; it does not launch a browser, drive Playwright or
// touch the running dev server (out of this lane's roots -- see the report
// for what live-server verification remains outstanding).
func TestTodo_UXAUDIT_006_Browser(t *testing.T) {
	homeDoc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	root := mustParse(t, homeDoc)
	footer := findFirst(root, "footer")
	if footer == nil {
		t.Fatal("document renders no footer")
	}
	footerText := textContent(footer)
	if !strings.Contains(footerText, "Workspace information") {
		t.Fatalf("footer text = %q, want a non-fabricated workspace label", footerText)
	}
	if found := uxaudit006ScanVocabulary(footerText); len(found) > 0 {
		t.Fatalf("footer still carries banned vocabulary: %v", found)
	}

	adminDoc, err := Render(ApplyRoleVisibility(testView(PageAdmin), []string{RoleHCMAdmin}))
	if err != nil {
		t.Fatal(err)
	}
	adminRoot := mustParse(t, adminDoc)
	journeyCard := findCardByTitle(adminRoot, "Promotion workflows")
	if journeyCard == nil {
		t.Fatal("admin home loses the journey card")
	}
	journeyCardText := textContent(journeyCard)
	if !strings.Contains(journeyCardText, "Start, review, and track promotion requests") {
		t.Fatalf("journey card text = %q, want the corrected task-language description", journeyCardText)
	}
	if found := uxaudit006ScanVocabulary(journeyCardText); len(found) > 0 {
		t.Fatalf("journey card still carries banned vocabulary: %v", found)
	}
	if !strings.Contains(adminDoc, "<small>Live</small>") {
		t.Fatal("admin hero eyebrow did not resolve to the corrected \"Live\" label")
	}

	settingsDoc, err := Render(testView(PageSettings))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(settingsDoc, "WORKFORCE_DIRECTORY") {
		t.Fatal("settings page data-source fact exposes the raw backend identifier")
	}
	if found := uxaudit006ScanVocabulary(settingsDoc); len(found) > 0 {
		t.Fatalf("settings page still carries banned vocabulary: %v", found)
	}
}

// I18N: de-DE and ar copy for the fixed keys is genuinely different
// translated text, not a duplicate of English wearing a different locale
// tag, and the vocabulary guard itself is locale-blind -- it must catch a
// banned identifier equally in an English, German or Arabic sentence.
func TestTodo_UXAUDIT_006_I18N(t *testing.T) {
	// Keys with a real, distinct translation in all three catalog locales:
	// every locale must differ from every other, and none may reintroduce
	// the vocabulary the English original was fixed for.
	threeWay := []string{
		"shell.loading_authorized",
		"organization.metadata_boundary",
		"organization.empty_description",
		"admin.journeys_unavailable_reason",
		"admin.studio_unavailable_reason",
		"organization_visibility.boundary_title",
		"organization_visibility.boundary_detail",
	}
	for _, key := range threeWay {
		en := ResolveProductLocale("en-US").Text(key)
		de := ResolveProductLocale("de-DE").Text(key)
		ar := ResolveProductLocale("ar").Text(key)
		if en == de || en == ar || de == ar {
			t.Errorf("%s: locales are not genuinely distinct: en-US=%q de-DE=%q ar=%q", key, en, de, ar)
		}
		for locale, text := range map[string]string{"en-US": en, "de-DE": de, "ar": ar} {
			if found := uxaudit006ScanVocabulary(text); len(found) > 0 {
				t.Errorf("%s/%s: translated copy still carries banned vocabulary %v: %q", locale, key, found, text)
			}
		}
	}

	// Keys translated in en-US and de-DE only (ar intentionally falls back
	// to the reviewed English per this catalog's partial-rollout design);
	// still require the two maintained locales to be genuinely distinct.
	twoWay := []string{"appearance.tenant", "page.people.subtitle", "shell.authenticated_scope"}
	for _, key := range twoWay {
		en := ResolveProductLocale("en-US").Text(key)
		de := ResolveProductLocale("de-DE").Text(key)
		if en == de {
			t.Errorf("%s: en-US and de-DE are not distinct: %q", key, en)
		}
	}

	// The guard applies uniformly to every locale: render the pages most
	// loaded with the fixed vocabulary in all three and confirm none
	// carries a banned term, proving a German or Arabic sentence containing
	// an English service identifier would still be caught.
	for _, id := range []PageID{PageOrganization, PageAdmin, PagePeople, PageRoles, PageAppearance, PageInsights} {
		for _, code := range SupportedProductLocales() {
			view := ApplyLocale(testView(id), ResolveProductLocale(code))
			doc, err := Render(view)
			if err != nil {
				t.Fatalf("%s/%s: %v", id, code, err)
			}
			if found := uxaudit006ScanVocabulary(doc); len(found) > 0 {
				t.Errorf("%s/%s: banned vocabulary %v", id, code, found)
			}
		}
	}
}

// Regression: this todo reworded copy in the header identity fallback, the
// Admin capability cards and the Insights attention panel, all of which sit
// directly on top of UXAUDIT-007's acting-context work and WEB-229's
// authorization-resolved Admin home. Neither must break.
func TestTodo_UXAUDIT_006_Regression(t *testing.T) {
	// UXAUDIT-007: ordinary self context on an unrelated page stays quiet.
	// The reworded authenticated-scope fallback text must not resurrect the
	// retired global "Acting as yourself" label or the acting-authority
	// banner for plain self context.
	quiet := testView(PageHome)
	quietDoc, err := Render(quiet)
	if err != nil {
		t.Fatal(err)
	}
	assertQuiet(t, quietDoc, "ordinary self context after the UXAUDIT-006 wording changes")

	// testView sets a non-empty Scope ("manager"), so exercise the header's
	// authenticated-scope fallback -- ResolvePageIdentity's substitute for an
	// honestly empty Scope -- directly with the empty-scope case it covers.
	noScope := NewView(PageHome, "tenant-test", "Taylor", "")
	noScopeDoc, err := Render(noScope)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(noScopeDoc, `id="page-title"`) {
		t.Fatal("ordinary page lost its page identity")
	}
	if strings.Contains(noScopeDoc, "Authenticated scope") {
		t.Fatal("ordinary page still renders the retired \"Authenticated scope\" label")
	}

	// UXAUDIT-007: delegated authority still discloses its banner with
	// tenant and delegator -- the wording fix touched only the header's own
	// fallback text, never the acting-context component.
	delegated := testView(PageHome)
	delegated.ContextSwitcher = web056Fixture()
	delegatedDoc, err := Render(delegated)
	if err != nil {
		t.Fatal(err)
	}
	assertBanner(t, delegatedDoc, "delegated authority after the UXAUDIT-006 wording changes", "HarborCare", "Maya Chen")

	// WEB-229: the Admin home's authorization-resolved card set is
	// unchanged by moving its descriptions into the locale catalog --
	// still every card for the platform admin, still an honest empty
	// message registry with no unresolved key.
	adminDoc, err := Render(ApplyRoleVisibility(testView(PageAdmin), []string{RoleHCMAdmin}))
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range []string{"Roles &amp; access", "Organization visibility", "Worker ID rules", "Brand &amp; appearance", "Promotion workflows", "Experience configuration"} {
		if !strings.Contains(adminDoc, card) {
			t.Fatalf("admin home lost capability card %q after the vocabulary fix", card)
		}
	}
	if strings.Contains(adminDoc, "⟦") {
		t.Fatal("admin home exposes an unresolved message key after the vocabulary fix")
	}

	// The Organization page's honest empty state (business metadata block
	// plus its now-actionable empty message) survives the copy rewording.
	emptyOrgDoc, err := Render(NewView(PageOrganization, "tenant-empty", "manager", "self"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(emptyOrgDoc, "Business metadata") || !strings.Contains(emptyOrgDoc, "No organization members to show") {
		t.Fatal("empty organization view lost its business metadata or empty state after the vocabulary fix")
	}
	if !strings.Contains(emptyOrgDoc, "ask your administrator") {
		t.Fatal("organization empty state lost its next-step guidance")
	}
}
