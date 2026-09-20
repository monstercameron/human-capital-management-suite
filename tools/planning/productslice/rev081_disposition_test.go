package productslice

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// RED for REV-081-01: the disposition vocabulary is a different
// closed set from the one the governing spec defines. The code
// implements CORE, OPTIONAL, DEFERRED, EXCLUDED and PARTNER_ONLY
// while planning/specs/default-product-slice-alignment.md
// defines CORE_REQUIRED, DOMAIN_PACK_DEFAULT,
// AVAILABLE_NOT_ENABLED, CUSTOMER_DEFINED, DEFERRED and
// PROHIBITED, so the ticked item does not define "the default
// product disposition vocabulary" the contract names.
func TestTodo_REV_081_01(t *testing.T) {
	codes := map[ProductDisposition]bool{}
	for _, definition := range DefaultProductDispositionVocabulary() {
		codes[definition.Code] = true
	}
	for _, want := range []ProductDisposition{
		DispositionCoreRequired, DispositionDomainPackDefault,
		DispositionAvailableNotEnabled, DispositionCustomerDefined,
		DispositionDeferred, DispositionProhibited,
	} {
		if !codes[want] {
			t.Fatalf("vocabulary lacks spec code %q", want)
		}
	}
	if len(codes) != 6 {
		t.Fatalf("vocabulary holds %d codes, want the spec six", len(codes))
	}
	for _, retired := range []ProductDisposition{"CORE", "OPTIONAL", "EXCLUDED", "PARTNER_ONLY"} {
		if codes[retired] {
			t.Fatalf("vocabulary still carries retired code %q", retired)
		}
	}
}

// specDispositionVocabulary is the literal transcription of the
// default_disposition list in
// planning/specs/default-product-slice-alignment.md:197-203.
// The conformance test below fails the build if the code and
// this transcription ever diverge again.
var specDispositionVocabulary = []DispositionDefinition{
	{Code: "CORE_REQUIRED", Definition: "Required for every supported installation."},
	{Code: "DOMAIN_PACK_DEFAULT", Definition: "Installed and enabled with an admitted domain pack."},
	{Code: "AVAILABLE_NOT_ENABLED", Definition: "Shipped but requires explicit customer activation."},
	{Code: "CUSTOMER_DEFINED", Definition: "Governed customer configuration using platform parts."},
	{Code: "DEFERRED", Definition: "Not present in a production release."},
	{Code: "PROHIBITED", Definition: "Not present in a production release."},
}

// Conformance: the vocabulary codes and their one-line
// definitions match the spec transcription exactly, in order.
func TestTodo_REV_081_01_Conformance(t *testing.T) {
	live := DefaultProductDispositionVocabulary()
	if len(live) != len(specDispositionVocabulary) {
		t.Fatalf("vocabulary holds %d entries, spec lists %d", len(live), len(specDispositionVocabulary))
	}
	for i, want := range specDispositionVocabulary {
		if live[i].Code != want.Code || live[i].Definition != want.Definition {
			t.Fatalf("entry %d = %+v, spec transcribes %+v", i, live[i], want)
		}
	}
}

// Golden: the spec vocabulary digest.
func TestTodo_REV_081_01_Golden(t *testing.T) {
	var builder strings.Builder
	for _, definition := range DefaultProductDispositionVocabulary() {
		builder.WriteString(string(definition.Code) + "\x00" + definition.Definition + "\x00\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "75363bea2a1d398f5d61c9344f03756c9613de17a87ba4ce4be2c987b94b609f"
	if got != want {
		t.Fatalf("disposition vocabulary digest = %s, want %s", got, want)
	}
}
