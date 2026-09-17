package workspace

// UXAUDIT-022 PRIMARY: replace appearance asset paths with a governed brand
// asset picker.
//
// RED: Appearance asks an administrator to type a logo path, does not preview
// light/dark and compact variants, or places governed Save beyond the working
// context.
//
// GREEN: an upload and authorized asset picker validates type, dimensions and
// safety; retains the original plus responsive proxies; previews shell,
// favicon and contrast variants; supports removal and rollback; and keeps the
// save/publish action visible with an exact diff.

import (
	"strings"
	"testing"
)

// TestTodo_UXAUDIT_022 proves the full governed picker lifecycle: a validated
// upload retains its original and proxy identities, serves only content-derived
// preview variants (shell, favicon, compact, contrast), carries an exact
// save/publish diff between revisions, and supports removal and rollback
// without ever exposing a typed filesystem path.
func TestTodo_UXAUDIT_022(t *testing.T) {
	first, err := ValidateBrandAsset(BrandAssetUpload{TenantID: "tenant-a", Name: "logo.png", Bytes: brandPNG(t, 320, 120)})
	if err != nil {
		t.Fatalf("validate v1: %v", err)
	}
	store := NewBrandAssetRevisionStore()
	v1, version, err := store.Save(0, first)
	if err != nil || version != 1 {
		t.Fatalf("save v1: version=%d err=%v", version, err)
	}

	variants := PreviewAssetVariants(v1)
	wantVariants := map[string]bool{"shell": false, "favicon": false, "compact": false, "contrast-light": false, "contrast-dark": false}
	for _, variant := range variants {
		if _, ok := wantVariants[variant.Name]; !ok {
			t.Fatalf("unexpected preview variant %q", variant.Name)
		}
		wantVariants[variant.Name] = true
		if variant.Original != v1.Original || variant.Proxy != v1.Proxy {
			t.Fatalf("variant %q does not retain original/proxy identities: %+v", variant.Name, variant)
		}
	}
	for name, seen := range wantVariants {
		if !seen {
			t.Fatalf("missing preview variant %q", name)
		}
	}

	second, err := ValidateBrandAsset(BrandAssetUpload{TenantID: "tenant-a", Name: "logo.png", Bytes: brandPNG(t, 640, 240)})
	if err != nil {
		t.Fatalf("validate v2: %v", err)
	}
	v2, version, err := store.Save(1, second)
	if err != nil || version != 2 {
		t.Fatalf("save v2: version=%d err=%v", version, err)
	}
	diff := DiffBrandAssets(v1, v2)
	if len(diff) == 0 {
		t.Fatal("expected an exact save/publish diff between v1 and v2, got none")
	}
	fields := map[string]bool{}
	for _, change := range diff {
		if change.From == change.To || strings.TrimSpace(change.Field) == "" {
			t.Fatalf("diff entry is not exact: %+v", change)
		}
		fields[change.Field] = true
	}
	for _, field := range []string{"digest", "original", "proxy", "width", "height"} {
		if !fields[field] {
			t.Fatalf("diff omits changed field %q: %+v", field, diff)
		}
	}

	restored, version, err := store.Rollback("tenant-a", 2, 1)
	if err != nil || version != 3 {
		t.Fatalf("rollback: version=%d err=%v", version, err)
	}
	if remaining := DiffBrandAssets(v1, restored); len(remaining) != 0 {
		t.Fatalf("rollback did not restore v1 exactly: %+v", remaining)
	}

	if version, err = store.Remove("tenant-a", 3); err != nil || version != 4 {
		t.Fatalf("remove: version=%d err=%v", version, err)
	}
	removed, _, ok := store.Current("tenant-a")
	if !ok || removed.Digest != "" {
		t.Fatalf("remove did not create an explicit empty revision: %+v", removed)
	}

	// The picker never serves a typed path: served identities are
	// content-derived and never echo the uploader-supplied filename.
	for _, served := range []string{v1.Original, v1.Proxy, v2.Original, v2.Proxy} {
		if served == "logo.png" || strings.ContainsAny(served, `/\`) {
			t.Fatalf("served identity looks like a typed path: %q", served)
		}
	}
}

// TestTodo_UXAUDIT_022_Regression pins the content-derived identity rule the
// picker depends on: the same bytes under different filenames resolve to one
// digest and one served identity, and removal preserves the revision history
// a rollback needs.
func TestTodo_UXAUDIT_022_Regression(t *testing.T) {
	body := brandPNG(t, 200, 100)
	a, err := ValidateBrandAsset(BrandAssetUpload{TenantID: "tenant-a", Name: "logo.png", Bytes: body})
	if err != nil {
		t.Fatal(err)
	}
	b, err := ValidateBrandAsset(BrandAssetUpload{TenantID: "tenant-a", Name: "brand.png", Bytes: body})
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest || a.Original != b.Original || a.Proxy != b.Proxy {
		t.Fatalf("same bytes produced different identities: %+v vs %+v", a, b)
	}
	store := NewBrandAssetRevisionStore()
	if _, _, err := store.Save(0, a); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Remove("tenant-a", 1); err != nil {
		t.Fatal(err)
	}
	restored, _, err := store.Rollback("tenant-a", 2, 1)
	if err != nil {
		t.Fatalf("history lost across removal: %v", err)
	}
	if restored.Digest != a.Digest {
		t.Fatalf("rollback after removal restored %+v, want digest %s", restored, a.Digest)
	}
}
