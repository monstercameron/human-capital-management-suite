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

func lifecycleVocabularyFixturePath(t *testing.T) string {
	t.Helper()
	return filepath.Join("testdata", "lifecycle-vocabulary.yaml")
}

func loadLifecycleFixture(t *testing.T) LifecycleVocabulary {
	t.Helper()
	v, err := LoadLifecycleVocabularyYAML(lifecycleVocabularyFixturePath(t))
	if err != nil {
		t.Fatalf("LoadLifecycleVocabularyYAML: %v", err)
	}
	return v
}

// TestTodo_ALIGN_005 proves the versioned multidimensional lifecycle rows
// have one explicit authority, canonical and projection rules, admitted
// dimensions, and a closed observe/carry boundary.
func TestTodo_ALIGN_005(t *testing.T) {
	v := loadLifecycleFixture(t)
	if err := v.Validate(); err != nil {
		t.Fatalf("lifecycle vocabulary Validate: %v", err)
	}
	if err := v.VerifyDigest(); err != nil {
		t.Fatalf("lifecycle vocabulary VerifyDigest: %v", err)
	}
	if len(v.Projections) < 8 {
		t.Fatalf("lifecycle vocabulary has %d rows, want the cross-layer lifecycle join points", len(v.Projections))
	}
	if got := v.Explain(); strings.Contains(got, "hire") || strings.Contains(got, "active") {
		t.Fatalf("Explain exposed lifecycle values: %q", got)
	}
}

func TestTodo_ALIGN_005_Property(t *testing.T) {
	v := loadLifecycleFixture(t)
	for _, row := range v.Projections {
		if row.OwningLayer == "" {
			t.Fatalf("row %q has no owner", row.Kind)
		}
		if len(row.Dimensions) == 0 {
			t.Fatalf("row %q spans no dimension", row.Kind)
		}
		if _, err := v.Resolve(row.Kind); err != nil {
			t.Fatalf("Resolve(%q): %v", row.Kind, err)
		}
	}
	first := v.DigestValue()
	second, err := LoadLifecycleVocabularyYAML(lifecycleVocabularyFixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := second.DigestValue(); got != first {
		t.Fatalf("lifecycle vocabulary digest is not deterministic: %q != %q", got, first)
	}
}

func TestTodo_ALIGN_005_Golden(t *testing.T) {
	v := loadLifecycleFixture(t)
	want, err := os.ReadFile(filepath.Join("testdata", "lifecycle-vocabulary.golden.json"))
	if err != nil {
		t.Fatalf("read lifecycle vocabulary golden: %v", err)
	}
	var golden struct {
		Digest string `json:"digest"`
	}
	if err := json.Unmarshal(want, &golden); err != nil {
		t.Fatalf("parse lifecycle vocabulary golden: %v", err)
	}
	const wantDigest = "sha256:cda46c3f67e3160d8dade22b4f950b527876928cc96769cbc1e10fe767496464"
	if golden.Digest != wantDigest {
		t.Fatalf("golden digest=%q want=%q", golden.Digest, wantDigest)
	}
	if got := v.DigestValue(); got != wantDigest || v.Digest != wantDigest {
		t.Fatalf("lifecycle vocabulary digest=%q stored=%q want=%q", got, v.Digest, wantDigest)
	}
}

func TestTodo_ALIGN_005_Security(t *testing.T) {
	base := LifecycleDefinition{Kind: "employment_status", OwningLayer: "business", CanonicalForm: "closed employment condition", ProjectionRule: "newest effective event decides", Dimensions: []string{"employment"}, ObservedBy: []string{"business"}, CarriedBy: []string{"presentation", "persistence"}}
	cases := []struct {
		name string
		rows []LifecycleDefinition
		want error
	}{
		{name: "ownerless", rows: []LifecycleDefinition{{Kind: "employment_status", CanonicalForm: "closed", ProjectionRule: "rule", Dimensions: []string{"employment"}}}, want: ErrLifecycleOwnerless},
		{name: "doubly owned", rows: []LifecycleDefinition{base, {Kind: "employment_status", OwningLayer: "persistence", CanonicalForm: base.CanonicalForm, ProjectionRule: base.ProjectionRule, Dimensions: base.Dimensions, ObservedBy: []string{"persistence"}, CarriedBy: []string{"business"}}}, want: ErrLifecycleDoublyOwned},
		{name: "owner carry-only", rows: []LifecycleDefinition{{Kind: "employment_status", OwningLayer: "business", CanonicalForm: base.CanonicalForm, ProjectionRule: base.ProjectionRule, Dimensions: base.Dimensions, ObservedBy: []string{"business"}, CarriedBy: []string{"business"}}}, want: ErrLifecycleInconsistent},
		{name: "owner not observing", rows: []LifecycleDefinition{{Kind: "employment_status", OwningLayer: "business", CanonicalForm: base.CanonicalForm, ProjectionRule: base.ProjectionRule, Dimensions: base.Dimensions, ObservedBy: []string{"persistence"}, CarriedBy: []string{"presentation"}}}, want: ErrLifecycleInconsistent},
		{name: "no dimension", rows: []LifecycleDefinition{{Kind: "employment_status", OwningLayer: "business", CanonicalForm: base.CanonicalForm, ProjectionRule: base.ProjectionRule, ObservedBy: []string{"business"}}}, want: ErrLifecycleInconsistent},
		{name: "unadmitted dimension", rows: []LifecycleDefinition{{Kind: "employment_status", OwningLayer: "business", CanonicalForm: base.CanonicalForm, ProjectionRule: base.ProjectionRule, Dimensions: []string{"astrology"}, ObservedBy: []string{"business"}}}, want: ErrLifecycleInconsistent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := NewLifecycleVocabulary(tc.rows...)
			err := v.Validate()
			if !errors.Is(err, tc.want) {
				t.Fatalf("Validate error=%v, want errors.Is(..., %v)", err, tc.want)
			}
			var refusalErr *LifecycleRefusal
			if !errors.As(err, &refusalErr) || refusalErr.Kind != "employment_status" || refusalErr.Layer == "" {
				t.Fatalf("refusal=%#v, want typed kind/layer refusal", err)
			}
		})
	}
}

