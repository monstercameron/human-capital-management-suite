package productslice

import (
	"errors"
	"strings"
	"testing"
)

func dispositionFixture() (ProductSliceDefinition, []SliceElementDisposition) {
	slice := ProductSliceDefinition{
		SliceID: "promotion", Version: 1,
		BusinessIntents: []string{"intent.promotion"}, Features: []string{"feature.promotion"},
		Pages: []string{"promotion.list"}, Widgets: []string{"promotion.card@1"},
		Capabilities: []string{"promotion.read"}, Packages: []string{"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"},
		Todos: []string{"ALIGN-002"},
	}
	var elements []SliceElementDisposition
	for _, ref := range slice.elementRefs() {
		elements = append(elements, SliceElementDisposition{ElementRef: ref, Disposition: DispositionCoreRequired, ReasonCode: "DEFAULT_SCOPE"})
	}
	return slice, elements
}

// TestTodo_ALIGN_002 proves the vocabulary and the per-element contract are
// closed, reasoned, and detached from mutable caller slices.
func TestTodo_ALIGN_002(t *testing.T) {
	if len(DefaultProductDispositionVocabulary()) != 6 {
		t.Fatalf("vocabulary length=%d, want 6", len(DefaultProductDispositionVocabulary()))
	}
	slice, elements := dispositionFixture()
	set, err := NewDispositionSet(slice, elements...)
	if err != nil {
		t.Fatalf("NewDispositionSet: %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if set.CanonicalDigest == "" {
		t.Fatal("set has no canonical digest")
	}
	missing := append([]SliceElementDisposition(nil), elements[:len(elements)-1]...)
	if got := slice.ValidateDispositions(missing); len(got) == 0 {
		t.Fatal("slice accepted an element without a disposition")
	}
}

func TestTodo_ALIGN_002_Property(t *testing.T) {
	_, elements := dispositionFixture()
	for _, disposition := range []ProductDisposition{DispositionCoreRequired, DispositionDomainPackDefault, DispositionAvailableNotEnabled, DispositionCustomerDefined, DispositionDeferred, DispositionProhibited} {
		candidate := elements[0]
		candidate.Disposition = disposition
		if err := candidate.Validate(); err != nil {
			t.Fatalf("%s: %v", disposition, err)
		}
	}
	bad := elements[0]
	bad.Disposition = ProductDisposition("UNKNOWN")
	if err := bad.Validate(); !errors.Is(err, ErrInvalidDisposition) {
		t.Fatalf("bad disposition error=%v", err)
	}
	bad = elements[0]
	bad.ReasonCode = ""
	if err := bad.Validate(); !errors.Is(err, ErrInvalidDisposition) {
		t.Fatalf("missing reason error=%v", err)
	}
}

func TestTodo_ALIGN_002_Golden(t *testing.T) {
	const want = "sha256:fec4134a620f0818e1c6896be98ba27bc89acc240e891f9166a3457a0c398946"
	if DispositionVocabularyDigest() != want || DefaultDispositionVocabularyDigest != want {
		t.Fatalf("vocabulary digest=%q constant=%q want=%q", DispositionVocabularyDigest(), DefaultDispositionVocabularyDigest, want)
	}
	vocabulary := DefaultProductDispositionVocabulary()
	vocabulary[0].Definition = "mutated"
	if DefaultProductDispositionVocabulary()[0].Definition == "mutated" {
		t.Fatal("vocabulary result aliases package state")
	}
}

func TestTodo_ALIGN_002_Security(t *testing.T) {
	_, elements := dispositionFixture()
	// Reason codes are identifiers, not a channel for multiline or hidden
	// content that could leak into logs and evidence.
	bad := elements[0]
	bad.ReasonCode = "REASON\nSECRET"
	if err := bad.Validate(); !errors.Is(err, ErrInvalidDisposition) {
		t.Fatalf("multiline reason accepted: %v", err)
	}
	set, err := NewDispositionSet(ProductSliceDefinition{SliceID: "promotion", Version: 1}, elements[0])
	if err != nil {
		t.Fatalf("single set: %v", err)
	}
	if strings.Contains(set.Explain(), elements[0].ReasonCode) || strings.Contains(set.Explain(), elements[0].ElementRef) {
		t.Fatal("Explain exposed element or reason values")
	}
}

func TestTodo_ALIGN_002_Conformance(t *testing.T) {
	slice, elements := dispositionFixture()
	set, err := NewDispositionSet(slice, elements...)
	if err != nil {
		t.Fatalf("NewDispositionSet: %v", err)
	}
	if got, err := set.Digest(); err != nil || got != set.CanonicalDigest {
		t.Fatalf("digest=%q err=%v canonical=%q", got, err, set.CanonicalDigest)
	}
	duplicate := append(append([]SliceElementDisposition(nil), elements...), elements[0])
	if err := ValidateDispositions(duplicate); !errors.Is(err, ErrInvalidDisposition) {
		t.Fatalf("duplicate accepted: %v", err)
	}
}
