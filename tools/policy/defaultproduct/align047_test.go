package defaultproduct

import (
	"errors"
	"testing"
)

// TestTodo_ALIGN_047 proves domain-pack activation manifests compile and
// activate: admitted features activate, unadmitted features are refused by
// name, and unmanifested features are unknown.
func TestTodo_ALIGN_047(t *testing.T) {
	manifest, err := Compile(DefaultPacks())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if err := manifest.VerifyDigest(); err != nil {
		t.Fatalf("VerifyDigest: %v", err)
	}
	activated, err := manifest.Activate([]string{"promotion.list", "promotion.execute"}, alignAllCapabilities)
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if len(activated) != 1 || activated[0].ID != "promotion.pack" || len(activated[0].Features) != 2 {
		t.Fatalf("activated = %+v", activated)
	}
	// An unadmitted feature is refused by name, never silently skipped.
	if _, err := manifest.Activate([]string{"operations.repair"}, []string{"promotion.view"}); !errors.Is(err, ErrActivationRefused) {
		t.Fatalf("Activate(unadmitted) = %v, want ErrActivationRefused", err)
	}
	// An unmanifested feature is unknown.
	if _, err := manifest.Activate([]string{"payroll.forge"}, alignAllCapabilities); !errors.Is(err, ErrActivationUnknown) {
		t.Fatalf("Activate(unknown) = %v, want ErrActivationUnknown", err)
	}
}

func TestTodo_ALIGN_047_Property(t *testing.T) {
	first, err := Compile(DefaultPacks())
	if err != nil {
		t.Fatal(err)
	}
	second, err := Compile(DefaultPacks())
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("manifest compile is not deterministic: %s != %s", first.Digest, second.Digest)
	}
	// Activation order follows the manifest, not the request.
	ordered, err := first.Activate([]string{"promotion.execute", "promotion.list"}, alignAllCapabilities)
	if err != nil {
		t.Fatal(err)
	}
	if len(ordered[0].Features) != 2 || ordered[0].Features[0] != "promotion.execute" {
		t.Fatalf("activation order = %+v, want request order within the pack", ordered)
	}
}

func TestTodo_ALIGN_047_Golden(t *testing.T) {
	manifest, err := Compile(DefaultPacks())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	const wantDigest = "sha256:898deefdf490757bb0210df2ca5be5699b326f96972f481be85a6b83da50eeb3"
	if manifest.Digest != wantDigest {
		t.Fatalf("manifest digest=%q want=%q", manifest.Digest, wantDigest)
	}
}

func TestTodo_ALIGN_047_Security(t *testing.T) {
	manifest, err := Compile(DefaultPacks())
	if err != nil {
		t.Fatal(err)
	}
	// A manifest edited after compile no longer verifies and cannot
	// activate: the digest is the admission.
	tampered := manifest
	tampered.Packs = append(append([]DomainPack(nil), manifest.Packs...), DomainPack{
		ID: "evil.pack", Version: "1",
		Features: []PackFeature{{ID: "evil.feature", Capability: "evil.capability", Disposition: "default"}},
	})
	if err := tampered.VerifyDigest(); err == nil {
		t.Fatal("tampered manifest verified")
	}
	if _, err := tampered.Activate([]string{"evil.feature"}, []string{"evil.capability"}); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("Activate(tampered) = %v, want ErrManifestInvalid", err)
	}
	// Duplicate features across packs are refused at compile.
	duplicated := DefaultPacks()
	duplicated[1].Features = append(duplicated[1].Features, PackFeature{ID: "promotion.list", Capability: "operations.repair", Disposition: "default"})
	if _, err := Compile(duplicated); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("Compile(duplicate) = %v, want ErrManifestInvalid", err)
	}
	// A feature without a capability is refused at compile.
	capabilityless := DefaultPacks()
	capabilityless[0].Features[0].Capability = ""
	if _, err := Compile(capabilityless); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("Compile(capabilityless) = %v, want ErrManifestInvalid", err)
	}
}

func TestTodo_ALIGN_047_Conformance(t *testing.T) {
	manifest, err := Compile(DefaultPacks())
	if err != nil {
		t.Fatal(err)
	}
	// Every manifested feature carries a capability: activation never
	// faces a feature it cannot judge.
	for _, pack := range manifest.Packs {
		for _, feature := range pack.Features {
			if !safeID(feature.Capability) {
				t.Fatalf("feature %q has no capability", feature.ID)
			}
		}
	}
	// Activating everything admitted admits exactly the manifested set
	// the grants cover: no more, no fewer.
	all := []string{"promotion.list", "promotion.execute", "operations.repair"}
	activated, err := manifest.Activate(all, alignAllCapabilities)
	if err != nil {
		t.Fatalf("Activate(all): %v", err)
	}
	count := 0
	for _, pack := range activated {
		count += len(pack.Features)
	}
	if count != len(all) {
		t.Fatalf("activated %d of %d manifested features", count, len(all))
	}
}

func FuzzTodo_ALIGN_047_Fuzz(f *testing.F) {
	f.Add("promotion.list", "promotion.view")
	f.Fuzz(func(t *testing.T, feature, capability string) {
		manifest, err := Compile(DefaultPacks())
		if err != nil {
			t.Fatal(err)
		}
		first, firstErr := manifest.Activate([]string{feature}, []string{capability})
		second, secondErr := manifest.Activate([]string{feature}, []string{capability})
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("activation is not deterministic for %q", feature)
		}
		if firstErr == nil && len(first) != len(second) {
			t.Fatalf("activation diverged for %q", feature)
		}
	})
}
