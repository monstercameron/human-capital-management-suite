package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
)

func TestIngestDemoPhotosUsesPrivateOriginalsAndPublicProxies(t *testing.T) {
	t.Parallel()

	sourceDir, assetDir, originalDir := t.TempDir(), t.TempDir(), t.TempDir()
	content := demoPNG(t)
	if err := os.WriteFile(filepath.Join(sourceDir, "hc-001.png"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	employees := []demoworkforce.Employee{
		{Row: workforce.WorkerRow{WorkerKey: "hc-001-test"}, HasProfilePhoto: true, PhotoSourceName: "hc-001.png", PhotoOriginalRef: "profile-originals/hc-001.png", PhotoProxyRef: "/workspace/assets/person-hc-001-small.jpg"},
		{HasProfilePhoto: false},
	}
	if err := ingestDemoPhotos(context.Background(), employees, sourceDir, assetDir, originalDir); err != nil {
		t.Fatal(err)
	}
	retained, err := os.ReadFile(filepath.Join(originalDir, "hc-001.png"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(retained, content) {
		t.Fatal("ingestion did not preserve the byte-exact original")
	}
	if info, err := os.Stat(filepath.Join(assetDir, "person-hc-001-small.jpg")); err != nil || info.Size() == 0 {
		t.Fatalf("proxy was not written: info=%v err=%v", info, err)
	}
}

// TestIngestDemoPhotosSkipsAlreadyPublishedProxy proves the PROMOUX-001
// seeding-path fix: a proxy already published under its target name is left
// alone rather than re-derived and byte-compared, so a source photo that has
// legitimately drifted (retouch, recompression) since the proxy was
// committed does not turn every re-run of `migrate demo-people` into a hard
// failure. Without this, ingestDemoPhotos would call profilephoto.Upload,
// which would in turn hit FileStore.atomicWrite's fail-closed "already
// exists with different content" error the moment the freshly computed JPEG
// bytes disagree with an already-seeded development proxy.
func TestIngestDemoPhotosSkipsAlreadyPublishedProxy(t *testing.T) {
	t.Parallel()

	sourceDir, assetDir, originalDir := t.TempDir(), t.TempDir(), t.TempDir()
	// A source photo that would NOT reproduce the existing proxy's bytes: a
	// single flat color the pipeline could never have produced (the
	// existing proxy is a fixed, unrelated marker string). If
	// ingestDemoPhotos attempted to regenerate and byte-compare, this would
	// fail with FileStore's "already exists with different content" error.
	if err := os.WriteFile(filepath.Join(sourceDir, "hc-002.png"), demoPNG(t), 0o644); err != nil {
		t.Fatal(err)
	}
	proxyPath := filepath.Join(assetDir, "person-hc-002-small.jpg")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	staleContent := []byte("stale-tracked-proxy-bytes-unrelated-to-any-real-jpeg")
	if err := os.WriteFile(proxyPath, staleContent, 0o644); err != nil {
		t.Fatal(err)
	}
	employees := []demoworkforce.Employee{
		{Row: workforce.WorkerRow{WorkerKey: "hc-002-test"}, HasProfilePhoto: true, PhotoSourceName: "hc-002.png",
			PhotoOriginalRef: "profile-originals/hc-002.png", PhotoProxyRef: "/workspace/assets/person-hc-002-small.jpg"},
	}
	if err := ingestDemoPhotos(context.Background(), employees, sourceDir, assetDir, originalDir); err != nil {
		t.Fatalf("ingestion of an already-published proxy must not fail: %v", err)
	}
	after, err := os.ReadFile(proxyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, staleContent) {
		t.Fatal("already-published proxy was overwritten instead of left alone")
	}
	// The retained original is a private, non-served path this test does not
	// assert on: the point under test is the proxy, which is the file the
	// FileStore's collision guard fails closed on.
	if err := ingestDemoPhotos(context.Background(), employees, sourceDir, assetDir, originalDir); err != nil {
		t.Fatalf("re-running ingestion against the same already-published proxy must stay idempotent: %v", err)
	}
}

func TestIngestDemoPhotosRefusesAMissingSelectedSource(t *testing.T) {
	t.Parallel()

	err := ingestDemoPhotos(context.Background(), []demoworkforce.Employee{{
		HasProfilePhoto: true, PhotoSourceName: "hc-001.png",
	}}, t.TempDir(), t.TempDir(), t.TempDir())
	if err == nil {
		t.Fatal("missing selected source unexpectedly succeeded")
	}
}

func demoPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 240, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 240; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 90, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}
