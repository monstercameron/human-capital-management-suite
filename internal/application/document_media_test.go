package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	transportmedia "github.com/monstercameron/human-capital-management-suite/internal/transport/documentmedia"
)

func TestDocumentMedia_DefaultRoot(t *testing.T) {
	for _, c := range []struct{ media, artifact, want string }{
		{"", "", filepath.Join(".artifacts", "document-media")},
		{"", filepath.Join("srv", "art"), filepath.Join("srv", "art", "document-media")},
		{filepath.Join("data", "chat"), "ignored", filepath.Join("data", "document-media")},
	} {
		if got := DefaultDocumentMediaRoot(c.media, c.artifact); got != c.want {
			t.Errorf("DefaultDocumentMediaRoot(%q,%q) = %q, want %q", c.media, c.artifact, got, c.want)
		}
	}
	for in, want := range map[string]string{"Leave: 2026/27": "Leave- 2026-27", "  ": "document", "..": "document", "Richtlinie Größe": "Richtlinie Größe"} {
		if got := exportFilename(in); got != want {
			t.Errorf("exportFilename(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[error]error{documenthubstore.ErrDenied: transportmedia.ErrNotFound, documenthubstore.ErrMediaTooLarge: transportmedia.ErrTooLarge, documenthubstore.ErrMediaUnsupported: transportmedia.ErrUnsupported} {
		if got := mediaError(in); !errors.Is(got, want) {
			t.Errorf("mediaError(%v) = %v", in, got)
		}
	}
}

func TestDocumentMedia_OverlayUnavailable(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := OverlayDocumentMedia(next, nil, t.TempDir(), mediaAdmission(t))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", transportmedia.PathPrefix+"doc", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated media route = %d", w.Code)
	}
	r := httptest.NewRequest("GET", transportmedia.PathPrefix+"doc", nil)
	r.Header.Set("Authorization", "Bearer valid")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("no store = %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/other", nil))
	if w.Code != http.StatusTeapot {
		t.Fatalf("fallback = %d", w.Code)
	}
}

// TestDocumentMedia_Integration drives the mounted boundary end to end on a
// real document store: upload, list, download, the three exports, and the
// refusals for a reader and a stranger.
func TestDocumentMedia_Integration(t *testing.T) {
	ctx := context.Background()
	svc := documentServiceFixture(t)
	const tenant, owner = "tenant", "member"
	id, _, err := svc.CreateDocument(ctx, tenant, owner, "Leave policy", "# Leave policy\n\nAccrue **monthly**.\n")
	if err != nil {
		t.Fatal(err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := OverlayDocumentMedia(next, svc.store, t.TempDir(), mediaAdmission(t))
	call := func(method, path string, body *bytes.Buffer, contentType string) *httptest.ResponseRecorder {
		if body == nil {
			body = &bytes.Buffer{}
		}
		r := httptest.NewRequest(method, path, body)
		r.Header.Set("Authorization", "Bearer valid")
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	var pic bytes.Buffer
	if err := png.Encode(&pic, image.NewRGBA(image.Rect(0, 0, 6, 4))); err != nil {
		t.Fatal(err)
	}
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	part, _ := mw.CreateFormFile("file", "chart.png")
	_, _ = part.Write(pic.Bytes())
	_ = mw.Close()
	w := call("POST", transportmedia.PathPrefix+id+"/upload", &form, mw.FormDataContentType())
	var uploaded transportmedia.Attachment
	if w.Code != http.StatusCreated || json.Unmarshal(w.Body.Bytes(), &uploaded) != nil || uploaded.MediaType != "image/png" || uploaded.Width != 6 {
		t.Fatalf("upload = %d %s", w.Code, w.Body)
	}
	w = call("GET", transportmedia.PathPrefix+id, nil, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), uploaded.ID) {
		t.Fatalf("list = %d %s", w.Code, w.Body)
	}
	w = call("GET", transportmedia.PathPrefix+id+"/"+uploaded.ID, nil, "")
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), pic.Bytes()) || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("download = %d %v", w.Code, w.Header())
	}
	if _, err := svc.CreateDocumentVersion(ctx, tenant, owner, id, mustVersion(t, svc, tenant, owner, id), "Leave policy", "# Leave policy\n\n![Chart](attachment:"+uploaded.ID+")\n"); err != nil {
		t.Fatal(err)
	}
	w = call("GET", transportmedia.PathPrefix+id+"/export?format=md", nil, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "(http://example.com"+transportmedia.PathPrefix+id+"/"+uploaded.ID+"?download=1)") || w.Header().Get("Content-Disposition") != `attachment; filename="Leave policy.md"` {
		t.Fatalf("markdown export = %d %v %s", w.Code, w.Header(), w.Body)
	}
	w = call("GET", transportmedia.PathPrefix+id+"/export?format=txt", nil, "")
	if w.Code != 200 || w.Body.String() != "Leave policy\n\n[Chart]\n" {
		t.Fatalf("text export = %d %q", w.Code, w.Body)
	}
	w = call("GET", transportmedia.PathPrefix+id+"/export?format=pdf", nil, "")
	if w.Code != 200 || !bytes.HasPrefix(w.Body.Bytes(), []byte("%PDF-")) || !bytes.Contains(w.Body.Bytes(), []byte("/Subtype /Image /Width 6 /Height 4")) {
		t.Fatalf("pdf export = %d", w.Code)
	}

	media := documentMediaService{store: svc.store, blobs: mustFiles(t)}
	if _, err := media.List(ctx, tenant, "stranger", id); !errors.Is(err, transportmedia.ErrNotFound) {
		t.Fatalf("stranger list = %v", err)
	}
	if _, err := svc.store.ShareDocument(ctx, tenant, id, owner, documenthubstore.GrantInput{SubjectID: "reader", Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectAllow}); err != nil {
		t.Fatal(err)
	}
	if _, err := media.Upload(ctx, tenant, "reader", id, "x.png", pic.Bytes()); !errors.Is(err, transportmedia.ErrForbidden) {
		t.Fatalf("reader upload = %v", err)
	}
	if _, err := media.Upload(ctx, tenant, "stranger", id, "x.png", pic.Bytes()); !errors.Is(err, transportmedia.ErrNotFound) {
		t.Fatalf("stranger upload = %v", err)
	}
	if _, err := media.Upload(ctx, tenant, owner, id, "x.txt", []byte("hello")); !errors.Is(err, transportmedia.ErrUnsupported) {
		t.Fatalf("text upload = %v", err)
	}
	if _, err := media.Export(ctx, tenant, owner, id, "docx", ""); !errors.Is(err, transportmedia.ErrInvalid) {
		t.Fatalf("unknown format = %v", err)
	}
	if _, _, err := media.Open(ctx, tenant, owner, id, "docm-missing"); !errors.Is(err, transportmedia.ErrNotFound) {
		t.Fatalf("missing attachment = %v", err)
	}
}

func mustVersion(t *testing.T, svc documentService, tenant, actor, id string) string {
	t.Helper()
	summary, _, _, err := svc.GetDocument(context.Background(), tenant, actor, id)
	if err != nil {
		t.Fatal(err)
	}
	return summary.VersionID
}

func mustFiles(t *testing.T) *documenthubstore.MediaFiles {
	t.Helper()
	files, err := documenthubstore.NewMediaFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return files
}
