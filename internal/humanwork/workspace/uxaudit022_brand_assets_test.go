package workspace

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func brandPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 10, G: 90, B: 70, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestTodo_UXAUDIT_022_LifecycleUnit(t *testing.T) {
	asset, err := ValidateBrandAsset(BrandAssetUpload{TenantID: "tenant-a", Name: "logo.png", Bytes: brandPNG(t, 320, 120)})
	if err != nil {
		t.Fatal(err)
	}
	if asset.Width != 320 || asset.Height != 120 || asset.MediaType != "image/png" || asset.Original == "" || asset.Proxy == "" || asset.Digest == "" {
		t.Fatalf("validated asset lost original/proxy metadata: %+v", asset)
	}
	store := NewBrandAssetRevisionStore()
	if _, version, err := store.Save(0, asset); err != nil || version != 1 {
		t.Fatalf("save v1: version=%d err=%v", version, err)
	}
	if _, _, err := store.Save(0, asset); !errors.Is(err, ErrBrandAssetVersionConflict) {
		t.Fatalf("stale save err=%v", err)
	}
	current, version, ok := store.Current("tenant-a")
	if !ok || version != 1 || current.Digest != asset.Digest {
		t.Fatalf("current asset=%+v version=%d ok=%v", current, version, ok)
	}
	if version, err = store.Remove("tenant-a", version); err != nil || version != 2 {
		t.Fatalf("remove: version=%d err=%v", version, err)
	}
	removed, _, ok := store.Current("tenant-a")
	if !ok || removed.Digest != "" {
		t.Fatalf("remove did not create an explicit empty revision: %+v", removed)
	}
}

func TestTodo_UXAUDIT_022_SecurityValidationUnit(t *testing.T) {
	valid := brandPNG(t, 64, 64)
	cases := []struct {
		name, filename string
		body           []byte
		want           error
	}{
		{"traversal", "../logo.png", valid, ErrBrandAssetUnsafe},
		{"remote SVG rejected", "logo.svg", []byte(`<svg width="64" height="64"><image href="//evil.test/x"/></svg>`), ErrBrandAssetInvalidType},
		{"event SVG rejected", "logo.svg", []byte(`<svg width="64" height="64" onload="alert(1)"/>`), ErrBrandAssetInvalidType},
		{"external SVG rejected", "logo.svg", []byte(`<svg width="64" height="64"><image href="/same-origin/secret"/></svg>`), ErrBrandAssetInvalidType},
		{"wrong type", "logo.gif", valid, ErrBrandAssetInvalidType},
		{"too small", "logo.png", brandPNG(t, 8, 32), ErrBrandAssetInvalidDimensions},
		{"too large", "logo.png", brandPNG(t, BrandAssetMaxSide+1, 32), ErrBrandAssetInvalidDimensions},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateBrandAsset(BrandAssetUpload{TenantID: "tenant-a", Name: tc.filename, Bytes: tc.body})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
		})
	}
	asset, err := ValidateBrandAsset(BrandAssetUpload{TenantID: "tenant-a", Name: "logo.png", Bytes: valid})
	if err != nil {
		t.Fatal(err)
	}
	store := NewBrandAssetRevisionStore()
	if _, _, err = store.Save(0, asset); err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.Rollback("tenant-b", 0, 1); !errors.Is(err, ErrBrandAssetVersionConflict) {
		t.Fatalf("cross-tenant rollback err=%v", err)
	}
}
