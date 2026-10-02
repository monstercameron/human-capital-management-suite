package chatmedia

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine"
)

type chatattach001External struct {
	called int
	err    error
}

func (s *chatattach001External) Scan(_ context.Context, _ string, r io.Reader) (quarantine.Verdict, error) {
	s.called++
	b, _ := io.ReadAll(r)
	return quarantine.Verdict{Safe: bytes.Equal(b, []byte("hello"))}, s.err
}

func chatattach001Office(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, content := range entries {
		f, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(f, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestTodo_CHATATTACH_001_Security(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		data []byte
		safe bool
	}{
		{"text", []byte("hello"), true}, {"pdf", []byte("%PDF-1.7\nplain document\n%%EOF"), true},
		{"pdf-script", []byte("%PDF-1.7\n/JavaScript (doBadThings)"), false},
		{"exe", []byte{'M', 'Z', 0, 0, 1, 3}, false}, {"script", []byte("#!/bin/sh\nrun"), false},
		{"html", []byte("<html>active</html>"), false}, {"empty", nil, false},
		{"archive", chatattach001Office(t, map[string]string{"payload.txt": "hi"}), false},
		{"office", chatattach001Office(t, map[string]string{"[Content_Types].xml": "<Types/>", "word/document.xml": "<document/>"}), true},
		{"office-macro", chatattach001Office(t, map[string]string{"[Content_Types].xml": "<Types/>", "word/document.xml": "<document/>", "word/vbaProject.bin": "macro"}), false},
		{"office-external", chatattach001Office(t, map[string]string{"[Content_Types].xml": "<Types/>", "word/document.xml": "<document/>", "word/_rels/document.xml.rels": "TargetMode=\"External\""}), false},
		{"office-traversal", chatattach001Office(t, map[string]string{"[Content_Types].xml": "<Types/>", "word/document.xml": "<document/>", "../payload": "bad"}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := (Chatattach001Scanner{}).Scan(ctx, "id", bytes.NewReader(tc.data))
			if err != nil || v.Safe != tc.safe {
				t.Fatalf("verdict=%+v err=%v", v, err)
			}
		})
	}
	if v, err := (Chatattach001Scanner{MaxBytes: 4}).Scan(ctx, "id", bytes.NewReader([]byte("hello"))); err != nil || v.Safe || v.Reason != "file too large" {
		t.Fatalf("size verdict=%+v err=%v", v, err)
	}
	external := &chatattach001External{}
	v, err := (Chatattach001Scanner{External: external}).Scan(ctx, "id", bytes.NewReader([]byte("hello")))
	if err != nil || !v.Safe || external.called != 1 {
		t.Fatalf("external=%+v v=%+v err=%v", external, v, err)
	}
	external.err = io.ErrUnexpectedEOF
	if _, err := (Chatattach001Scanner{External: external}).Scan(ctx, "id", bytes.NewReader([]byte("hello"))); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := (Chatattach001Scanner{}).Scan(cancelled, "id", bytes.NewReader([]byte("hello"))); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestTodo_CHATATTACH_001(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewFilesystemStore(root)
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(_ context.Context, r AccessRequest) error {
		if r.PrincipalID != "alice" && r.PrincipalID != "bob" {
			return ErrUnauthorized
		}
		return nil
	}
	media := New(Config{Store: store, Scanner: Chatattach001Scanner{}, Authorize: authorize})
	now := time.Now().UTC()
	linked := false
	service := &Chatattach001Uploads{Media: media, Root: root, Authorize: authorize, PersonBytes: 10, ConversationBytes: 15, Now: func() time.Time { return now }, Linked: func(context.Context, Reference) (bool, error) { return linked, nil }}
	r := UploadRequest{TenantID: "tenant", ConversationID: "room", PrincipalID: "alice", Filename: "note.txt", DeclaredType: "text/plain", Content: []byte("hello")}
	ref, err := service.Upload(ctx, r)
	if err != nil || ref.State != StateAdmitted || ref.MediaType != "text/plain" || ref.Size != 5 {
		t.Fatalf("upload=%+v err=%v", ref, err)
	}
	if a, err := store.Get(ctx, "tenant", ref.ArtifactID); err != nil || !bytes.Equal(a.Content, r.Content) {
		t.Fatalf("stored=%+v err=%v", a, err)
	}
	if _, err := store.Get(ctx, "other", ref.ArtifactID); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("cross tenant read=%v", err)
	}
	if retry, err := service.Upload(ctx, r); err != nil || retry.ArtifactID != ref.ArtifactID {
		t.Fatalf("retry=%+v err=%v", retry, err)
	}
	r.Content = []byte("123456")
	if _, err := service.Upload(ctx, r); !errors.Is(err, ErrChatattach001Quota) {
		t.Fatalf("person quota=%v", err)
	}
	r.PrincipalID = "bob"
	if _, err := service.Upload(ctx, r); err != nil {
		t.Fatal(err)
	}
	r.Content = []byte("abcdef")
	if _, err := service.Upload(ctx, r); !errors.Is(err, ErrChatattach001Quota) {
		t.Fatalf("conversation quota=%v", err)
	}
	// Restart preserves quota instead of offering a fresh allowance.
	restarted := &Chatattach001Uploads{Media: media, Root: root, Authorize: authorize, PersonBytes: 10, ConversationBytes: 15}
	if _, err := restarted.Upload(ctx, r); !errors.Is(err, ErrChatattach001Quota) {
		t.Fatalf("restart quota=%v", err)
	}
	r.Content = bytes.Repeat([]byte("a"), int(Chatattach001MaxBytes)+1)
	if _, err := service.Upload(ctx, r); !errors.Is(err, ErrChatattach001Size) {
		t.Fatalf("size=%v", err)
	}
	r.Content = []byte("PK\x03\x04archive")
	if _, err := service.Upload(ctx, r); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("type=%v", err)
	}
	r.PrincipalID = "intruder"
	r.Content = []byte("hi")
	if _, err := service.Upload(ctx, r); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("authorization=%v", err)
	}
	linked = true
	if err := service.Retain(ctx, "tenant"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(48 * time.Hour)
	if err := service.Retain(ctx, "tenant"); err != nil {
		t.Fatal(err)
	}
	if _, err := media.Chatattach001Describe(ctx, "tenant", "room", ref.ArtifactID); err != nil {
		t.Fatalf("retained message lost bytes: %v", err)
	}
	linked = false
	if err := service.Retain(ctx, "tenant"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "tenant", ref.ArtifactID); err == nil {
		t.Fatal("purged message retained bytes")
	}
}

func TestTodo_CHATATTACH_001_Security_QuarantineCleanup(t *testing.T) {
	ctx := context.Background()
	authorize := func(context.Context, AccessRequest) error { return nil }
	root := t.TempDir()
	store, err := NewFilesystemStore(root)
	if err != nil {
		t.Fatal(err)
	}
	external := &chatattach001External{err: io.ErrUnexpectedEOF}
	media := New(Config{Store: store, Scanner: Chatattach001Scanner{External: external}, Authorize: authorize})
	uploads := &Chatattach001Uploads{Media: media, Root: root, Authorize: authorize}
	r := UploadRequest{TenantID: "tenant", ConversationID: "room", PrincipalID: "alice", Filename: "file.txt", Content: []byte("hello")}
	if _, err := uploads.Upload(ctx, r); !errors.Is(err, ErrScannerUnavailable) {
		t.Fatalf("scanner failure=%v", err)
	}
	files, err := filepath.Glob(filepath.Join(root, "*"))
	if err != nil || len(files) != 0 {
		t.Fatalf("quarantine leaked files=%v err=%v", files, err)
	}
	external.err = nil
	r.Content = []byte("unsafe")
	if _, err := uploads.Upload(ctx, r); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("external refusal=%v", err)
	}
	files, err = filepath.Glob(filepath.Join(root, "*"))
	if err != nil || len(files) != 0 {
		t.Fatalf("refused bytes leaked files=%v err=%v", files, err)
	}
	r.Filename = "file.exe"
	r.Content = []byte("hello")
	if _, err := uploads.Upload(ctx, r); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("executable filename=%v", err)
	}
	// Images go through the existing rendition service and remain readable.
	media.scanner = Chatattach001Scanner{}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	r.Filename = "photo.png"
	r.Content = encoded.Bytes()
	r.DeclaredType = "image/png"
	ref, err := uploads.Upload(ctx, r)
	if err != nil || ref.MediaType != MediaPNG {
		t.Fatalf("image=%+v err=%v", ref, err)
	}
	artifact, err := store.Get(ctx, "tenant", ref.ArtifactID)
	if err != nil || !bytes.Equal(artifact.Content, r.Content) {
		t.Fatalf("image bytes=%+v err=%v", artifact.Reference, err)
	}
	if err := uploads.removeArtifact(ref, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ref.ArtifactID+".bytes")); err != nil {
		t.Fatalf("failure cleanup deleted admitted image: %v", err)
	}
	r.Filename = "table.csv"
	r.Content = []byte("name,value\nAlice,5")
	r.DeclaredType = "text/csv"
	if ref, err := uploads.Upload(ctx, r); err != nil || ref.MediaType != "text/plain" {
		t.Fatalf("CSV sniff=%+v err=%v", ref, err)
	}
}
