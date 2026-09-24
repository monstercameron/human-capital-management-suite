package productslice

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func ownershipFixturePath(t *testing.T) string {
	t.Helper()
	return filepath.Join("testdata", "slice-ownership.yaml")
}

func loadOwnershipFixture(t *testing.T) OwnershipRegistry {
	t.Helper()
	r, err := LoadOwnershipRegistryYAML(ownershipFixturePath(t))
	if err != nil {
		t.Fatalf("LoadOwnershipRegistryYAML: %v", err)
	}
	return r
}

// TestTodo_ALIGN_007 proves every declared product slice has exactly one
// owner, at least one admitted consumer, a pinned definition digest, and a
// closed ownership boundary.
func TestTodo_ALIGN_007(t *testing.T) {
	r := loadOwnershipFixture(t)
	if err := r.Validate(); err != nil {
		t.Fatalf("ownership Validate: %v", err)
	}
	if err := r.VerifyDigest(); err != nil {
		t.Fatalf("ownership VerifyDigest: %v", err)
	}
	if len(r.Slices) < 1 {
		t.Fatalf("ownership registry declares %d slices, want the admitted product slices", len(r.Slices))
	}
	decl, err := r.Resolve("promotion")
	if err != nil {
		t.Fatalf("Resolve(promotion): %v", err)
	}
	if !r.Admits("promotion", decl.Consumers[0]) {
		t.Fatalf("declared consumer %q is not admitted", decl.Consumers[0])
	}
	if got := r.Explain(); strings.Contains(got, "people") || strings.Contains(got, "promotion") && strings.Contains(got, ".") {
		t.Fatalf("Explain exposed ownership values: %q", got)
	}
}

func TestTodo_ALIGN_007_Property(t *testing.T) {
	r := loadOwnershipFixture(t)
	for _, row := range r.Slices {
		if row.Owner == "" {
			t.Fatalf("slice %q has no owner", row.SliceID)
		}
		if len(row.Consumers) == 0 {
			t.Fatalf("slice %q has no consumer", row.SliceID)
		}
		if _, err := r.Resolve(row.SliceID); err != nil {
			t.Fatalf("Resolve(%q): %v", row.SliceID, err)
		}
	}
	first := r.DigestValue()
	second, err := LoadOwnershipRegistryYAML(ownershipFixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := second.DigestValue(); got != first {
		t.Fatalf("ownership digest is not deterministic: %q != %q", got, first)
	}
}

func TestTodo_ALIGN_007_Golden(t *testing.T) {
	r := loadOwnershipFixture(t)
	want, err := os.ReadFile(filepath.Join("testdata", "slice-ownership.golden.json"))
	if err != nil {
		t.Fatalf("read slice ownership golden: %v", err)
	}
	var golden struct {
		Digest string `json:"digest"`
	}
	if err := json.Unmarshal(want, &golden); err != nil {
		t.Fatalf("parse slice ownership golden: %v", err)
	}
	const wantDigest = "sha256:83374141584c8924f512c32c6d3eb6033164e7961da20ac003320459fa0098f4"
	if golden.Digest != wantDigest {
		t.Fatalf("golden digest=%q want=%q", golden.Digest, wantDigest)
	}
	if got := r.DigestValue(); got != wantDigest || r.Digest != wantDigest {
		t.Fatalf("slice ownership digest=%q stored=%q want=%q", got, r.Digest, wantDigest)
	}
}

func TestTodo_ALIGN_007_Security(t *testing.T) {
	base := SliceDeclaration{SliceID: "promotion", Owner: "people", Consumers: []string{"promotion.detail.page"}, DefinitionDigest: "sha256:promotion-definition"}
	cases := []struct {
		name string
		rows []SliceDeclaration
		want error
	}{
		{name: "ownerless", rows: []SliceDeclaration{{SliceID: "promotion", Consumers: []string{"promotion.detail.page"}, DefinitionDigest: "sha256:promotion-definition"}}, want: ErrOwnershipOwnerless},
		{name: "doubly owned", rows: []SliceDeclaration{base, {SliceID: "promotion", Owner: "payroll", Consumers: base.Consumers, DefinitionDigest: base.DefinitionDigest}}, want: ErrOwnershipDoublyOwned},
		{name: "consumerless", rows: []SliceDeclaration{{SliceID: "promotion", Owner: "people", DefinitionDigest: "sha256:promotion-definition"}}, want: ErrOwnershipConsumerless},
		{name: "owner consumes itself", rows: []SliceDeclaration{{SliceID: "promotion", Owner: "people", Consumers: []string{"people"}, DefinitionDigest: base.DefinitionDigest}}, want: ErrOwnershipInconsistent},
		{name: "duplicate consumer", rows: []SliceDeclaration{{SliceID: "promotion", Owner: "people", Consumers: []string{"promotion.detail.page", "promotion.detail.page"}, DefinitionDigest: base.DefinitionDigest}}, want: ErrOwnershipInconsistent},
		{name: "missing definition digest", rows: []SliceDeclaration{{SliceID: "promotion", Owner: "people", Consumers: base.Consumers}}, want: ErrOwnershipInconsistent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewOwnershipRegistry(tc.rows...)
			err := r.Validate()
			if !errors.Is(err, tc.want) {
				t.Fatalf("Validate error=%v, want errors.Is(..., %v)", err, tc.want)
			}
			var refusalErr *OwnershipRefusal
			if !errors.As(err, &refusalErr) || refusalErr.Slice != "promotion" || refusalErr.Layer == "" {
				t.Fatalf("refusal=%#v, want typed slice/layer refusal", err)
			}
		})
	}
	// Undeclared consumers and unknown slices are refused uniformly.
	r := loadOwnershipFixture(t)
	if r.Admits("promotion", "attacker.evil.page") {
		t.Fatal("undeclared consumer was admitted")
	}
	if r.Admits("payroll", "promotion.detail.page") {
		t.Fatal("unknown slice admitted a consumer")
	}
}

