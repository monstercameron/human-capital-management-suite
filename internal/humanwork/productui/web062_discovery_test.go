package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// RED for WEB-062: resource discoverability decisions. Discovery surfaces
// must enforce the record verdicts from WEB-061: when the server sends
// verdicts, undisclosable records — and records without a verdict —
// never reach search items, and admitted records advertise projected
// labels, never raw values. When the server is silent (no verdicts) the
// current behavior stands: presentation enforces the authorization it is
// given instead of inventing its own.
func TestTodo_WEB_062(t *testing.T) {
	lc := ResolveProductLocale("en-US")
	verdicts := map[string]AuthorizedRecord{
		"worker-avery": {ID: "worker-avery", Disclosable: true, Fields: map[string]AuthorizedField{
			"name": {Effect: PresentationAllow}, "role": {Effect: PresentationDenied, Reason: "Not for this purpose."},
		}},
		"worker-jordan": {ID: "worker-jordan", Disclosable: false, DenialReason: "No record access."},
	}
	for _, admission := range []struct {
		id    string
		allow bool
	}{
		{"worker-avery", true}, {"worker-jordan", false}, {"worker-ghost", false},
	} {
		if got := DiscoveryAdmitted(admission.id, verdicts); got != admission.allow {
			t.Fatalf("DiscoveryAdmitted(%q) = %v, want %v", admission.id, got, admission.allow)
		}
		if got := DiscoveryAdmitted(admission.id, nil); !got {
			t.Fatalf("DiscoveryAdmitted(%q, nil) = false, want silent-server passthrough", admission.id)
		}
		if got := DiscoveryAdmitted(admission.id, map[string]AuthorizedRecord{}); !got {
			t.Fatalf("DiscoveryAdmitted(%q, empty) = false, want silent-server passthrough", admission.id)
		}
	}
	if got := DiscoveryLabel(lc, "worker-avery", "Avery Patel", "name", verdicts); got != "Avery Patel" {
		t.Fatalf("allowed label projects as %q", got)
	}
	if got := DiscoveryLabel(lc, "worker-avery", "DES2", "role", verdicts); got != "Unavailable" {
		t.Fatalf("denied label projects as %q", got)
	}
	if got := DiscoveryLabel(lc, "worker-avery", "Toronto", "location", verdicts); got != "Withheld" {
		t.Fatalf("verdict-less label projects as %q, want withholding", got)
	}
	if got := DiscoveryLabel(lc, "worker-avery", "Avery Patel", "name", nil); got != "Avery Patel" {
		t.Fatalf("silent-server label projects as %q", got)
	}

	// Search items enforce the verdicts: Jordan's person and promotion
	// entries vanish, Avery's survive with projected labels.
	view := testView(PageHome)
	view.RecordVerdicts = verdicts
	items := globalSearchItems(view)
	ids := map[string]string{}
	for _, item := range items {
		ids[item.ID] = item.Label
	}
	for _, forbidden := range []string{"person:worker-jordan", "action:promotion:worker-jordan"} {
		if label, ok := ids[forbidden]; ok {
			t.Fatalf("undisclosable record advertised as %q (%q)", forbidden, label)
		}
	}
	if _, ok := ids["person:worker-avery"]; !ok {
		t.Fatal("disclosable record missing from search items")
	}
	if hasGlobalSearchKind(items, "action") {
		t.Fatal("global search duplicated an executable action owned by the quick launcher")
	}
	for id, label := range ids {
		if strings.Contains(label, "Jordan Lee") {
			t.Fatalf("undisclosable name leaks through %q (%q)", id, label)
		}
	}

	// A silent server keeps the current discovery set byte-for-byte.
	plain := globalSearchItems(testView(PageHome))
	var want []string
	for _, item := range plain {
		want = append(want, item.ID)
	}
	unfiltered := testView(PageHome)
	unfiltered.RecordVerdicts = nil
	var got []string
	for _, item := range globalSearchItems(unfiltered) {
		got = append(got, item.ID)
	}
	if strings.Join(want, "\x00") != strings.Join(got, "\x00") {
		t.Fatal("silent-server discovery set changed")
	}
}

