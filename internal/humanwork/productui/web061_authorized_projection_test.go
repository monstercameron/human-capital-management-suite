package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// RED for WEB-061: the authorized presentation projection. Pages must
// render the server's record- and field-level authorization verdicts —
// never raw values, never their own policy calls. The projector maps one
// display vocabulary (allow, redacted, denied, withheld) to localized
// presentation: allowed values render (blank reads as not-reported),
// redacted reads as redacted, denied as unavailable with the server's
// reason, withheld — and anything unknown — as withheld. A missing
// verdict or a non-disclosable subject projects to uniform withholding:
// no values, reason only. Unknown effects fail closed; inputs are never
// mutated.
func TestTodo_WEB_061(t *testing.T) {
	labels := map[string]map[string]string{
		"en-US": {"notReported": "Not reported", "redacted": "Redacted", "unavailable": "Unavailable", "withheld": "Withheld"},
		"de-DE": {"notReported": "Nicht gemeldet", "redacted": "Geschwärzt", "unavailable": "Nicht verfügbar", "withheld": "Zurückgehalten"},
		"ar":    {"notReported": "غير مذكور", "redacted": "منقّح", "unavailable": "غير متاح", "withheld": "محجوب"},
	}
	for locale, want := range labels {
		lc := ResolveProductLocale(locale)
		for _, value := range []struct {
			name     string
			field    AuthorizedField
			text     string
			wantText string
			wantWhy  string
		}{
			{"allow", AuthorizedField{Effect: PresentationAllow}, "Avery Patel", "Avery Patel", ""},
			{"allow-blank", AuthorizedField{Effect: PresentationAllow}, "", want["notReported"], ""},
			{"redacted", AuthorizedField{Effect: PresentationRedact}, "118000", want["redacted"], ""},
			{"redacted-blank", AuthorizedField{Effect: PresentationRedact}, "", want["redacted"], ""},
			{"denied", AuthorizedField{Effect: PresentationDenied, Reason: "Not for this purpose."}, "118000", want["unavailable"], "Not for this purpose."},
			{"denied-silent", AuthorizedField{Effect: PresentationDenied}, "118000", want["unavailable"], ""},
			{"withheld", AuthorizedField{Effect: PresentationWithheld, Reason: "Subject withheld."}, "118000", want["withheld"], "Subject withheld."},
			{"unknown-empty", AuthorizedField{}, "118000", want["withheld"], ""},
			{"unknown-garbage", AuthorizedField{Effect: PresentationEffect("superuser")}, "118000", want["withheld"], ""},
		} {
			got := ProjectAuthorizedValue(lc, value.text, value.field)
			if got.Text != value.wantText || got.Reason != value.wantWhy {
				t.Fatalf("%s %s = (%q, %q), want (%q, %q)", locale, value.name, got.Text, got.Reason, value.wantText, value.wantWhy)
			}
		}
	}

	lc := ResolveProductLocale("en-US")
	verdict := &AuthorizedRecord{
		ID: "worker-avery", Disclosable: true,
		Fields: map[string]AuthorizedField{
			"name":   {Effect: PresentationAllow},
			"salary": {Effect: PresentationDenied, Reason: "Not for this purpose."},
			"bonus":  {Effect: PresentationRedact},
		},
	}
	record := ProjectAuthorizedRecord(lc, "worker-avery",
		map[string]string{"name": "Avery Patel", "salary": "118000", "bonus": "12%", "tenure": "4y"}, verdict)
	if record.Withheld {
		t.Fatal("disclosable record projects as withheld")
	}
	if got := record.Values["name"].Text; got != "Avery Patel" {
		t.Fatalf("allowed field projects as %q", got)
	}
	if got := record.Values["salary"]; got.Text != "Unavailable" || got.Reason != "Not for this purpose." {
		t.Fatalf("denied field projects as %#v", got)
	}
	if got := record.Values["bonus"].Text; got != "Redacted" {
		t.Fatalf("redacted field projects as %q", got)
	}
	// A field without a verdict fails closed; it never leaks the value.
	if got := record.Values["tenure"]; got.Text != "Withheld" {
		t.Fatalf("verdict-less field projects as %q, want uniform withholding", got.Text)
	}

	// No verdict, or a non-disclosable subject, means uniform withholding:
	// reason only, never values.
	for name, record := range map[string]ProjectedRecord{
		"nil-verdict": ProjectAuthorizedRecord(lc, "ghost", map[string]string{"name": "Ghost"}, nil),
		"undisclosable": ProjectAuthorizedRecord(lc, "worker-x", map[string]string{"name": "X"},
			&AuthorizedRecord{ID: "worker-x", Disclosable: false, DenialReason: "No record access."}),
		"undisclosable-silent": ProjectAuthorizedRecord(lc, "worker-x", map[string]string{"name": "X"},
			&AuthorizedRecord{ID: "worker-x", Disclosable: false}),
	} {
		if !record.Withheld {
			t.Fatalf("%s projects as disclosable", name)
		}
		if len(record.Values) != 0 {
			t.Fatalf("%s leaks values: %#v", name, record.Values)
		}
	}
	if got := ProjectAuthorizedRecord(lc, "x", nil, nil); !got.Withheld || got.Reason != "Unavailable" {
		t.Fatalf("silent nil verdict = %#v, want uniform withholding with fallback reason", got)
	}
}

