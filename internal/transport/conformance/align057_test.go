package conformance_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/conformance"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// TestTodo_ALIGN_057 proves SSR and enhanced-browser semantic parity: both
// surfaces project the same semantic envelope, and the full qualified set
// holds parity with no hidden subject anywhere.
func TestTodo_ALIGN_057(t *testing.T) {
	envelope := execute(t, true)
	ssr, err := conformance.ProjectSurface(conformance.SurfaceSSR, envelope)
	if err != nil {
		t.Fatalf("ProjectSurface(SSR): %v", err)
	}
	enhanced, err := conformance.ProjectSurface(conformance.SurfaceEnhancedBrowser, envelope)
	if err != nil {
		t.Fatalf("ProjectSurface(ENHANCED_BROWSER): %v", err)
	}
	if err := conformance.AssertParity(ssr, enhanced); err != nil {
		t.Fatalf("SSR/enhanced-browser parity: %v", err)
	}
	if err := conformance.AssertAllParity(envelope, []values.EntityRef{subject(hiddenID)}); err != nil {
		t.Fatalf("AssertAllParity: %v", err)
	}
}

func TestTodo_ALIGN_057_Property(t *testing.T) {
	first := execute(t, true)
	reversed, err := conformance.Execute(context.Background(), request(t, true, true))
	if err != nil {
		t.Fatalf("Execute(reversed): %v", err)
	}
	if first.SemanticDigest != reversed.SemanticDigest {
		t.Fatalf("reordering changed semantic digest: %s != %s", first.SemanticDigest, reversed.SemanticDigest)
	}
	left, err := conformance.ProjectSurface(conformance.SurfaceEnhancedBrowser, first)
	if err != nil {
		t.Fatal(err)
	}
	right, err := conformance.ProjectSurface(conformance.SurfaceEnhancedBrowser, reversed)
	if err != nil {
		t.Fatal(err)
	}
	if err := conformance.AssertParity(left, right); err != nil {
		t.Fatalf("enhanced-browser stability: %v", err)
	}
}

func TestTodo_ALIGN_057_Golden(t *testing.T) {
	envelope := execute(t, false)
	const wantDigest = "sha256:5acc94653476d1d4f71ed85581770ac8741d5f8c05ba63932cc9b2cccabafbc2"
	if envelope.SemanticDigest != wantDigest {
		t.Fatalf("semantic digest=%q want=%q", envelope.SemanticDigest, wantDigest)
	}
}

func TestTodo_ALIGN_057_Security(t *testing.T) {
	envelope := execute(t, true)
	// A tampered enhanced-browser projection breaks parity.
	enhanced, err := conformance.ProjectSurface(conformance.SurfaceEnhancedBrowser, envelope)
	if err != nil {
		t.Fatal(err)
	}
	ssr, err := conformance.ProjectSurface(conformance.SurfaceSSR, envelope)
	if err != nil {
		t.Fatal(err)
	}
	enhanced.SemanticDigest = "sha256:tampered"
	if err := conformance.AssertParity(ssr, enhanced); !errors.Is(err, conformance.ErrParityMismatch) {
		t.Fatalf("tampered parity = %v, want ErrParityMismatch", err)
	}
	// A hidden subject smuggled onto the enhanced surface is caught.
	leaked := envelope.Rows[0]
	leaked.Subject = subject(hiddenID)
	smuggled := []conformance.SurfaceProjection{ssr, enhanced}
	smuggled[1].SemanticDigest = ssr.SemanticDigest
	smuggled[1].Envelope.Rows = append(append([]authz.Projection(nil), smuggled[1].Envelope.Rows...), leaked)
	if err := conformance.CheckNoninterference(smuggled, []values.EntityRef{subject(hiddenID)}); !errors.Is(err, conformance.ErrParityMismatch) {
		t.Fatalf("smuggled hidden subject = %v, want ErrParityMismatch", err)
	}
	// Surfaces outside the closed set stay invalid.
	for _, surface := range []conformance.Surface{0, 99} {
		if _, err := conformance.ProjectSurface(surface, envelope); !errors.Is(err, conformance.ErrInvalidSurface) {
			t.Fatalf("ProjectSurface(%d) = %v, want ErrInvalidSurface", surface, err)
		}
	}
}

func TestTodo_ALIGN_057_Integration(t *testing.T) {
	envelope := execute(t, true)
	triad := []conformance.Surface{conformance.SurfaceSSR, conformance.SurfaceBrowser, conformance.SurfaceEnhancedBrowser}
	projections := make([]conformance.SurfaceProjection, 0, len(triad))
	for _, surface := range triad {
		projection, err := conformance.ProjectSurface(surface, envelope)
		if err != nil {
			t.Fatalf("ProjectSurface(%s): %v", surface, err)
		}
		projections = append(projections, projection)
	}
	if err := conformance.CheckNoninterference(projections, []values.EntityRef{subject(hiddenID)}); err != nil {
		t.Fatalf("SSR/browser/enhanced noninterference: %v", err)
	}
}

func TestTodo_ALIGN_057_Fault(t *testing.T) {
	envelope := execute(t, false)
	left, err := conformance.ProjectSurface(conformance.SurfaceSSR, envelope)
	if err != nil {
		t.Fatal(err)
	}
	right, err := conformance.ProjectSurface(conformance.SurfaceEnhancedBrowser, envelope)
	if err != nil {
		t.Fatal(err)
	}
	right.SemanticDigest = "sha256:fault"
	if err := conformance.AssertParity(left, right); !errors.Is(err, conformance.ErrParityMismatch) {
		t.Fatalf("faulted parity = %v, want ErrParityMismatch", err)
	}
	// One surface is not a parity proof.
	if err := conformance.CheckNoninterference([]conformance.SurfaceProjection{left}, nil); !errors.Is(err, conformance.ErrParityMismatch) {
		t.Fatalf("single-surface check = %v, want ErrParityMismatch", err)
	}
}

func TestTodo_ALIGN_057_Conformance(t *testing.T) {
	envelope := execute(t, false)
	// Every qualified surface projects, with stable spellings.
	surfaces := map[conformance.Surface]string{
		conformance.SurfaceSSR:             "SSR",
		conformance.SurfaceBrowser:         "BROWSER",
		conformance.SurfaceEnhancedBrowser: "ENHANCED_BROWSER",
		conformance.SurfaceRPC:             "RPC",
		conformance.SurfaceExport:          "EXPORT",
	}
	for surface, spelling := range surfaces {
		if surface.String() != spelling {
			t.Fatalf("surface %d spells %q, want %q", surface, surface.String(), spelling)
		}
		if _, err := conformance.ProjectSurface(surface, envelope); err != nil {
			t.Fatalf("ProjectSurface(%s): %v", surface, err)
		}
	}
}
