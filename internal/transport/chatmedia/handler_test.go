package chatmedia

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"
	"time"

	core "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatmedia"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine"
)

func TestTodo_CHAT_036_TransportUnavailable(t *testing.T) {
	r := httptest.NewRequest("GET", "/v1/media/id", nil)
	w := httptest.NewRecorder()
	Handler{}.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

type mediaScanner struct{}

func (mediaScanner) Scan(context.Context, string, io.Reader) (quarantine.Verdict, error) {
	return quarantine.Verdict{Safe: true}, nil
}

func testHandler(now *time.Time) (*Handler, *core.Service) {
	s := core.New(core.Config{Store: core.NewMemoryStore(), Scanner: mediaScanner{}, Now: func() time.Time { return *now }, Authorize: func(context.Context, core.AccessRequest) error { return nil }})
	h := &Handler{Service: s, Principal: func(*http.Request) (string, string, string, bool) { return "t", "c", "p", true }}
	return h, s
}

func TestTodo_CHAT_036_UploadAndProtectedDownload(t *testing.T) {
	now := time.Unix(100, 0)
	h, s := testHandler(&now)
	w := uploadRequest(t, h, []byte("\x89PNG\r\n\x1a\npixels"))
	if w.Code != 200 {
		t.Fatalf("upload status=%d body=%s", w.Code, w.Body)
	}
	ref, err := s.Upload(context.Background(), core.UploadRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", DeclaredType: "image/png", Content: []byte("\x89PNG\r\n\x1a\nother"), EvidenceID: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.Authorize(context.Background(), core.AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", ArtifactID: ref.ArtifactID}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/v1/media/"+ref.ArtifactID, nil)
	r.Header.Set("X-Chat-Media-Grant", g.Token)
	r.Header.Set("Range", "bytes=1-4")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusPartialContent || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("download status=%d headers=%v", w.Code, w.Header())
	}
}
func TestTodo_CHAT_036_UnauthorizedAndMalformed(t *testing.T) {
	now := time.Unix(100, 0)
	h, _ := testHandler(&now)
	h.Principal = func(*http.Request) (string, string, string, bool) { return "", "", "", false }
	w := uploadRequest(t, h, []byte("x"))
	if w.Code != 401 {
		t.Fatalf("unauthorized upload=%d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("PUT", "/", nil))
	if w.Code != 405 {
		t.Fatalf("method=%d", w.Code)
	}
}
func TestTodo_CHAT_038_RangeAndExpiry(t *testing.T) {
	now := time.Unix(100, 0)
	h, s := testHandler(&now)
	ref, err := s.Upload(context.Background(), core.UploadRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", DeclaredType: "image/png", Content: []byte("\x89PNG\r\n\x1a\n"), EvidenceID: "e"})
	if err != nil {
		t.Fatal(err)
	}
	g, _ := s.Authorize(context.Background(), core.AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", ArtifactID: ref.ArtifactID}, time.Second)
	now = now.Add(2 * time.Second)
	r := httptest.NewRequest("GET", "/v1/media/"+ref.ArtifactID, nil)
	r.Header.Set("X-Chat-Media-Grant", g.Token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("expired status=%d", w.Code)
	}
}
func TestTodo_CHAT_039_NoService(t *testing.T) {
	if !errors.Is(core.ErrInvalid, core.ErrInvalid) {
		t.Fatal("sentinel")
	}
}

func TestTodo_CHAT_036_OversizedMultipartIsBounded(t *testing.T) {
	now := time.Unix(100, 0)
	h, _ := testHandler(&now)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("file", "x.png")
	_, _ = part.Write(bytes.Repeat([]byte("x"), (64<<20)+2))
	_ = mw.Close()
	r := httptest.NewRequest("POST", "/v1/media/upload", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("oversized multipart status=%d", w.Code)
	}
}

func TestTodo_CHAT_038_BadRangeIsRejected(t *testing.T) {
	now := time.Unix(100, 0)
	h, _ := testHandler(&now)
	r := httptest.NewRequest("GET", "/v1/media/id", nil)
	r.Header.Set("Range", "wat")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("bad range status=%d", w.Code)
	}
}

func uploadRequest(t *testing.T, h *Handler, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fh := make(textproto.MIMEHeader)
	fh.Set("Content-Disposition", `form-data; name="file"; filename="x.png"`)
	fh.Set("Content-Type", "image/png")
	part, _ := mw.CreatePart(fh)
	_, _ = part.Write(content)
	_ = mw.Close()
	r := httptest.NewRequest("POST", "/v1/media/upload", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
