package defaultproduct

import (
	"errors"
	"testing"
)

// TestTodo_ALIGN_046 proves zero-override product usability: the default
// composition assembles and validates with no overrides, and overrides
// that would break usability are refused.
func TestTodo_ALIGN_046(t *testing.T) {
	composition, err := DefaultComposition(alignAllCapabilities)
	if err != nil {
		t.Fatalf("DefaultComposition: %v", err)
	}
	if err := composition.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(composition.Shell.Entries) != len(DefaultShellEntries()) {
		t.Fatalf("shell renders %d entries with zero overrides", len(composition.Shell.Entries))
	}
	// A label override keeps the product usable.
	renamed, err := composition.WithOverride(CompositionOverride{ShellEntryID: "shell.nav.home", Label: "Start"})
	if err != nil {
		t.Fatalf("WithOverride(label): %v", err)
	}
	if renamed.Shell.Entries[0].Label != "Start" {
		t.Fatalf("override did not apply: %+v", renamed.Shell.Entries[0])
	}
	if err := renamed.Validate(); err != nil {
		t.Fatalf("Validate(renamed): %v", err)
	}
}

func TestTodo_ALIGN_046_Property(t *testing.T) {
	first, err := DefaultComposition(alignAllCapabilities)
	if err != nil {
		t.Fatal(err)
	}
	second, err := DefaultComposition(alignAllCapabilities)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("composition is not deterministic: %s != %s", first.Digest, second.Digest)
	}
}

func TestTodo_ALIGN_046_Golden(t *testing.T) {
	composition, err := DefaultComposition(alignAllCapabilities)
	if err != nil {
		t.Fatalf("DefaultComposition: %v", err)
	}
	const wantDigest = "sha256:ff8c6e275615ccbf7ed0316181fe3392d7b458c393ff4b26c4e05184b31c4a80"
	if composition.Digest != wantDigest {
		t.Fatalf("composition digest=%q want=%q", composition.Digest, wantDigest)
	}
}

func TestTodo_ALIGN_046_Security(t *testing.T) {
	// No capabilities means no usable product: refused, never an empty
	// shell presented as success.
	if _, err := DefaultComposition(nil); !errors.Is(err, ErrCompositionInvalid) {
		t.Fatalf("DefaultComposition(nil) = %v, want ErrCompositionInvalid", err)
	}
	composition, err := DefaultComposition(alignAllCapabilities)
	if err != nil {
		t.Fatal(err)
	}
	// An override pointing a route at an unseeded page breaks usability:
	// refused.
	if _, err := composition.WithOverride(CompositionOverride{RoutePath: "/promotion", PageID: "attacker.page"}); !errors.Is(err, ErrCompositionInvalid) {
		t.Fatalf("WithOverride(ghost page) = %v, want ErrCompositionInvalid", err)
	}
	// An override naming an unrendered shell entry is refused.
	if _, err := composition.WithOverride(CompositionOverride{ShellEntryID: "shell.nav.ghost", Label: "Ghost"}); !errors.Is(err, ErrCompositionInvalid) {
		t.Fatalf("WithOverride(ghost entry) = %v, want ErrCompositionInvalid", err)
	}
	// An override naming an unregistered route is refused.
	if _, err := composition.WithOverride(CompositionOverride{RoutePath: "/ghost", PageID: "promotion.list.page"}); !errors.Is(err, ErrCompositionInvalid) {
		t.Fatalf("WithOverride(ghost route) = %v, want ErrCompositionInvalid", err)
	}
}

func TestTodo_ALIGN_046_Conformance(t *testing.T) {
	composition, err := DefaultComposition(alignAllCapabilities)
	if err != nil {
		t.Fatal(err)
	}
	// Every default route resolves under the composition's capabilities:
	// the shipped product navigates everywhere it links.
	for _, route := range composition.Routes.Routes {
		resolved, err := composition.Routes.Resolve(route.Path, composition.Capabilities)
		if err != nil {
			t.Fatalf("composition route %q does not resolve: %v", route.Path, err)
		}
		if resolved.PageID != route.PageID {
			t.Fatalf("composition route %q drifted to %q", route.Path, resolved.PageID)
		}
	}
	// The composition pins the exact token seed it was built from.
	if composition.TokensDigest != DefaultTokens().Digest {
		t.Fatal("composition token pin drifted from the seed")
	}
}

func FuzzTodo_ALIGN_046_Fuzz(f *testing.F) {
	f.Add("shell.nav.home", "Start")
	f.Fuzz(func(t *testing.T, entryID, label string) {
		composition, err := DefaultComposition(alignAllCapabilities)
		if err != nil {
			t.Fatal(err)
		}
		first, firstErr := composition.WithOverride(CompositionOverride{ShellEntryID: entryID, Label: label})
		second, secondErr := composition.WithOverride(CompositionOverride{ShellEntryID: entryID, Label: label})
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("override is not deterministic for %q", entryID)
		}
		if firstErr == nil && first.Digest != second.Digest {
			t.Fatalf("override digest is not deterministic for %q", entryID)
		}
	})
}
