package chatmedia

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine"
	"golang.org/x/image/bmp"
)

func validChatPNG(t *testing.T, width, height int, alpha bool) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			seed := uint32(x*73856093) ^ uint32(y*19349663)
			seed ^= seed >> 13
			seed *= 1274126177
			a := uint8(255)
			if alpha && x < width/2 {
				a = 100
			}
			im.SetNRGBA(x, y, color.NRGBA{R: uint8(seed), G: uint8(seed >> 8), B: uint8(seed >> 16), A: a})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, im); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestTodo_CHAT_038_StaticRenditionsRetainOriginalAndAuthorization(t *testing.T) {
	content := validChatPNG(t, 1400, 700, false)
	store := NewMemoryStore()
	allowed := true
	s := New(Config{Store: store, Scanner: testScanner{verdict: quarantine.Verdict{Safe: true}}, Authorize: func(context.Context, AccessRequest) error {
		if !allowed {
			return ErrUnauthorized
		}
		return nil
	}})
	ref, err := s.Upload(context.Background(), UploadRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", DeclaredType: "image/png", Content: content})
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.Authorize(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", ArtifactID: ref.ArtifactID}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	req := AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", ArtifactID: ref.ArtifactID, Grant: g.Token}
	for _, variant := range []struct {
		name          string
		width, height int
	}{{VariantThumbnail, 640, 320}, {VariantDisplay, 1400, 700}} {
		body, gotRef, err := s.OpenVariant(context.Background(), req, variant.name, 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := io.ReadAll(body)
		body.Close()
		if err != nil || gotRef.MediaType != MediaJPEG || len(encoded) >= len(content) {
			t.Fatalf("%s: type=%s bytes=%d source=%d err=%v", variant.name, gotRef.MediaType, len(encoded), len(content), err)
		}
		config, err := jpeg.DecodeConfig(bytes.NewReader(encoded))
		if err != nil || config.Width != variant.width || config.Height != variant.height {
			t.Fatalf("%s: dimensions=%dx%d err=%v", variant.name, config.Width, config.Height, err)
		}
	}
	original, _, err := s.Open(context.Background(), req, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	gotOriginal, _ := io.ReadAll(original)
	original.Close()
	if !bytes.Equal(gotOriginal, content) {
		t.Fatal("original upload changed")
	}
	allowed = false
	if _, _, err := s.OpenVariant(context.Background(), req, VariantThumbnail, 0, nil); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked membership: %v", err)
	}
	allowed = true
	s.Revoke(ref.ArtifactID)
	if _, _, err := s.OpenVariant(context.Background(), req, VariantDisplay, 0, nil); !errors.Is(err, ErrRevoked) {
		t.Fatalf("revoked grant: %v", err)
	}
}

func TestTodo_CHAT_038_TransparentPNGAndAnimatedGIF(t *testing.T) {
	s := makeService(testScanner{verdict: quarantine.Verdict{Safe: true}}, func(context.Context, AccessRequest) error { return nil })
	ref, err := s.Upload(context.Background(), UploadRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", DeclaredType: "image/png", Content: validChatPNG(t, 800, 400, true)})
	if err != nil {
		t.Fatal(err)
	}
	g, _ := s.Authorize(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", ArtifactID: ref.ArtifactID}, time.Minute)
	body, got, err := s.OpenVariant(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", ArtifactID: ref.ArtifactID, Grant: g.Token}, VariantThumbnail, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	preview, _ := io.ReadAll(body)
	body.Close()
	if got.MediaType != MediaPNG || len(preview) == 0 {
		t.Fatalf("transparent preview type=%s bytes=%d", got.MediaType, len(preview))
	}
	frame := image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Black, color.White})
	var animation bytes.Buffer
	if err := gif.EncodeAll(&animation, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{10, 20}}); err != nil {
		t.Fatal(err)
	}
	gifRef, err := s.Upload(context.Background(), UploadRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", DeclaredType: "image/gif", Content: animation.Bytes()})
	if err != nil {
		t.Fatal(err)
	}
	g, _ = s.Authorize(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", ArtifactID: gifRef.ArtifactID}, time.Minute)
	body, got, err = s.OpenVariant(context.Background(), AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", ArtifactID: gifRef.ArtifactID, Grant: g.Token}, VariantThumbnail, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	animated, _ := io.ReadAll(body)
	body.Close()
	if got.MediaType != MediaGIF || !bytes.Equal(animated, animation.Bytes()) {
		t.Fatal("animated GIF was changed")
	}
}

func TestTodo_CHAT_038_FilesystemRenditionPersistsWithoutOriginalMetadataCopy(t *testing.T) {
	root := t.TempDir()
	store, err := NewFilesystemStore(root)
	if err != nil {
		t.Fatal(err)
	}
	s := New(Config{Store: store, Scanner: testScanner{verdict: quarantine.Verdict{Safe: true}}, Authorize: func(context.Context, AccessRequest) error { return nil }})
	content := validChatPNG(t, 900, 450, false)
	ref, err := s.Upload(context.Background(), UploadRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", DeclaredType: "image/png", Content: content})
	if err != nil {
		t.Fatal(err)
	}
	meta, err := os.ReadFile(filepath.Join(root, ref.ArtifactID+".json"))
	if err != nil || bytes.Contains(meta, content) || len(meta) > 4096 {
		t.Fatalf("metadata bytes=%d err=%v", len(meta), err)
	}
	r, err := store.GetRendition(context.Background(), "t", ref.ArtifactID, VariantThumbnail)
	if err != nil || r.MediaType != MediaJPEG || r.Size == 0 {
		t.Fatalf("persisted rendition=%+v err=%v", r.Reference, err)
	}
	if _, err := store.GetRendition(context.Background(), "foreign", ref.ArtifactID, VariantThumbnail); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("foreign tenant read=%v", err)
	}
	original, err := store.Get(context.Background(), "t", ref.ArtifactID)
	if err != nil || !bytes.Equal(original.Content, content) {
		t.Fatalf("original read=%v", err)
	}
}

func TestTodo_CHAT_038_BMPAndJPEGNormalizedWithoutSourceMetadata(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 700, 350))
	for y := 0; y < 350; y++ {
		for x := 0; x < 700; x++ {
			im.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 140, A: 255})
		}
	}
	var bmpBytes bytes.Buffer
	if err := bmp.Encode(&bmpBytes, im); err != nil {
		t.Fatal(err)
	}
	bmpRenditions, err := imageRenditions(bmpBytes.Bytes(), MediaBMP)
	if err != nil || bmpRenditions[VariantThumbnail].MediaType != MediaJPEG {
		t.Fatalf("BMP rendition=%v err=%v", bmpRenditions, err)
	}
	var jpegBytes bytes.Buffer
	if err := jpeg.Encode(&jpegBytes, im, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	// A legal APP1 segment simulates phone orientation and sensitive metadata.
	tiff := []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 0x01, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	exif := append([]byte("Exif\x00\x00"), tiff...)
	exif = append(exif, []byte("private-location")...)
	segment := append([]byte{0xff, 0xe1, 0, byte(len(exif) + 2)}, exif...)
	withEXIF := append(append([]byte(nil), jpegBytes.Bytes()[:2]...), segment...)
	withEXIF = append(withEXIF, jpegBytes.Bytes()[2:]...)
	jpegRenditions, err := imageRenditions(withEXIF, MediaJPEG)
	if err != nil || len(jpegRenditions) != 2 {
		t.Fatalf("JPEG rendition count=%d err=%v", len(jpegRenditions), err)
	}
	if orientation := jpegEXIFOrientation(withEXIF); orientation != 6 {
		t.Fatalf("EXIF orientation=%d", orientation)
	}
	for variant, r := range jpegRenditions {
		if bytes.Contains(r.Content, []byte("private-location")) || bytes.Contains(r.Content, []byte("Exif")) {
			t.Fatalf("%s retained source metadata", variant)
		}
		config, err := jpeg.DecodeConfig(bytes.NewReader(r.Content))
		if err != nil || config.Width >= config.Height {
			t.Fatalf("%s orientation not applied: %dx%d err=%v", variant, config.Width, config.Height, err)
		}
	}
}

func TestTodo_CHAT_038_EXIFOrientationCoordinates(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 2; x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: uint8(10*y + x), A: 255})
		}
	}
	for _, tc := range []struct {
		orientation int
		want        uint8
	}{{1, 0}, {2, 1}, {3, 21}, {4, 20}, {5, 0}, {6, 20}, {7, 21}, {8, 1}} {
		oriented := orientedImage{source: source, orientation: tc.orientation}
		got := color.NRGBAModel.Convert(oriented.At(0, 0)).(color.NRGBA).R
		if got != tc.want {
			t.Fatalf("orientation %d top-left=%d want=%d", tc.orientation, got, tc.want)
		}
	}
}

