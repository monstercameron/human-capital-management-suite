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

func absenceVocabularyFixturePath(t *testing.T) string {
	t.Helper()
	return filepath.Join("testdata", "absence-vocabulary.yaml")
}

func loadAbsenceFixture(t *testing.T) AbsenceVocabulary {
	t.Helper()
	v, err := LoadAbsenceVocabularyYAML(absenceVocabularyFixturePath(t))
	if err != nil {
		t.Fatalf("LoadAbsenceVocabularyYAML: %v", err)
	}
	return v
}

// TestTodo_ALIGN_006 proves the versioned absence and disclosure-state rows
// have one explicit authority, canonical and disclosure rules, a closed
// omit/placeholder/deferred presentation, and a closed observe/carry
// boundary.
func TestTodo_ALIGN_006(t *testing.T) {
	v := loadAbsenceFixture(t)
	if err := v.Validate(); err != nil {
		t.Fatalf("absence vocabulary Validate: %v", err)
	}
	if err := v.VerifyDigest(); err != nil {
		t.Fatalf("absence vocabulary VerifyDigest: %v", err)
	}
	if len(v.States) < 8 {
		t.Fatalf("absence vocabulary has %d rows, want the cross-layer absence join points", len(v.States))
	}
	if got := v.Explain(); strings.Contains(got, "redact") || strings.Contains(got, "withhold") {
		t.Fatalf("Explain exposed disclosure values: %q", got)
	}
}

func TestTodo_ALIGN_006_Property(t *testing.T) {
	v := loadAbsenceFixture(t)
	for _, row := range v.States {
		if row.OwningLayer == "" {
			t.Fatalf("row %q has no owner", row.Kind)
		}
		if !validAbsencePresentation(strings.TrimSpace(row.Presentation)) {
			t.Fatalf("row %q has presentation %q", row.Kind, row.Presentation)
		}
		if _, err := v.Resolve(row.Kind); err != nil {
			t.Fatalf("Resolve(%q): %v", row.Kind, err)
		}
	}
	first := v.DigestValue()
	second, err := LoadAbsenceVocabularyYAML(absenceVocabularyFixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := second.DigestValue(); got != first {
		t.Fatalf("absence vocabulary digest is not deterministic: %q != %q", got, first)
	}
}

func TestTodo_ALIGN_006_Golden(t *testing.T) {
	v := loadAbsenceFixture(t)
	want, err := os.ReadFile(filepath.Join("testdata", "absence-vocabulary.golden.json"))
	if err != nil {
		t.Fatalf("read absence vocabulary golden: %v", err)
	}
	var golden struct {
		Digest string `json:"digest"`
	}
	if err := json.Unmarshal(want, &golden); err != nil {
		t.Fatalf("parse absence vocabulary golden: %v", err)
	}
	const wantDigest = "sha256:d4b6649cddc5af34b9b9ee5e36f49527bc168238aca7fd9a53403482ceb2703a"
	if golden.Digest != wantDigest {
		t.Fatalf("golden digest=%q want=%q", golden.Digest, wantDigest)
	}
	if got := v.DigestValue(); got != wantDigest || v.Digest != wantDigest {
		t.Fatalf("absence vocabulary digest=%q stored=%q want=%q", got, v.Digest, wantDigest)
	}
}

func TestTodo_ALIGN_006_Security(t *testing.T) {
	base := AbsenceDefinition{Kind: "withheld_authorization", OwningLayer: "governance", CanonicalForm: "authorization withholds the field", DisclosureRule: "omit uniformly", Presentation: AbsencePresentOmit, ObservedBy: []string{"governance", "business"}, CarriedBy: []string{"persistence", "presentation"}}
	cases := []struct {
		name string
		rows []AbsenceDefinition
		want error
	}{
		{name: "ownerless", rows: []AbsenceDefinition{{Kind: "withheld_authorization", CanonicalForm: "closed", DisclosureRule: "omit", Presentation: AbsencePresentOmit}}, want: ErrAbsenceOwnerless},
		{name: "doubly owned", rows: []AbsenceDefinition{base, {Kind: "withheld_authorization", OwningLayer: "business", CanonicalForm: base.CanonicalForm, DisclosureRule: base.DisclosureRule, Presentation: base.Presentation, ObservedBy: []string{"business"}, CarriedBy: []string{"presentation"}}}, want: ErrAbsenceDoublyOwned},
		{name: "owner carry-only", rows: []AbsenceDefinition{{Kind: "withheld_authorization", OwningLayer: "governance", CanonicalForm: base.CanonicalForm, DisclosureRule: base.DisclosureRule, Presentation: base.Presentation, ObservedBy: []string{"governance"}, CarriedBy: []string{"governance"}}}, want: ErrAbsenceInconsistent},
		{name: "owner not observing", rows: []AbsenceDefinition{{Kind: "withheld_authorization", OwningLayer: "governance", CanonicalForm: base.CanonicalForm, DisclosureRule: base.DisclosureRule, Presentation: base.Presentation, ObservedBy: []string{"business"}, CarriedBy: []string{"presentation"}}}, want: ErrAbsenceInconsistent},
		{name: "unknown presentation", rows: []AbsenceDefinition{{Kind: "withheld_authorization", OwningLayer: "governance", CanonicalForm: base.CanonicalForm, DisclosureRule: base.DisclosureRule, Presentation: "whisper", ObservedBy: []string{"governance"}}}, want: ErrAbsenceInconsistent},
		{name: "empty presentation", rows: []AbsenceDefinition{{Kind: "withheld_authorization", OwningLayer: "governance", CanonicalForm: base.CanonicalForm, DisclosureRule: base.DisclosureRule, ObservedBy: []string{"governance"}}}, want: ErrAbsenceInconsistent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := NewAbsenceVocabulary(tc.rows...)
			err := v.Validate()
			if !errors.Is(err, tc.want) {
				t.Fatalf("Validate error=%v, want errors.Is(..., %v)", err, tc.want)
			}
			var refusalErr *AbsenceRefusal
			if !errors.As(err, &refusalErr) || refusalErr.Kind != "withheld_authorization" || refusalErr.Layer == "" {
				t.Fatalf("refusal=%#v, want typed kind/layer refusal", err)
			}
		})
	}
}