func TestTodo_ALIGN_005_Conformance(t *testing.T) {
	v := loadLifecycleFixture(t)
	if err := v.Validate(); err != nil {
		t.Fatalf("lifecycle vocabulary Validate: %v", err)
	}
	// Every admitted lifecycle axis must be spanned by at least one row,
	// otherwise a product surface could project a dimension the vocabulary
	// does not govern.
	covered := v.DimensionsCovered()
	for dim := range admittedLifecycleDimensions {
		found := false
		for _, got := range covered {
			if got == dim {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("admitted dimension %q is spanned by no vocabulary row", dim)
		}
	}
	// The workflow-stage row must join the workflow and proposal axes so the
	// governed work loop and the intent lifecycle project as one state.
	stage, err := v.Resolve("workflow_stage")
	if err != nil {
		t.Fatalf("Resolve(workflow_stage): %v", err)
	}
	axes := make(map[string]bool, len(stage.Dimensions))
	for _, dim := range stage.Dimensions {
		axes[dim] = true
	}
	if !axes["workflow"] || !axes["proposal"] {
		t.Fatalf("workflow_stage dimensions=%v, want the workflow and proposal axes", stage.Dimensions)
	}
}

func FuzzTodo_ALIGN_005_Fuzz(f *testing.F) {
	data, err := os.ReadFile(filepath.Join("testdata", "lifecycle-vocabulary.yaml"))
	if err != nil {
		f.Skip("lifecycle vocabulary fixture is missing")
	}
	f.Add(data)
	f.Fuzz(func(t *testing.T, raw []byte) {
		var v LifecycleVocabulary
		if err := yaml.Unmarshal(raw, &v); err != nil {
			t.Skip("not a vocabulary document")
		}
		_ = v.Validate()
		first := v.DigestValue()
		if second := v.DigestValue(); first != second {
			t.Fatalf("digest is not deterministic: %q != %q", first, second)
		}
	})
}
