package agentpersonastore

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

// agentUX069UploadPNG builds a real decodable PNG upload fixture.
func agentUX069UploadPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			canvas.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, canvas); err != nil {
		t.Fatal(err)
	}
	return raw.Bytes()
}

// An owner can upload their own image for an agent: it is re-encoded,
// stored with the agent's icon at a new revision, and served back.
func TestAgentUX069_UploadStoresImage(t *testing.T) {
	f, s := iconFixture(t)
	ctx := context.Background()
	original := createIconDraft(t, s, "upload")
	if original.HasImage {
		t.Fatal("generated icon claims an upload")
	}
	changed, err := s.SetIconImage(ctx, "upload", 1, "user:business-owner", f.when, nil, agentUX069UploadPNG(t, 200, 100), "image/png")
	if err != nil || changed.Revision != 2 || !changed.HasImage || changed.Value != original.Value {
		t.Fatal("upload", changed, err)
	}
	content, mediaType, revision, err := s.GetIconImage(ctx, "upload")
	if err != nil || mediaType != "image/jpeg" || revision != 2 || len(content) == 0 {
		t.Fatal("serve", len(content), mediaType, revision, err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil || format != "jpeg" || config.Width != 160 || config.Height != 160 {
		t.Fatalf("not a re-encoded square proxy: %dx%d %s %v", config.Width, config.Height, format, err)
	}
	if _, _, _, err = f.store(t, "icon-b").GetIconImage(ctx, "upload"); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross tenant image visible", err)
	}
}

// Oversized, unreadable, mismatched and non-image uploads are refused and
// leave the stored icon untouched.
func TestAgentUX069_UploadRefusesInvalid(t *testing.T) {
	f, s := iconFixture(t)
	ctx := context.Background()
	original := createIconDraft(t, s, "refuse")
	good := agentUX069UploadPNG(t, 200, 200)
	for name, tc := range map[string]struct {
		content []byte
		media   string
	}{
		"empty":          {nil, "image/png"},
		"oversize":       {bytes.Repeat([]byte{1}, 3<<20), "image/png"},
		"not an image":   {[]byte("hello, world"), "image/png"},
		"sniff mismatch": {good, "image/jpeg"},
		"gif refused":    {[]byte("GIF89a" + strings.Repeat("x", 100)), "image/gif"},
		"tiny":           {agentUX069UploadPNG(t, 8, 8), "image/png"},
	} {
		if _, err := s.SetIconImage(ctx, "refuse", 1, "user:business-owner", f.when, nil, tc.content, tc.media); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	if _, err := s.SetIconImage(ctx, "refuse", 1, "intruder", f.when, nil, good, "image/png"); !errors.Is(err, ErrIconDenied) {
		t.Fatal("unauthorized upload", err)
	}
	if stored, err := s.GetIcon(ctx, "refuse"); err != nil || stored != original {
		t.Fatal("refusal mutated state", stored, err)
	}
}

// Reset clears an uploaded image back to the generated icon, and undo
// restores the image that was there before.
func TestAgentUX069_UploadResetUndo(t *testing.T) {
	f, s := iconFixture(t)
	ctx := context.Background()
	original := createIconDraft(t, s, "cycle")
	good := agentUX069UploadPNG(t, 200, 200)
	if _, err := s.SetIconImage(ctx, "cycle", 1, "user:business-owner", f.when, nil, good, "image/png"); err != nil {
		t.Fatal(err)
	}
	reset, err := s.ChangeIcon(ctx, "cycle", 2, "reset", "user:business-owner", f.when, nil)
	if err != nil || reset.Value != original.Value || reset.HasImage || reset.Revision != 3 {
		t.Fatal("reset kept the upload", reset, err)
	}
	if _, _, _, err = s.GetIconImage(ctx, "cycle"); !errors.Is(err, ErrNotFound) {
		t.Fatal("reset image still served", err)
	}
	undone, err := s.ChangeIcon(ctx, "cycle", 3, "undo", "user:business-owner", f.when, nil)
	if err != nil || !undone.HasImage || undone.Revision != 4 {
		t.Fatal("undo lost the upload", undone, err)
	}
	if _, _, _, err = s.GetIconImage(ctx, "cycle"); err != nil {
		t.Fatal("undone image not served", err)
	}
	redone, err := s.ChangeIcon(ctx, "cycle", 4, "undo", "user:business-owner", f.when, nil)
	if err != nil || redone.HasImage || redone.Value != original.Value || redone.Revision != 5 {
		t.Fatal("second undo did not restore the generated icon", redone, err)
	}
}
