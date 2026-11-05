package application

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/conformance"
)

// TestTodo_ALIGN_061_Served proves the restart/restore capability on the
// served path. The A-F reachability audit (2026-09-24) reopened ALIGN-061
// because internal/transport/conformance was absent from the shipped-command
// dependency closure; the served alignment surface must therefore expose
// the restart envelope round-trip so product state after restart and
// restore is proven through the same surface shipped commands compose.
func TestTodo_ALIGN_061_Served(t *testing.T) {
	surface := NewServedAlignmentSurface()
	if surface.EncodeRestartEnvelope == nil || surface.DecodeRestartEnvelope == nil || surface.RestartDigest == nil {
		t.Fatal("served alignment surface omitted restart contracts")
	}

	envelope := servedAlignmentEnvelope(t)
	raw, err := surface.EncodeRestartEnvelope(envelope)
	if err != nil {
		t.Fatalf("served restart encode: %v", err)
	}
	if digest := surface.RestartDigest(raw); digest == "" {
		t.Fatal("served restart digest is empty")
	}
	restored, err := surface.DecodeRestartEnvelope(raw)
	if err != nil {
		t.Fatalf("served restart decode: %v", err)
	}
	if restored.CanonicalDigest() != envelope.CanonicalDigest() {
		t.Fatalf("served restart digest = %q, want %q", restored.CanonicalDigest(), envelope.CanonicalDigest())
	}

	// A corrupted backup is refused on the served path too: it never
	// becomes product state.
	raw[len(raw)-1] ^= 0xff
	if _, err := surface.DecodeRestartEnvelope(raw); !errors.Is(err, conformance.ErrInvalidQuery) {
		t.Fatalf("served corrupt restart decode = %v, want ErrInvalidQuery", err)
	}
}