func TestTodo_ALIGN_006_Conformance(t *testing.T) {
	v := loadAbsenceFixture(t)
	if err := v.Validate(); err != nil {
		t.Fatalf("absence vocabulary Validate: %v", err)
	}
	// Authorization-withheld fields must omit uniformly with never-collected
	// fields, so a product response is not an existence oracle.
	withheld, err := v.Resolve("withheld_authorization")
	if err != nil {
		t.Fatalf("Resolve(withheld_authorization): %v", err)
	}
	neverCollected, err := v.Resolve("not_collected")
	if err != nil {
		t.Fatalf("Resolve(not_collected): %v", err)
	}
	if withheld.Presentation != AbsencePresentOmit || neverCollected.Presentation != AbsencePresentOmit {
		t.Fatalf("withheld=%q not_collected=%q, both must omit", withheld.Presentation, neverCollected.Presentation)
	}
	// Every presentation class must be exercised by at least one row, so no
	// class is dead contract.
	seen := make(map[string]bool)
	for _, row := range v.States {
		seen[strings.TrimSpace(row.Presentation)] = true
	}
	for _, class := range []string{AbsencePresentOmit, AbsencePresentPlaceholder, AbsencePresentDeferred} {
		if !seen[class] {
			t.Fatalf("presentation class %q has no vocabulary row", class)
		}
	}
}

func FuzzTodo_ALIGN_006_Fuzz(f *testing.F) {
	data, err := os.ReadFile(filepath.Join("testdata", "absence-vocabulary.yaml"))
	if err != nil {
		f.Skip("absence vocabulary fixture is missing")
	}
	f.Add(data)
	f.Fuzz(func(t *testing.T, raw []byte) {
		var v AbsenceVocabulary
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
