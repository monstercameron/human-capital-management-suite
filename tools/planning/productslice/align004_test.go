package productslice

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func temporalVocabularyFixturePath(t *testing.T) string {
	t.Helper()
	return filepath.Join("testdata", "temporal-vocabulary.yaml")
}

func loadTemporalFixture(t *testing.T) TemporalVocabulary {
	t.Helper()
	v, err := LoadTemporalVocabularyYAML(temporalVocabularyFixturePath(t))
	if err != nil {
		t.Fatalf("LoadTemporalVocabularyYAML: %v", err)
	}
	return v
}

// TestTodo_ALIGN_004 proves the versioned temporal rows have one explicit
// authority, canonical and digest rules, half-open interval semantics, and a
// closed observe/carry boundary.
func TestTodo_ALIGN_004(t *testing.T) {
	v := loadTemporalFixture(t)
	if err := v.Validate(); err != nil {
		t.Fatalf("temporal vocabulary Validate: %v", err)
	}
	if err := v.VerifyDigest(); err != nil {
		t.Fatalf("temporal vocabulary VerifyDigest: %v", err)
	}
	if len(v.Moments) < 10 {
		t.Fatalf("temporal vocabulary has %d rows, want the cross-layer temporal join points", len(v.Moments))
	}
	if got := v.Explain(); strings.Contains(got, "2026") || strings.Contains(got, ":") && strings.Contains(got, "T") {
		t.Fatalf("Explain exposed temporal values: %q", got)
	}
}

func TestTodo_ALIGN_004_Property(t *testing.T) {
	v := loadTemporalFixture(t)
	for _, row := range v.Moments {
		if row.OwningLayer == "" {
			t.Fatalf("row %q has no owner", row.Kind)
		}
		if _, err := v.Resolve(row.Kind); err != nil {
			t.Fatalf("Resolve(%q): %v", row.Kind, err)
		}
	}
	first := v.DigestValue()
	second, err := LoadTemporalVocabularyYAML(temporalVocabularyFixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := second.DigestValue(); got != first {
		t.Fatalf("temporal vocabulary digest is not deterministic: %q != %q", got, first)
	}
}

func TestTodo_ALIGN_004_Golden(t *testing.T) {
	v := loadTemporalFixture(t)
	want, err := os.ReadFile(filepath.Join("testdata", "temporal-vocabulary.golden.json"))
	if err != nil {
		t.Fatalf("read temporal vocabulary golden: %v", err)
	}
	var golden struct {
		Digest string `json:"digest"`
	}
	if err := json.Unmarshal(want, &golden); err != nil {
		t.Fatalf("parse temporal vocabulary golden: %v", err)
	}
	const wantDigest = "sha256:68337c0d4343b2f56391ea5ffc51c1d87acca20160656ce28de483d4804fb41e"
	if golden.Digest != wantDigest {
		t.Fatalf("golden digest=%q want=%q", golden.Digest, wantDigest)
	}
	if got := v.DigestValue(); got != wantDigest || v.Digest != wantDigest {
		t.Fatalf("temporal vocabulary digest=%q stored=%q want=%q", got, v.Digest, wantDigest)
	}
}

func TestTodo_ALIGN_004_Security(t *testing.T) {
	base := TemporalDefinition{Kind: "effective_at", OwningLayer: "business", CanonicalForm: "RFC 3339 timestamptz", TimeClass: TimeClassInstant, DigestRule: "sha256 canonical", ObservedBy: []string{"business"}, CarriedBy: []string{"presentation", "persistence"}}
	cases := []struct {
		name string
		rows []TemporalDefinition
		want error
	}{
		{name: "ownerless", rows: []TemporalDefinition{{Kind: "effective_at", CanonicalForm: "RFC 3339", TimeClass: TimeClassInstant, DigestRule: "sha256"}}, want: ErrTemporalOwnerless},
		{name: "doubly owned", rows: []TemporalDefinition{base, {Kind: "effective_at", OwningLayer: "persistence", CanonicalForm: base.CanonicalForm, TimeClass: base.TimeClass, DigestRule: base.DigestRule, ObservedBy: []string{"persistence"}, CarriedBy: []string{"business"}}}, want: ErrTemporalDoublyOwned},
		{name: "owner carry-only", rows: []TemporalDefinition{{Kind: "effective_at", OwningLayer: "business", CanonicalForm: base.CanonicalForm, TimeClass: base.TimeClass, DigestRule: base.DigestRule, ObservedBy: []string{"business"}, CarriedBy: []string{"business"}}}, want: ErrTemporalInconsistent},
		{name: "owner not observing", rows: []TemporalDefinition{{Kind: "effective_at", OwningLayer: "business", CanonicalForm: base.CanonicalForm, TimeClass: base.TimeClass, DigestRule: base.DigestRule, ObservedBy: []string{"persistence"}, CarriedBy: []string{"presentation"}}}, want: ErrTemporalInconsistent},
		{name: "unknown class", rows: []TemporalDefinition{{Kind: "effective_at", OwningLayer: "business", CanonicalForm: base.CanonicalForm, TimeClass: "eternal", DigestRule: base.DigestRule, ObservedBy: []string{"business"}}}, want: ErrTemporalInconsistent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := NewTemporalVocabulary(tc.rows...)
			err := v.Validate()
			if !errors.Is(err, tc.want) {
				t.Fatalf("Validate error=%v, want errors.Is(..., %v)", err, tc.want)
			}
			var refusalErr *TemporalRefusal
			if !errors.As(err, &refusalErr) || refusalErr.Kind != "effective_at" || refusalErr.Layer == "" {
				t.Fatalf("refusal=%#v, want typed kind/layer refusal", err)
			}
		})
	}
}

func TestTodo_ALIGN_004_Conformance(t *testing.T) {
	v := loadTemporalFixture(t)
	if err := v.Validate(); err != nil {
		t.Fatalf("temporal vocabulary Validate: %v", err)
	}
	// Every temporal join point named by the modeling conventions
	// (TemporalPoint, EffectiveInterval) and the default product alignment
	// (requested time, projection watermark, source sequence) resolves.
	required := []string{
		"effective_at", "known_at", "recorded_at", "observed_at",
		"received_at", "policy_as_of", "requested_time",
		"effective_interval", "projection_watermark", "source_sequence",
	}
	for _, kind := range required {
		row, err := v.Resolve(kind)
		if err != nil {
			t.Errorf("temporal kind %q: %v", kind, err)
			continue
		}
		if row.OwningLayer == "" {
			t.Errorf("temporal kind %q has no owning layer", kind)
		}
	}
	interval, err := v.Resolve("effective_interval")
	if err != nil {
		t.Fatal(err)
	}
	if interval.TimeClass != TimeClassHalfOpenInterval {
		t.Fatalf("effective_interval class=%q, want half-open", interval.TimeClass)
	}
	effective, err := v.Resolve("effective_at")
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := v.Resolve("recorded_at")
	if err != nil {
		t.Fatal(err)
	}
	// Runtime timestamps never substitute for business-effective time: kinds
	// are comparable only with themselves.
	if !effective.Comparable(effective) || effective.Comparable(recorded) {
		t.Fatal("temporal kinds must compare only with their own kind")
	}
}
