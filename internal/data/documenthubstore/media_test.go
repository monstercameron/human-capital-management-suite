package documenthubstore

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 200, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testWebP(w, h int) []byte {
	bits := uint32(w-1) | uint32(h-1)<<14
	chunk := []byte{0x2f, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(chunk[1:], bits)
	chunk = append(chunk, make([]byte, 16)...)
	out := []byte("RIFF\x00\x00\x00\x00WEBPVP8L")
	size := make([]byte, 4)
	binary.LittleEndian.PutUint32(size, uint32(len(chunk)))
	out = append(out, size...)
	out = append(out, chunk...)
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(out)-8))
	return out
}

const testPDF = "%PDF-1.4\n1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n2 0 obj << /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >> endobj\n3 0 obj << /Type /Page /Parent 2 0 R >> endobj\n4 0 obj << /Type /Page /Parent 2 0 R >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n"

// TestDocumentMedia_Sniffing: the media type comes from the bytes, never a
// declared type, and only the admitted image and PDF types pass.
func TestDocumentMedia_Sniffing(t *testing.T) {
	var jpg, gf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	if err := jpeg.Encode(&jpg, img, nil); err != nil {
		t.Fatal(err)
	}
	if err := gif.Encode(&gf, img, nil); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		content []byte
		want    MediaFacts
		err     error
	}{
		{"png", testPNG(t, 5, 7), MediaFacts{MediaType: MediaPNG, Width: 5, Height: 7}, nil},
		{"jpeg", jpg.Bytes(), MediaFacts{MediaType: MediaJPEG, Width: 4, Height: 3}, nil},
		{"gif", gf.Bytes(), MediaFacts{MediaType: MediaGIF, Width: 4, Height: 3}, nil},
		{"webp", testWebP(9, 11), MediaFacts{MediaType: MediaWebP, Width: 9, Height: 11}, nil},
		{"pdf", []byte(testPDF), MediaFacts{MediaType: MediaPDF, Pages: 2}, nil},
		{"html", []byte("<html><script>alert(1)</script></html>"), MediaFacts{}, ErrMediaUnsupported},
		{"svg", []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"></svg>`), MediaFacts{}, ErrMediaUnsupported},
		{"truncated png", testPNG(t, 2, 2)[:20], MediaFacts{}, ErrMediaUnsupported},
		{"empty", nil, MediaFacts{}, ErrMediaUnsupported},
		{"text", []byte("just words"), MediaFacts{}, ErrMediaUnsupported},
	}
	for _, c := range cases {
		got, err := InspectMedia(c.content)
		if !errors.Is(err, c.err) || got != c.want {
			t.Errorf("%s: got %+v, %v; want %+v, %v", c.name, got, err, c.want, c.err)
		}
	}
}

// TestDocumentMedia_Caps: images stop at 10 MB and PDFs at 25 MB.
func TestDocumentMedia_Caps(t *testing.T) {
	bigImage := append(testPNG(t, 2, 2), make([]byte, MaxMediaImageBytes)...)
	if _, err := InspectMedia(bigImage); !errors.Is(err, ErrMediaTooLarge) {
		t.Fatalf("oversized image: %v", err)
	}
	pdf := append([]byte(testPDF), make([]byte, MaxMediaImageBytes)...)
	if facts, err := InspectMedia(pdf); err != nil || facts.MediaType != MediaPDF {
		t.Fatalf("an 11 MB PDF is under its own cap: %+v %v", facts, err)
	}
	bigPDF := append([]byte(testPDF), make([]byte, MaxMediaPDFBytes)...)
	if _, err := InspectMedia(bigPDF); !errors.Is(err, ErrMediaTooLarge) {
		t.Fatalf("oversized PDF: %v", err)
	}
	if _, err := InspectMedia(testWebP(16000, 16000)); !errors.Is(err, ErrMediaUnsupported) {
		t.Fatalf("decompression bomb admitted: %v", err)
	}
}

