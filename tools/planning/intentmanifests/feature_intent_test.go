package intentmanifests

import (
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func featureIntakePath() string {
	return filepath.Join("..", "..", "..", "definitions", "governance", "feature-intent-intake.yaml")
}

func loadFeatureIntake(t *testing.T) FeatureManifest {
	t.Helper()
	manifest, err := LoadFeatureIntakeYAML(featureIntakePath())
	if err != nil {
		t.Fatalf("load feature intake: %v", err)
	}
	return manifest
}

// TestFeatureIntentSourceManifest is the PRIMARY test for FEATURE-001. It pins
// group order, uniqueness, provenance, full mapping resolution, and the intake digest.
func TestFeatureIntentSourceManifest(t *testing.T) {
	manifest := loadFeatureIntake(t)
	if err := ValidateFeatureIntakeYAML(manifest); err != nil {
		t.Fatalf("validate complete feature intake: %v", err)
	}
	if got := len(manifest.Groups); got != 49 {
		t.Fatalf("groups=%d, want 49", got)
	}

	descriptors, err := LoadIntentManifestYAML(filepath.Join("..", "..", "..", "definitions", "governance", "intent-conformance-descriptors.yaml"))
	if err != nil {
		t.Fatalf("load intent catalog: %v", err)
	}
	known := make(map[string]bool, len(descriptors))
	for _, descriptor := range descriptors {
		known[descriptor.IntentTypeID+"/v"+strconv.Itoa(descriptor.Version)] = true
	}
	featureCount := 0
	for _, group := range manifest.Groups {
		for _, feature := range group.Features {
			featureCount++
			if feature.MappedIntentID == "DEFERRED" || feature.MappedIntentID == "MISSING" {
				continue
			}
			for _, reference := range strings.Split(feature.MappedIntentID, ",") {
				reference = strings.TrimSpace(reference)
				if !known[reference] {
					t.Errorf("group %d feature %s maps to unresolved intent %q", group.GroupID, feature.FeatureID, reference)
				}
			}
		}
	}
	if featureCount != 253 {
		t.Fatalf("feature count=%d, want 253", featureCount)
	}
}

// TestTodo_FEATURE_001_Golden pins all group labels, feature labels, categories,
// mappings, provenance and ordering through the canonical digest.
func TestTodo_FEATURE_001_Golden(t *testing.T) {
	manifest := loadFeatureIntake(t)
	digest, err := ComputeFeatureDigestYAML(manifest.Groups)
	if err != nil {
		t.Fatalf("compute canonical group digest: %v", err)
	}
	const goldenDigest = "34628df74aed22c2245ec62675e9f5778a4a139120bb9ae7cb9c769a68373285"
	if digest != goldenDigest {
		t.Fatalf("feature intake digest=%q, want %q", digest, goldenDigest)
	}
	if manifest.SourceManifestDigest != goldenDigest {
		t.Fatalf("declared source digest=%q, want %q", manifest.SourceManifestDigest, goldenDigest)
	}
}

// TestTodo_FEATURE_001_Race verifies concurrent loads produce the same validated intake.
func TestTodo_FEATURE_001_Race(t *testing.T) {
	const workers = 12
	type result struct {
		groups int
		digest string
		err    error
	}
	start := make(chan struct{})
	results := make(chan result, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			manifest, err := LoadFeatureIntakeYAML(featureIntakePath())
			if err == nil {
				err = ValidateFeatureIntakeYAML(manifest)
			}
			if err != nil {
				results <- result{err: err}
				return
			}
			digest, err := ComputeFeatureDigestYAML(manifest.Groups)
			results <- result{groups: len(manifest.Groups), digest: digest, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var want string
	for got := range results {
		if got.err != nil {
			t.Fatalf("concurrent intake load: %v", got.err)
		}
		if got.groups != 49 || len(got.digest) != 64 {
			t.Fatalf("concurrent result groups=%d digest=%q", got.groups, got.digest)
		}
		if want == "" {
			want = got.digest
		} else if got.digest != want {
			t.Fatalf("concurrent digest=%q, want %q", got.digest, want)
		}
	}
}

// TestTodo_FEATURE_001_Fault rejects missing, reordered, duplicated, and malformed intake data.
func TestTodo_FEATURE_001_Fault(t *testing.T) {
	base := loadFeatureIntake(t)
	t.Run("missing group", func(t *testing.T) {
		bad := base
		bad.Groups = append([]FeatureGroup(nil), base.Groups[:48]...)
		if err := ValidateFeatureIntakeYAML(bad); err == nil {
			t.Fatal("missing group accepted")
		}
	})
	t.Run("reordered group", func(t *testing.T) {
		bad := base
		bad.Groups = append([]FeatureGroup(nil), base.Groups...)
		bad.Groups[0], bad.Groups[1] = bad.Groups[1], bad.Groups[0]
		if err := ValidateFeatureManifestYAML(bad.Groups); err == nil {
			t.Fatal("reordered groups accepted")
		}
	})
	t.Run("duplicate feature across groups", func(t *testing.T) {
		bad := base
		bad.Groups = append([]FeatureGroup(nil), base.Groups...)
		bad.Groups[1].Features = append(append([]Feature(nil), bad.Groups[1].Features...), bad.Groups[0].Features[0])
		if err := ValidateFeatureManifestYAML(bad.Groups); err == nil {
			t.Fatal("cross-group duplicate feature accepted")
		}
	})
	t.Run("missing provenance", func(t *testing.T) {
		bad := base
		bad.Groups = append([]FeatureGroup(nil), base.Groups...)
		bad.Groups[0].SourceProvenance = ""
		if err := ValidateFeatureManifestYAML(bad.Groups); err == nil {
			t.Fatal("missing provenance accepted")
		}
	})
	t.Run("bad mapping", func(t *testing.T) {
		bad := base
		bad.Groups = append([]FeatureGroup(nil), base.Groups...)
		bad.Groups[0].Features = append([]Feature(nil), base.Groups[0].Features...)
		bad.Groups[0].Features[0].MappedIntentID = "not-an-intent"
		if err := ValidateFeatureManifestYAML(bad.Groups); err == nil {
			t.Fatal("malformed feature mapping accepted")
		}
	})
	t.Run("digest mismatch", func(t *testing.T) {
		bad := base
		bad.SourceManifestDigest = strings.Repeat("0", 64)
		if err := ValidateFeatureIntakeYAML(bad); err == nil {
			t.Fatal("mismatched manifest digest accepted")
		}
	})
}