func TestTodo_CHAT_038_PixelBoundRejectsBeforeDecodeAllocation(t *testing.T) {
	oversized := validChatPNG(t, 1, 1, false)
	binary.BigEndian.PutUint32(oversized[16:20], 16_384)
	binary.BigEndian.PutUint32(oversized[20:24], 16_384)
	binary.BigEndian.PutUint32(oversized[29:33], crc32.ChecksumIEEE(oversized[12:29]))
	store := NewMemoryStore()
	s := New(Config{Store: store, Scanner: testScanner{verdict: quarantine.Verdict{Safe: true}}, Authorize: func(context.Context, AccessRequest) error { return nil }})
	ref, err := s.Upload(context.Background(), UploadRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", DeclaredType: "image/png", Content: oversized})
	if !errors.Is(err, ErrInvalid) || ref.ArtifactID != "" {
		t.Fatalf("oversized dimensions ref=%+v err=%v", ref, err)
	}
	id := scopedArtifactID("t", "c", oversized)
	if _, err := store.Get(context.Background(), "t", id); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("oversized bytes admitted: %v", err)
	}
}

func TestTodo_CHAT_038_MalformedStaticImagesStayQuarantined(t *testing.T) {
	for _, tc := range []struct {
		mediaType string
		content   []byte
	}{{"image/png", []byte("\x89PNG\r\n\x1a\ninvalid")}, {"image/jpeg", []byte{0xff, 0xd8, 0xff, 0xd9}}, {"image/bmp", []byte("BMinvalid")}} {
		store := NewMemoryStore()
		s := New(Config{Store: store, Scanner: testScanner{verdict: quarantine.Verdict{Safe: true}}, Authorize: func(context.Context, AccessRequest) error { return nil }})
		_, err := s.Upload(context.Background(), UploadRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", DeclaredType: tc.mediaType, Content: tc.content})
		if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("%s malformed upload=%v", tc.mediaType, err)
		}
		id := scopedArtifactID("t", "c", tc.content)
		if _, err := store.Get(context.Background(), "t", id); !errors.Is(err, ErrQuarantined) {
			t.Fatalf("%s malformed bytes admitted: %v", tc.mediaType, err)
		}
	}
}

