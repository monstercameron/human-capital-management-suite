package productui

import (
	"go/build"
	"strings"
	"testing"

	evidencev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/evidence/v1"
	lineage "github.com/monstercameron/human-capital-management-suite/internal/data/provenance"
)

// TestTodo_CHATBUG_014_NoDataLayerInTheClient: the page's provenance values are
// the canonical ones, and the package as the browser client compiles it imports
// neither the data layer nor the evidence messages.
func TestTodo_CHATBUG_014_NoDataLayerInTheClient(t *testing.T) {
	for page, canonical := range map[ProvenanceLineage]lineage.Status{
		ProvenanceLineageComplete: lineage.StatusComplete,
		ProvenanceLineagePartial:  lineage.StatusPartial,
		ProvenanceLineageUnknown:  lineage.StatusUnknown,
	} {
		if string(page) != string(canonical) {
			t.Errorf("the page's %q is not the canonical %q", page, canonical)
		}
		if _, _, _, valid := lineageToken(ProvenanceLineage(canonical)); !valid {
			t.Errorf("the canonical %q is not drawn", canonical)
		}
	}
	for page, canonical := range map[ProvenanceAuthority]evidencev1.AuthorityKind{
		ProvenanceAuthorityLocal:    evidencev1.AuthorityKind_AUTHORITY_KIND_LOCAL_AUTHORITATIVE,
		ProvenanceAuthorityExternal: evidencev1.AuthorityKind_AUTHORITY_KIND_EXTERNAL_OBSERVATION,
		ProvenanceAuthorityDerived:  evidencev1.AuthorityKind_AUTHORITY_KIND_DERIVED,
	} {
		if int32(page) != int32(canonical) {
			t.Errorf("the page's authority %d is not the canonical %s (%d)", page, canonical, canonical)
		}
		if _, _, _, valid := authorityKindToken(ProvenanceAuthority(canonical)); !valid {
			t.Errorf("the canonical %s is not drawn", canonical)
		}
	}
	// Every kind the messages define is either drawn or the unspecified one.
	for number, name := range evidencev1.AuthorityKind_name {
		_, _, _, valid := authorityKindToken(ProvenanceAuthority(number))
		if valid == (number == int32(evidencev1.AuthorityKind_AUTHORITY_KIND_UNSPECIFIED)) {
			t.Errorf("authority kind %s (%d): drawn = %v", name, number, valid)
		}
	}
	// The server-side projection still carries the canonical kind across.
	projection := ProjectAuthorizedCanonicalProvenance(&evidencev1.SourceAuthority{Kind: evidencev1.AuthorityKind_AUTHORITY_KIND_DERIVED, System: "payroll"}, nil, ProvenanceLineagePartial)
	if kind, ok := projection.AuthorityKind.Get(); !ok || kind != ProvenanceAuthorityDerived {
		t.Fatalf("projected authority = %v, %v", kind, ok)
	}

	// The imports of the package's own files, tests excluded, as the browser
	// build sees them.
	context := build.Default
	context.GOOS, context.GOARCH = "js", "wasm"
	pkg, err := context.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Imports) < 5 {
		t.Fatalf("read %d imports; the package was not read", len(pkg.Imports))
	}
	for _, path := range pkg.Imports {
		for _, server := range []string{"/internal/data/provenance", "/gen/go/hcmnext/evidence/", "/gen/go/hcmnext/provenance/", "jackc/pgx", "gopkg.in/yaml"} {
			if strings.Contains(path, server) {
				t.Errorf("productui imports %s in the browser client", path)
			}
		}
	}
}