func TestDocumentMedia_Filename(t *testing.T) {
	for in, want := range map[string]string{
		`C:\fakepath\plan.png`: "plan.png", "../../etc/passwd": "passwd", "a\"b\r\n.pdf": "ab.pdf",
		"": "attachment", "..": "attachment", strings.Repeat("x", 200): strings.Repeat("x", 120),
	} {
		if got := CleanMediaFilename(in); got != want {
			t.Errorf("CleanMediaFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDocumentMedia_FilesKeepTenantsApart(t *testing.T) {
	files, err := NewMediaFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sha := strings.Repeat("ab", 32)
	if err := files.Put(ctx, "tenant-a", sha, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := files.Put(ctx, "tenant-a", sha, []byte("two")); err != nil {
		t.Fatal(err)
	}
	if got, err := files.Get(ctx, "tenant-a", sha); err != nil || string(got) != "one" {
		t.Fatalf("content-addressed blob rewritten: %q %v", got, err)
	}
	if _, err := files.Get(ctx, "tenant-b", sha); err == nil {
		t.Fatal("another tenant read the blob")
	}
	if err := files.Put(ctx, "tenant-a", "../../escape", []byte("x")); !errors.Is(err, ErrDenied) {
		t.Fatalf("path-like hash accepted: %v", err)
	}
	if _, err := NewMediaFiles(" "); err == nil {
		t.Fatal("empty root accepted")
	}
}

// TestDocumentMedia_Access is the integration test: uploads need the
// owner's edit right, reads follow the read grant, bytes are
// content-addressed and verified, and tenants never see each other's rows.
func TestDocumentMedia_Access(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	root := t.TempDir()
	files, err := NewMediaFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	docID, _, err := s.CreatePersonalDocument(ctx, "tenant-a", "u-owner", "Handbook", "# Handbook\n\nText.\n")
	if err != nil {
		t.Fatal(err)
	}
	pic := testPNG(t, 3, 2)
	if _, err := s.AddMedia(ctx, files, "tenant-a", "u-stranger", docID, "x.png", pic); !errors.Is(err, ErrDenied) {
		t.Fatalf("stranger uploaded: %v", err)
	}
	if _, err := s.AddMedia(ctx, files, "tenant-a", "u-owner", docID, "x.txt", []byte("hello")); !errors.Is(err, ErrMediaUnsupported) {
		t.Fatalf("text admitted: %v", err)
	}
	m, err := s.AddMedia(ctx, files, "tenant-a", "u-owner", docID, `C:\fakepath\chart.png`, pic)
	if err != nil {
		t.Fatal(err)
	}
	if m.MediaType != MediaPNG || m.Filename != "chart.png" || m.Width != 3 || m.Height != 2 || m.SizeBytes != int64(len(pic)) || len(m.SHA256) != 64 || !strings.HasPrefix(m.ID, "docm-") || !m.IsImage() {
		t.Fatalf("stored media wrong: %+v", m)
	}
	again, err := s.AddMedia(ctx, files, "tenant-a", "u-owner", docID, "again.png", pic)
	if err != nil || again.ID != m.ID {
		t.Fatalf("same bytes made a second row: %+v %v", again, err)
	}
	pdf, err := s.AddMedia(ctx, files, "tenant-a", "u-owner", docID, "policy.pdf", []byte(testPDF))
	if err != nil || pdf.MediaType != MediaPDF || pdf.Pages != 2 || pdf.IsImage() {
		t.Fatalf("pdf: %+v %v", pdf, err)
	}
	listed, err := s.ListMedia(ctx, "tenant-a", "u-owner", docID)
	if err != nil || len(listed) != 2 || listed[0].ID != m.ID || listed[1].ID != pdf.ID {
		t.Fatalf("list: %+v %v", listed, err)
	}
	if _, err := s.ListMedia(ctx, "tenant-a", "u-reader", docID); !errors.Is(err, ErrDenied) {
		t.Fatalf("ungranted list: %v", err)
	}
	if _, _, err := s.OpenMedia(ctx, files, "tenant-a", "u-reader", docID, m.ID); !errors.Is(err, ErrDenied) {
		t.Fatalf("ungranted open: %v", err)
	}
	if _, err := s.ShareDocument(ctx, "tenant-a", docID, "u-owner", GrantInput{SubjectKind: "person", SubjectID: "u-reader", Action: ActionRead, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	opened, content, err := s.OpenMedia(ctx, files, "tenant-a", "u-reader", docID, m.ID)
	if err != nil || opened.ID != m.ID || !bytes.Equal(content, pic) {
		t.Fatalf("granted open: %+v %v", opened, err)
	}
	if _, err := s.AddMedia(ctx, files, "tenant-a", "u-reader", docID, "y.png", testPNG(t, 1, 1)); !errors.Is(err, ErrDenied) {
		t.Fatalf("reader uploaded: %v", err)
	}
	if _, _, err := s.OpenMedia(ctx, files, "tenant-a", "u-reader", docID, "docm-unknown"); !errors.Is(err, ErrDenied) {
		t.Fatalf("unknown id: %v", err)
	}
	if _, err := s.ListMedia(ctx, "tenant-b", "u-owner", docID); !errors.Is(err, ErrDenied) {
		t.Fatalf("cross-tenant list: %v", err)
	}
	// Tampered bytes on disk are refused rather than served.
	blob, err := files.path("tenant-a", m.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blob, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.OpenMedia(ctx, files, "tenant-a", "u-owner", docID, m.ID); !errors.Is(err, ErrMediaCorrupt) {
		t.Fatalf("tampered blob served: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddMedia(ctx, nil, "tenant-a", "u-owner", docID, "x.png", pic); err == nil {
		t.Fatal("nil blob store accepted")
	}
}
