package productui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	evidencev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/evidence/v1"
	lineage "github.com/monstercameron/human-capital-management-suite/internal/data/provenance"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func canonicalProvenanceFixture() ProvenanceProjection {
	recorded := time.Date(2026, 9, 6, 12, 30, 0, 0, time.UTC)
	projection := ProjectAuthorizedCanonicalProvenance(
		&evidencev1.SourceAuthority{
			Kind:      evidencev1.AuthorityKind_AUTHORITY_KIND_LOCAL_AUTHORITATIVE,
			System:    "Workforce service",
			PolicyRef: "policy:protected-authority-ref",
		},
		&evidencev1.Provenance{
			Source:      "Journey ledger",
			EvidenceRef: "evidence:protected-record-ref",
			RecordedAt:  timestamppb.New(recorded),
		},
		lineage.StatusPartial,
	)
	projection.SourceVersion = values.Value("v17")
	projection.EffectiveAt = values.Value(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	return projection
}

func provenanceFixture(locale string, projection ProvenanceProjection) (string, error) {
	return ui.RenderToString(ui.CreateElement(ProvenancePresentation, ProvenancePresentationProps{
		I18nProps:  I18nProps{Locale: ResolveProductLocale(locale)},
		IDSeed:     "intent-42-provenance",
		Projection: projection,
	}))
}

// TestTodo_WEB_022 proves that the grammar projects canonical source authority
// and lineage status without inventing an evidence or permission vocabulary.
func TestTodo_WEB_022(t *testing.T) {
	markup, err := provenanceFixture("en-US", canonicalProvenanceFixture())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<section`, `aria-labelledby="` + stableProvenanceID("intent-42-provenance") + `-heading"`, `<dl class="provenance-item-list">`, `<dt class="provenance-item-name">`, `<dd class="provenance-item-value">`,
		`data-provenance-kind="source-authority"`, `data-provenance-kind="authority-system"`, `data-provenance-kind="evidence-source"`, `data-provenance-kind="lineage-completeness"`,
		`Source authority`, `Local authoritative source`, `Authority system`, `Workforce service`, `Evidence source`, `Journey ledger`, `Lineage completeness`, `Partial lineage`,
		`Source version`, `v17`, `Effective at`, `1 Sep 2026 · 00:00 UTC`, `Recorded at`, `6 Sep 2026 · 12:30 UTC`, `datetime="2026-09-06T12:30:00Z"`,
		`does not grant access, permission, or action authority`, `aria-hidden="true"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("provenance grammar missing %q in %s", want, markup)
		}
	}
	if strings.Count(markup, `data-provenance-kind=`) != 7 || strings.Contains(markup, "intent-42-provenance") {
		t.Fatalf("provenance facts were flattened or caller identifier leaked: %s", markup)
	}
	for _, forbidden := range []string{"protected-authority-ref", "protected-record-ref", "PERMITTED_WRITER", "PERMITTED_PROPOSER", "DIRECT", "SUPPORTING"} {
		if strings.Contains(markup, forbidden) {
			t.Fatalf("invented or protected provenance vocabulary leaked %q: %s", forbidden, markup)
		}
	}
}

func TestTodo_WEB_022_Golden(t *testing.T) {
	first, err := provenanceFixture("en-US", canonicalProvenanceFixture())
	if err != nil {
		t.Fatal(err)
	}
	second, err := provenanceFixture("en-US", canonicalProvenanceFixture())
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("provenance presentation is not deterministic")
	}
	wantID := `id="` + stableProvenanceID("intent-42-provenance") + `"`
	if strings.Count(first, wantID) != 1 || strings.Count(first, `<div class="provenance-item `) != 7 {
		t.Fatalf("stable provenance identity or seven independent facts missing from %s", first)
	}
}