// Golden: the projection matrix digest (effect × locale → text).
func TestTodo_WEB_061_Golden(t *testing.T) {
	var builder strings.Builder
	for _, effect := range []PresentationEffect{PresentationAllow, PresentationRedact, PresentationDenied, PresentationWithheld, ""} {
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			projected := ProjectAuthorizedValue(ResolveProductLocale(locale), "118000", AuthorizedField{Effect: effect})
			builder.WriteString(string(effect))
			builder.WriteString("\x00")
			builder.WriteString(locale)
			builder.WriteString("\x00")
			builder.WriteString(projected.Text)
			builder.WriteString("\n")
		}
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "068f869625028e787de42d947b82bd1f6fc89d1cf8592a8056368c12b5ee5ca1"
	if got != want {
		t.Fatalf("projection matrix digest = %s, want %s", got, want)
	}
}

// Browser: server evidence identifiers render byte-identically in every
// catalog locale — presentation never localizes, mangles, or drops the
// identifiers the authorization story is audited by.
func TestTodo_WEB_061_Browser(t *testing.T) {
	view := testView(PageHome)
	view.ContextSwitcher = web056Fixture()
	view.BreakGlassActivation = &BreakGlassActivationProps{IncidentRef: "INC-2026-118", ReasonDetail: "Blocked.", Capabilities: []string{"payroll.commit"}, TTLDetail: "60 minutes.", ActivateHref: "/workspace/app/journeys?breakglass=INC-2026-118"}
	view.PolicySimulation = &PolicySimulationProps{Subject: "Avery Patel (manager)", Allowed: false, Rules: []string{"compensation.withhold", "tenure.read"}, Versions: []string{"policy-2026-09-01"}, EvidenceRef: "ev:authz:9f2c", ExitHref: "/workspace/app/home"}
	evidence := []string{"HarborCare", "Maya Chen", "INC-2026-118", "payroll.commit", "compensation.withhold", "policy-2026-09-01", "ev:authz:9f2c"}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		local := view
		local.Locale = ResolveProductLocale(locale)
		local = ApplyLocale(local, local.Locale)
		doc, err := Render(local)
		if err != nil {
			t.Fatal(err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		body := textContent(root)
		for _, identifier := range evidence {
			if !strings.Contains(body, identifier) {
				t.Fatalf("%s document corrupts evidence identifier %q", locale, identifier)
			}
		}
	}
}

// Conformance: withheld copy in three locales, determinism, immutability,
// nil field maps.
func TestTodo_WEB_061_Conformance(t *testing.T) {
	for locale, withheld := range map[string]string{"en-US": "Withheld", "de-DE": "Zurückgehalten", "ar": "محجوب"} {
		lc := ResolveProductLocale(locale)
		if got := ProjectAuthorizedValue(lc, "x", AuthorizedField{Effect: PresentationWithheld}).Text; got != withheld {
			t.Fatalf("%s withheld reads %q, want %q", locale, got, withheld)
		}
		if got := lc.Text("provenance.value.withheld"); got != withheld {
			t.Fatalf("%s withheld key reads %q, want %q", locale, got, withheld)
		}
		if key := lc.Text("provenance.value.withheld"); strings.Contains(key, "⟦") {
			t.Fatalf("%s withheld key unresolved: %q", locale, key)
		}
	}
	lc := ResolveProductLocale("en-US")
	values := map[string]string{"name": "Avery Patel"}
	verdict := &AuthorizedRecord{ID: "w", Disclosable: true, Fields: map[string]AuthorizedField{"name": {Effect: PresentationAllow}}}
	first := ProjectAuthorizedRecord(lc, "w", values, verdict)
	second := ProjectAuthorizedRecord(lc, "w", values, verdict)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("projection is nondeterministic:\n%#v\n%#v", first, second)
	}
	if !reflect.DeepEqual(values, map[string]string{"name": "Avery Patel"}) || !reflect.DeepEqual(verdict.Fields, map[string]AuthorizedField{"name": {Effect: PresentationAllow}}) {
		t.Fatal("projection mutates its inputs")
	}
	bare := ProjectAuthorizedRecord(lc, "w", map[string]string{"name": "X"}, &AuthorizedRecord{ID: "w", Disclosable: true})
	if got := bare.Values["name"].Text; got != "Withheld" {
		t.Fatalf("nil field map projects as %q, want uniform withholding", got)
	}
}