// Golden: the discovery matrix digest (record × verdict state → label).
func TestTodo_WEB_062_Golden(t *testing.T) {
	verdicts := map[string]AuthorizedRecord{
		"worker-avery": {ID: "worker-avery", Disclosable: true, Fields: map[string]AuthorizedField{
			"name": {Effect: PresentationAllow}, "role": {Effect: PresentationDenied},
		}},
		"worker-jordan": {ID: "worker-jordan", Disclosable: false},
	}
	var builder strings.Builder
	lc := ResolveProductLocale("en-US")
	for _, id := range []string{"worker-avery", "worker-jordan", "worker-ghost"} {
		builder.WriteString(id)
		builder.WriteString("\x00")
		if DiscoveryAdmitted(id, verdicts) {
			builder.WriteString("admit\x00")
			builder.WriteString(DiscoveryLabel(lc, id, "Avery Patel", "name", verdicts))
		} else {
			builder.WriteString("exclude")
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "2e49b791ad5606c5e887808044c0ac4a007ca6a8dcd04815106d6f1e70d7fa8e"
	if got != want {
		t.Fatalf("discovery matrix digest = %s, want %s", got, want)
	}
}

// Browser: the verdict-armed discovery pipeline end to end — searching the
// undisclosable person's name finds no person or promotion hit, while the
// disclosable colleague stays discoverable; armed verdicts never break the
// rendered shell in any locale.
func TestTodo_WEB_062_Browser(t *testing.T) {
	view := testView(PageHome)
	view.RecordVerdicts = map[string]AuthorizedRecord{
		"worker-avery": {ID: "worker-avery", Disclosable: true, Fields: map[string]AuthorizedField{
			"name": {Effect: PresentationAllow},
		}},
		"worker-jordan": {ID: "worker-jordan", Disclosable: false, DenialReason: "No record access."},
	}
	items := globalSearchItems(view)
	for _, query := range []string{"Jordan", "Jordan Lee", "worker-jordan"} {
		for _, hit := range SearchGlobalItems(items, query, globalSearchLimit) {
			if strings.HasPrefix(hit.ID, "person:worker-jordan") || strings.HasPrefix(hit.ID, "action:promotion:worker-jordan") {
				t.Fatalf("query %q discovers undisclosable record %q", query, hit.ID)
			}
		}
	}
	found := false
	for _, hit := range SearchGlobalItems(items, "Avery", globalSearchLimit) {
		if hit.ID == "person:worker-avery" {
			found = true
		}
	}
	if !found {
		t.Fatal("query \"Avery\" loses the disclosable record")
	}
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
		if findElementByID(root, "main-content") == nil {
			t.Fatalf("%s armed render loses main content", locale)
		}
	}
}

// Conformance: localized denial labels, verdict-map immutability.
func TestTodo_WEB_062_Conformance(t *testing.T) {
	for locale, unavailable := range map[string]string{"en-US": "Unavailable", "de-DE": "Nicht verfügbar", "ar": "غير متاح"} {
		lc := ResolveProductLocale(locale)
		verdicts := map[string]AuthorizedRecord{
			"w": {ID: "w", Disclosable: true, Fields: map[string]AuthorizedField{"name": {Effect: PresentationDenied}}},
		}
		if got := DiscoveryLabel(lc, "w", "X", "name", verdicts); got != unavailable {
			t.Fatalf("%s denied label = %q, want %q", locale, got, unavailable)
		}
	}
	verdicts := map[string]AuthorizedRecord{
		"worker-avery": {ID: "worker-avery", Disclosable: true, Fields: map[string]AuthorizedField{"name": {Effect: PresentationAllow}}},
	}
	_ = DiscoveryAdmitted("worker-avery", verdicts)
	_ = DiscoveryLabel(ResolveProductLocale("en-US"), "worker-avery", "Avery Patel", "name", verdicts)
	if len(verdicts) != 1 || verdicts["worker-avery"].Fields["name"].Effect != PresentationAllow {
		t.Fatal("discovery projection mutates its verdicts")
	}
	if got := DiscoveryAdmitted("x", nil); !got {
		t.Fatal("nil verdicts map does not pass through")
	}
}