func TestTodo_ALIGN_007_Conformance(t *testing.T) {
	r := loadOwnershipFixture(t)
	if err := r.Validate(); err != nil {
		t.Fatalf("ownership Validate: %v", err)
	}
	// Every slice admitted by the generated product-slice registry must
	// carry exactly one ownership declaration, so no admitted slice is
	// ownerless or consumerless in production.
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	reg, err := LoadRegistryYAML(filepath.Join(root, "definitions", "planning", "product-slices.yaml"))
	if err != nil {
		t.Fatalf("LoadRegistryYAML: %v", err)
	}
	if len(reg.Slices) == 0 {
		t.Fatal("generated product-slice registry admits no slices")
	}
	for _, slice := range reg.Slices {
		decl, err := r.Resolve(slice.SliceID)
		if err != nil {
			t.Fatalf("admitted slice %q has no ownership declaration: %v", slice.SliceID, err)
		}
		if decl.Owner == "" || len(decl.Consumers) == 0 {
			t.Fatalf("admitted slice %q is ownerless or consumerless", slice.SliceID)
		}
	}
}

// TestTodo_REV_081_02 proves the checked-in ownership registry has a valid
// declaration for every currently admitted product slice.
func TestTodo_REV_081_02(t *testing.T) {
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	registry, err := LoadOwnershipRegistryYAML(filepath.Join(root, "definitions", "planning", "slice-ownership.yaml"))
	if err != nil {
		t.Fatalf("LoadOwnershipRegistryYAML: %v", err)
	}
	if err := registry.Validate(); err != nil {
		t.Fatalf("ownership Validate: %v", err)
	}
	if err := registry.VerifyDigest(); err != nil {
		t.Fatalf("ownership VerifyDigest: %v", err)
	}
	if _, err := registry.Resolve("promotion"); err != nil {
		t.Fatalf("Resolve(promotion): %v", err)
	}
}

