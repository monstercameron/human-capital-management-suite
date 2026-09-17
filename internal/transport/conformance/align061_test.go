package conformance_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/conformance"
)

// TestTodo_ALIGN_061 proves product state after restart and restore: the
// canonical restart bytes restore to the identical validated envelope, and
// anything corrupted, forged, or version-skewed is refused.
func TestTodo_ALIGN_061(t *testing.T) {
	envelope := execute(t, false)
	raw, err := conformance.EncodeEnvelope(envelope)
	if err != nil {
		t.Fatalf("EncodeEnvelope: %v", err)
	}
	restored, err := conformance.DecodeEnvelope(raw)
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}
	if err := restored.Validate(); err != nil {
		t.Fatalf("restored Validate: %v", err)
	}
	if restored.SemanticDigest != envelope.SemanticDigest || len(restored.Rows) != len(envelope.Rows) {
		t.Fatalf("restored digest=%q rows=%d, want %q rows=%d",
			restored.SemanticDigest, len(restored.Rows), envelope.SemanticDigest, len(envelope.Rows))
	}
}

func TestTodo_ALIGN_061_Property(t *testing.T) {
	envelope := execute(t, false)
	first, err := conformance.EncodeEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	second, err := conformance.EncodeEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if conformance.RestartDigest(first) != conformance.RestartDigest(second) {
		t.Fatal("restart encoding is not deterministic")
	}
}

func TestTodo_ALIGN_061_Golden(t *testing.T) {
	envelope := execute(t, false)
	const wantDigest = "sha256:5acc94653476d1d4f71ed85581770ac8741d5f8c05ba63932cc9b2cccabafbc2"
	if envelope.SemanticDigest != wantDigest {
		t.Fatalf("semantic digest=%q want=%q", envelope.SemanticDigest, wantDigest)
	}
}

func TestTodo_ALIGN_061_Security(t *testing.T) {
	envelope := execute(t, false)
	raw, err := conformance.EncodeEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	// A corrupted payload is refused, never partially restored.
	corrupted := append([]byte(nil), raw...)
	corrupted[len(corrupted)/2] ^= 0xff
	if _, err := conformance.DecodeEnvelope(corrupted); err == nil {
		t.Fatal("corrupted restart bytes were restored")
	}
	// A version-skewed container is refused.
	var container map[string]any
	if err := json.Unmarshal(raw, &container); err != nil {
		t.Fatal(err)
	}
	container["version"] = 2
	skewed, err := json.Marshal(container)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conformance.DecodeEnvelope(skewed); !errors.Is(err, conformance.ErrInvalidQuery) {
		t.Fatalf("DecodeEnvelope(skewed) = %v, want ErrInvalidQuery", err)
	}
	// An unknown field is refused: the restore surface is closed.
	container["version"] = 1
	container["injected"] = "evil"
	extended, err := json.Marshal(container)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conformance.DecodeEnvelope(extended); !errors.Is(err, conformance.ErrInvalidQuery) {
		t.Fatalf("DecodeEnvelope(extended) = %v, want ErrInvalidQuery", err)
	}
}

func TestTodo_ALIGN_061_Integration(t *testing.T) {
	envelope := execute(t, false)
	raw, err := conformance.EncodeEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := conformance.DecodeEnvelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	// The restored envelope serves every surface with the same semantics
	// as the live one: restart changes the process, not the product.
	live, err := conformance.ProjectSurface(conformance.SurfaceSSR, envelope)
	if err != nil {
		t.Fatal(err)
	}
	reborn, err := conformance.ProjectSurface(conformance.SurfaceSSR, restored)
	if err != nil {
		t.Fatal(err)
	}
	if err := conformance.AssertParity(live, reborn); err != nil {
		t.Fatalf("restart parity: %v", err)
	}
}

func TestTodo_ALIGN_061_Fault(t *testing.T) {
	// Truncated, empty, and null payloads are refused.
	for _, raw := range [][]byte{nil, {}, []byte("null"), []byte("{")} {
		if _, err := conformance.DecodeEnvelope(raw); !errors.Is(err, conformance.ErrInvalidQuery) {
			t.Fatalf("DecodeEnvelope(%q) = %v, want ErrInvalidQuery", raw, err)
		}
	}
	// A forged digest on a valid envelope is refused.
	envelope := execute(t, false)
	raw, err := conformance.EncodeEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	var container map[string]any
	if err := json.Unmarshal(raw, &container); err != nil {
		t.Fatal(err)
	}
	container["digest"] = "sha256:forged"
	forged, err := json.Marshal(container)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conformance.DecodeEnvelope(forged); !errors.Is(err, conformance.ErrInvalidQuery) {
		t.Fatalf("DecodeEnvelope(forged) = %v, want ErrInvalidQuery", err)
	}
}

func TestTodo_ALIGN_061_Conformance(t *testing.T) {
	envelope := execute(t, true)
	raw, err := conformance.EncodeEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := conformance.DecodeEnvelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	// Restart preserves the authorization boundary: the hidden subject
	// stays absent and the digest stays identical.
	for _, row := range restored.Rows {
		if strings.Contains(row.Subject.String(), hiddenID) {
			t.Fatalf("hidden subject survived restart: %v", row.Subject)
		}
	}
	if err := conformance.AssertAllParity(restored, nil); err != nil {
		t.Fatalf("AssertAllParity(restored): %v", err)
	}
}

func FuzzTodo_ALIGN_061_Fuzz(f *testing.F) {
	f.Add([]byte("{}"))
	f.Fuzz(func(t *testing.T, raw []byte) {
		restored, err := conformance.DecodeEnvelope(raw)
		if err != nil {
			return
		}
		reencoded, err := conformance.EncodeEnvelope(restored)
		if err != nil {
			t.Fatalf("restored envelope does not re-encode: %v", err)
		}
		again, err := conformance.DecodeEnvelope(reencoded)
		if err != nil || again.SemanticDigest != restored.SemanticDigest {
			t.Fatalf("restart round trip diverged: %v", err)
		}
	})
}