func TestTodo_WEB_022_Browser(t *testing.T) {
	withoutProvenance, err := Render(testView(PageWork))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(withoutProvenance, `class="provenance-grammar"`) {
		t.Fatal("an unbound projection rendered a misleading provenance panel")
	}

	view := testView(PageWork)
	view.Work[0].Provenance = canonicalProvenanceFixture()
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-provenance-kind="source-authority"`, `data-provenance-kind="lineage-completeness"`,
		`.provenance-item-list{grid-template-columns:repeat(4,minmax(0,1fr))`, `@media (max-width:900px)`, `@media (max-width:760px)`,
		`@media (prefers-reduced-motion:reduce)`, `@media (forced-colors:active)`, `@media (print)`,
		`CanvasText`, `var(--hcm-color-info`, `var(--hcm-color-warning`, `.work-preview>.provenance-grammar{grid-column:1 / -1;}`,
		`overflow-wrap:anywhere;}`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("production document missing provenance browser contract %q", want)
		}
	}
	for _, forbidden := range []string{"text-overflow:ellipsis", ".provenance-item-value{overflow:hidden", ".provenance-item{color:#"} {
		if strings.Contains(provenancePresentationStylesStylesheet(), forbidden) {
			t.Fatalf("provenance CSS truncates or bypasses semantic colors with %q", forbidden)
		}
	}
}

func TestTodo_WEB_022_Conformance(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup, err := provenanceFixture(locale, canonicalProvenanceFixture())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(markup, "provenance.") || strings.Contains(markup, "⟦") || strings.Count(markup, `data-provenance-kind=`) != 7 {
			t.Fatalf("%s leaked a key or lost a provenance fact: %s", locale, markup)
		}
	}

	longLabel := strings.Repeat("Long authorized evidence source label ", 12)
	projection := canonicalProvenanceFixture()
	projection.EvidenceSource = values.Value(longLabel)
	markup, err := provenanceFixture("de-DE", projection)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, strings.TrimSpace(longLabel)) {
		t.Fatal("a long authorized source label was truncated")
	}
}

func TestProvenanceProjectionPreservesPartialRedactedOpaqueAndUnavailable(t *testing.T) {
	projection := canonicalProvenanceFixture()
	projection.AuthorityKind = values.Redacted[evidencev1.AuthorityKind]("policy_ref=secret;principal=secret")
	projection.AuthoritySystem = values.Redacted[string]("authority-system-ref=secret")
	projection.EvidenceSource = values.Unavailable[string]("evidence_ref=secret")
	projection.SourceVersion = values.Redacted[string]("digest=secret")
	projection.RecordedAt = values.Redacted[time.Time]("recorded-policy=secret")
	projection.HasOpaqueBoundary = true
	markup, err := provenanceFixture("en-US", projection)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Redacted", "Unavailable", "Partial lineage", "Opaque boundary: protected lineage is not shown"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("honest provenance cue %q missing: %s", want, markup)
		}
	}
	for _, secret := range []string{"policy_ref=secret", "principal=secret", "authority-system-ref=secret", "evidence_ref=secret", "digest=secret", "recorded-policy=secret"} {
		if strings.Contains(markup, secret) {
			t.Fatalf("protected provenance reason leaked: %q", secret)
		}
	}
}

func TestCanonicalProvenanceAdapterDropsRefsAndKeepsCanonicalTypes(t *testing.T) {
	projection := canonicalProvenanceFixture()
	if _, ok := projection.AuthorityKind.Get(); !ok {
		t.Fatal("canonical AuthorityKind was not projected")
	}
	if got, ok := projection.LineageStatus.Get(); !ok || got != lineage.StatusPartial {
		t.Fatalf("canonical lineage status = %q, %v", got, ok)
	}

	typ := reflect.TypeOf(projection)
	for index := 0; index < typ.NumField(); index++ {
		name := strings.ToLower(typ.Field(index).Name)
		for _, forbidden := range []string{"policyref", "evidenceref", "digest", "reason", "permission", "trust", "confidence"} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("display projection exposes protected/derived field %q", typ.Field(index).Name)
			}
		}
	}
	markup, err := provenanceFixture("en-US", projection)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "protected-authority-ref") || strings.Contains(markup, "protected-record-ref") {
		t.Fatal("canonical adapter exposed a protected policy or evidence ref")
	}
}

func TestProvenanceProjectionDoesNotInferPermissionOrAggregateTrust(t *testing.T) {
	projection := canonicalProvenanceFixture()
	projection.AuthorityKind = values.Value(evidencev1.AuthorityKind_AUTHORITY_KIND_DERIVED)
	projection.LineageStatus = values.Value(lineage.StatusComplete)
	projection.HasOpaqueBoundary = true // preserve even a contradictory service projection; do not normalize it.
	markup, err := provenanceFixture("en-US", projection)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Derived; no independent source authority", "Complete for this query", "Opaque boundary"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("independent canonical fact %q missing: %s", want, markup)
		}
	}
	for _, forbidden := range []string{"Trusted", "Trust score", "Can edit", "Can approve", "Permission granted"} {
		if strings.Contains(markup, forbidden) {
			t.Fatalf("provenance inferred aggregate trust or permission %q: %s", forbidden, markup)
		}
	}
}

func TestProvenanceProjectionRejectsUnknownCanonicalValues(t *testing.T) {
	projection := canonicalProvenanceFixture()
	projection.AuthorityKind = values.Value(evidencev1.AuthorityKind(999))
	projection.LineageStatus = values.Value(lineage.Status("FORGED_STATUS"))
	projection.EvidenceSource = values.Value("   ")
	projection.RecordedAt = values.Value(time.Time{})
	markup, err := provenanceFixture("en-US", projection)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(markup, "Invalid or unspecified") != 4 || strings.Count(markup, "provenance-tone-invalid") != 4 {
		t.Fatalf("invalid canonical values were not independently visible: %s", markup)
	}
	for _, forbidden := range []string{"999", "FORGED_STATUS"} {
		if strings.Contains(markup, forbidden) {
			t.Fatalf("unknown canonical value escaped into markup: %q", forbidden)
		}
	}
}

func TestUnboundProvenanceRendersNothing(t *testing.T) {
	markup, err := provenanceFixture("en-US", ProvenanceProjection{})
	if err != nil {
		t.Fatal(err)
	}
	if markup != "" {
		t.Fatalf("unbound provenance should not create an empty or unavailable panel: %q", markup)
	}
}

func BenchmarkProvenancePresentationRendering(b *testing.B) {
	node := ui.CreateElement(ProvenancePresentation, ProvenancePresentationProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, IDSeed: "benchmark-provenance", Projection: canonicalProvenanceFixture(),
	})
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ui.RenderToString(node); err != nil {
			b.Fatal(err)
		}
	}
}