// TestTodo_REV_081_02_Conformance checks the checked-in declaration against
// the real Promotion definition and generated admitted-slice registry.
func TestTodo_REV_081_02_Conformance(t *testing.T) {
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	ownershipPath := filepath.Join(root, "definitions", "planning", "slice-ownership.yaml")
	ownership, err := LoadOwnershipRegistryYAML(ownershipPath)
	if err != nil {
		t.Fatalf("LoadOwnershipRegistryYAML(%s): %v", ownershipPath, err)
	}
	if err := ownership.Validate(); err != nil {
		t.Fatalf("ownership Validate: %v", err)
	}
	if err := ownership.VerifyDigest(); err != nil {
		t.Fatalf("ownership VerifyDigest: %v", err)
	}

	productPath := filepath.Join(root, "definitions", "planning", "product-slices.yaml")
	products, err := LoadRegistryYAML(productPath)
	if err != nil {
		t.Fatalf("LoadRegistryYAML(%s): %v", productPath, err)
	}
	if err := products.VerifyDigest(); err != nil {
		t.Fatalf("product slice registry VerifyDigest: %v", err)
	}
	if len(products.Slices) != 1 || products.Slices[0].SliceID != "promotion" {
		t.Fatalf("live admitted slices=%v, want only promotion", products.Slices)
	}

	live := PromotionSliceDefinition()
	if live.SliceID != products.Slices[0].SliceID {
		t.Fatalf("live Promotion slice id %q differs from admitted id %q", live.SliceID, products.Slices[0].SliceID)
	}
	declaration, err := ownership.Resolve(live.SliceID)
	if err != nil {
		t.Fatalf("Resolve(%s): %v", live.SliceID, err)
	}
	if declaration.DefinitionDigest != live.Digest() {
		t.Fatalf("ownership definition_digest=%q, want live Promotion digest %q", declaration.DefinitionDigest, live.Digest())
	}
	if err := ownership.VerifyDefinition(live); err != nil {
		t.Fatalf("ownership VerifyDefinition(live Promotion): %v", err)
	}
	if products.Slices[0].Digest() != live.Digest() {
		t.Fatal("checked-in admitted Promotion definition differs from the live Promotion definition")
	}
	if declaration.Owner != "people" {
		t.Fatalf("Promotion owner=%q, want people", declaration.Owner)
	}
	for _, consumer := range []string{"promotion.journeys.list", "promotion.journeys.detail", "hcmnext.people.promote_worker"} {
		if !ownership.Admits(live.SliceID, consumer) {
			t.Errorf("live Promotion consumer %q is not admitted", consumer)
		}
	}
}

func TestTodo_REV_081_02_Security(t *testing.T) {
	r := NewOwnershipRegistry(SliceDeclaration{
		SliceID: "promotion", Owner: "people", Consumers: []string{"promotion.journeys.detail"},
		DefinitionDigest: PromotionSliceDefinition().Digest(),
	})
	if r.Admits("promotion", "promotion.admin.unknown") {
		t.Fatal("undeclared consumer was admitted")
	}
	if r.Admits("payroll", "promotion.journeys.detail") {
		t.Fatal("consumer was admitted to an unknown slice")
	}
	forged := r
	forged.Slices = append([]SliceDeclaration(nil), r.Slices...)
	forged.Slices[0].DefinitionDigest = "sha256:forged"
	if err := forged.Validate(); err != nil {
		t.Fatalf("forged but structurally valid registry should reach digest check: %v", err)
	}
	if err := forged.VerifyDigest(); err == nil {
		t.Fatal("forged definition digest did not invalidate ownership registry digest")
	}
	if err := r.VerifyDefinition(ProductSliceDefinition{SliceID: "promotion", Version: 99}); err == nil {
		t.Fatal("ownership registry accepted a different live definition")
	}
}

func TestTodo_REV_081_02_Golden(t *testing.T) {
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	path := filepath.Join(root, "definitions", "planning", "slice-ownership.yaml")
	r, err := LoadOwnershipRegistryYAML(path)
	if err != nil {
		t.Fatalf("LoadOwnershipRegistryYAML: %v", err)
	}
	const wantDigest = "sha256:adc762debcdd999b1e744448b2976150a6d538629cadca0c73ad0649d8903fac"
	if r.Digest != wantDigest || r.DigestValue() != wantDigest {
		t.Fatalf("checked-in ownership digest=%q recomputed=%q, want %q", r.Digest, r.DigestValue(), wantDigest)
	}
}

func FuzzTodo_ALIGN_007_Fuzz(f *testing.F) {
	data, err := os.ReadFile(filepath.Join("testdata", "slice-ownership.yaml"))
	if err != nil {
		f.Skip("slice ownership fixture is missing")
	}
	f.Add(data)
	f.Fuzz(func(t *testing.T, raw []byte) {
		var r OwnershipRegistry
		if err := yaml.Unmarshal(raw, &r); err != nil {
			t.Skip("not an ownership document")
		}
		_ = r.Validate()
		first := r.DigestValue()
		if second := r.DigestValue(); first != second {
			t.Fatalf("digest is not deterministic: %q != %q", first, second)
		}
	})
}