type holdingImageScanner struct {
	entered chan struct{}
	release chan struct{}
}

type toggleImageScanner struct{ safe bool }

func (s *toggleImageScanner) Scan(context.Context, string, io.Reader) (quarantine.Verdict, error) {
	return quarantine.Verdict{Safe: s.safe}, nil
}

func TestTodo_CHAT_038_DuplicateUploadKeepsAdmittedOriginal(t *testing.T) {
	for _, tc := range []struct {
		name  string
		store Store
	}{{"memory", NewMemoryStore()}, {"filesystem", func() Store {
		store, err := NewFilesystemStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return store
	}()}} {
		t.Run(tc.name, func(t *testing.T) {
			scanner := &toggleImageScanner{safe: true}
			s := New(Config{Store: tc.store, Scanner: scanner, Authorize: func(context.Context, AccessRequest) error { return nil }})
			content := validChatPNG(t, 4, 2, false)
			req := UploadRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", DeclaredType: "image/png", Content: content}
			ref, err := s.Upload(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			scanner.safe = false
			if _, err := s.Upload(context.Background(), req); !errors.Is(err, ErrQuarantined) {
				t.Fatalf("duplicate rejection=%v", err)
			}
			original, err := tc.store.Get(context.Background(), "t", ref.ArtifactID)
			if err != nil || !bytes.Equal(original.Content, content) {
				t.Fatalf("admitted original changed: %v", err)
			}
			if _, err := tc.store.GetRendition(context.Background(), "t", ref.ArtifactID, VariantThumbnail); err != nil {
				t.Fatalf("admitted rendition changed: %v", err)
			}
		})
	}
}

func (s holdingImageScanner) Scan(_ context.Context, _ string, _ io.Reader) (quarantine.Verdict, error) {
	s.entered <- struct{}{}
	<-s.release
	return quarantine.Verdict{Safe: true}, nil
}

func TestTodo_CHAT_038_GlobalImageMemoryBound(t *testing.T) {
	entered := make(chan struct{}, MaxConcurrentImageDerivations)
	release := make(chan struct{})
	s := New(Config{Store: NewMemoryStore(), Scanner: holdingImageScanner{entered: entered, release: release}, Authorize: func(context.Context, AccessRequest) error { return nil }})
	content := validChatPNG(t, 1, 1, false)
	results := make(chan error, MaxConcurrentImageDerivations)
	for i := 0; i < MaxConcurrentImageDerivations; i++ {
		go func(i int) {
			_, err := s.Upload(context.Background(), UploadRequest{TenantID: fmt.Sprintf("t%d", i), ConversationID: "c", PrincipalID: "p", DeclaredType: "image/png", Content: content})
			results <- err
		}(i)
	}
	for i := 0; i < MaxConcurrentImageDerivations; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("image slot holder did not reach scanner")
		}
	}
	if _, err := s.Upload(context.Background(), UploadRequest{TenantID: "overflow", ConversationID: "c", PrincipalID: "p", DeclaredType: "image/png", Content: content}); !errors.Is(err, ErrBusy) {
		t.Fatalf("global image bound=%v", err)
	}
	close(release)
	for i := 0; i < MaxConcurrentImageDerivations; i++ {
		if err := <-results; err != nil {
			t.Fatalf("within image bound: %v", err)
		}
	}
}
