package documentmedia

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type fakeService struct {
	uploaded       []byte
	filename       string
	calls          []string
	err            error
	origin, format string
	block          chan struct{}
	mu             sync.Mutex
}

func (f *fakeService) Upload(_ context.Context, tenant, actor, doc, filename string, content []byte) (Attachment, error) {
	f.record("upload:" + tenant + ":" + actor + ":" + doc)
	if f.block != nil {
		<-f.block
	}
	if f.err != nil {
		return Attachment{}, f.err
	}
	f.uploaded, f.filename = content, filename
	return Attachment{ID: "docm-1", DocumentID: doc, Filename: filename, MediaType: "image/png", Size: int64(len(content))}, nil
}

func (f *fakeService) List(_ context.Context, tenant, actor, doc string) ([]Attachment, error) {
	f.record("list:" + doc)
	if f.err != nil {
		return nil, f.err
	}
	return nil, nil
}

func (f *fakeService) Open(_ context.Context, tenant, actor, doc, id string) (Attachment, []byte, error) {
	f.record("open:" + doc + ":" + id)
	if f.err != nil {
		return Attachment{}, nil, f.err
	}
	if id == "docm-pdf" {
		return Attachment{ID: id, Filename: "Richtlinie – Größe.pdf", MediaType: "application/pdf"}, []byte("%PDF-1.4"), nil
	}
	return Attachment{ID: id, Filename: "chart.png", MediaType: "image/png"}, []byte("\x89PNG"), nil
}

func (f *fakeService) Export(_ context.Context, tenant, actor, doc, format, origin string) (File, error) {
	f.record("export:" + doc + ":" + format)
	f.origin, f.format = origin, format
	if f.err != nil {
		return File{}, f.err
	}
	return File{Name: "Handbook.md", ContentType: "text/markdown; charset=utf-8", Content: []byte("# Handbook\n")}, nil
}

func signedIn(t *testing.T, kind trust.SubjectKind) func(*http.Request) *http.Request {
	t.Helper()
	now := time.Now()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: "u-owner", SubjectKind: kind, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	return func(r *http.Request) *http.Request { return r.WithContext(trust.WithPrincipal(r.Context(), p)) }
}

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func multipartBody(t *testing.T, declared string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	header := make(map[string][]string)
	header["Content-Disposition"] = []string{`form-data; name="file"; filename="chart.png"`}
	header["Content-Type"] = []string{declared}
	part, err := mw.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(content)
	_ = mw.WriteField("note", "ignored")
	_ = mw.Close()
	return &body, mw.FormDataContentType()
}

// TestDocumentMediaHandler_Authz: nothing is served without a trusted human
// principal, and the principal (never a request field) names tenant and
// actor.
func TestDocumentMediaHandler_Authz(t *testing.T) {
	f := &fakeService{}
	h := NewHandler(f)
	if rec := serve(h, httptest.NewRequest(http.MethodGet, PathPrefix+"doc-1", nil)); rec.Code != http.StatusUnauthorized || len(f.calls) != 0 {
		t.Fatalf("anonymous list: %d %v", rec.Code, f.calls)
	}
	machine := signedIn(t, trust.SubjectKindService)
	if rec := serve(h, machine(httptest.NewRequest(http.MethodGet, PathPrefix+"doc-1", nil))); rec.Code != http.StatusUnauthorized {
		t.Fatalf("service principal: %d", rec.Code)
	}
	human := signedIn(t, trust.SubjectKindHuman)
	body, contentType := multipartBody(t, "text/html", []byte("\x89PNG bytes"))
	req := human(httptest.NewRequest(http.MethodPost, PathPrefix+"doc-1/upload?tenant=tenant-b", body))
	req.Header.Set("Content-Type", contentType)
	rec := serve(h, req)
	if rec.Code != http.StatusCreated || f.calls[0] != "upload:tenant-a:u-owner:doc-1" || string(f.uploaded) != "\x89PNG bytes" || f.filename != "chart.png" {
		t.Fatalf("upload: %d %v %q", rec.Code, f.calls, f.uploaded)
	}
	var got Attachment
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.ID != "docm-1" {
		t.Fatalf("upload response: %s", rec.Body)
	}
	for err, want := range map[error]int{ErrNotFound: 404, ErrForbidden: 403, ErrTooLarge: 413, ErrUnsupported: 415, ErrInvalid: 400, errors.New("disk /srv/x failed"): 500} {
		f.err = err
		rec := serve(h, human(httptest.NewRequest(http.MethodGet, PathPrefix+"doc-1/docm-1", nil)))
		if rec.Code != want || strings.Contains(rec.Body.String(), "/srv") {
			t.Errorf("%v: %d %q", err, rec.Code, rec.Body)
		}
	}
	if (&Handler{}).Service != nil || serve(&Handler{}, human(httptest.NewRequest(http.MethodGet, PathPrefix+"doc-1", nil))).Code != http.StatusServiceUnavailable {
		t.Fatal("unconfigured handler did not answer 503")
	}
}

// TestDocumentMediaHandler_Headers: downloads carry an exact type, a
// disposition with an RFC 2231 filename, nosniff, a sandboxing CSP and
// no-store.
func TestDocumentMediaHandler_Headers(t *testing.T) {
	f := &fakeService{}
	h := NewHandler(f)
	human := signedIn(t, trust.SubjectKindHuman)
	rec := serve(h, human(httptest.NewRequest(http.MethodGet, PathPrefix+"doc-1/docm-pdf?download=1", nil)))
	header := rec.Header()
	if rec.Code != 200 || header.Get("Content-Type") != "application/pdf" || header.Get("X-Content-Type-Options") != "nosniff" ||
		header.Get("Cache-Control") != "no-store" || header.Get("Content-Security-Policy") != "default-src 'none'; sandbox" ||
		header.Get("Content-Disposition") != "attachment; filename*=utf-8''Richtlinie%20%E2%80%93%20Gr%C3%B6%C3%9Fe.pdf" || header.Get("Content-Length") != "8" {
		t.Fatalf("pdf download headers: %d %v", rec.Code, header)
	}
	inline := serve(h, human(httptest.NewRequest(http.MethodGet, PathPrefix+"doc-1/docm-img", nil)))
	if inline.Header().Get("Content-Disposition") != `inline; filename=chart.png` || inline.Header().Get("Content-Type") != "image/png" || inline.Body.String() != "\x89PNG" {
		t.Fatalf("inline image: %v %q", inline.Header(), inline.Body)
	}
	list := serve(h, human(httptest.NewRequest(http.MethodGet, PathPrefix+"doc-1", nil)))
	if list.Code != 200 || strings.TrimSpace(list.Body.String()) != `{"attachments":[]}` || list.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("list: %d %q", list.Code, list.Body)
	}
	export := httptest.NewRequest(http.MethodGet, "https://hcm.example"+PathPrefix+"doc-1/export?format=md", nil)
	export.Header.Set("X-Forwarded-Proto", "https")
	rec = serve(h, human(export))
	if rec.Code != 200 || f.format != "md" || f.origin != "https://hcm.example" || rec.Header().Get("Content-Disposition") != "attachment; filename=Handbook.md" || rec.Header().Get("Content-Type") != "text/markdown; charset=utf-8" {
		t.Fatalf("export: %d %v origin=%q", rec.Code, rec.Header(), f.origin)
	}
	if rec := serve(h, human(httptest.NewRequest(http.MethodGet, PathPrefix+"doc-1/export?format=docx", nil))); rec.Code != 400 {
		t.Fatalf("unknown format: %d", rec.Code)
	}
}

func TestDocumentMediaHandler_Routing(t *testing.T) {
	f := &fakeService{}
	h := NewHandler(f)
	human := signedIn(t, trust.SubjectKindHuman)
	cases := []struct {
		method, path string
		want         int
	}{
		{http.MethodDelete, PathPrefix + "doc-1/docm-1", 405},
		{http.MethodGet, PathPrefix + "doc-1/upload", 405},
		{http.MethodPost, PathPrefix + "doc-1/docm-1", 405},
		{http.MethodGet, PathPrefix + "doc-1/a/b", 404},
		{http.MethodGet, PathPrefix + "doc%20x", 404},
		{http.MethodGet, PathPrefix, 404},
		{http.MethodPost, PathPrefix + "doc-1", 404},
	}
	for _, c := range cases {
		if rec := serve(h, human(httptest.NewRequest(c.method, c.path, nil))); rec.Code != c.want {
			t.Errorf("%s %s = %d, want %d", c.method, c.path, rec.Code, c.want)
		}
	}
	if validID("..") || validID(strings.Repeat("a", 129)) || !validID("docm-6f1e") {
		t.Fatal("validID")
	}
	hostile := httptest.NewRequest(http.MethodGet, "/x", nil)
	hostile.Host = `bad"host`
	if got := requestOrigin(hostile); got != "http://localhost" {
		t.Fatalf("hostile host kept: %q", got)
	}
}

// TestDocumentMediaHandler_UploadBounds: oversized bodies, missing files
// and malformed multipart are refused before the service; concurrent
// uploads beyond the bound get 429.
func TestDocumentMediaHandler_UploadBounds(t *testing.T) {
	f := &fakeService{}
	h := NewHandler(f)
	human := signedIn(t, trust.SubjectKindHuman)
	body, contentType := multipartBody(t, "image/png", make([]byte, MaxUploadBytes+1))
	req := human(httptest.NewRequest(http.MethodPost, PathPrefix+"doc-1/upload", body))
	req.Header.Set("Content-Type", contentType)
	if rec := serve(h, req); rec.Code != http.StatusRequestEntityTooLarge || len(f.calls) != 0 {
		t.Fatalf("oversized upload: %d %v", rec.Code, f.calls)
	}
	req = human(httptest.NewRequest(http.MethodPost, PathPrefix+"doc-1/upload", strings.NewReader("nope")))
	req.Header.Set("Content-Type", "text/plain")
	if rec := serve(h, req); rec.Code != 400 {
		t.Fatalf("not multipart: %d", rec.Code)
	}
	var empty bytes.Buffer
	mw := multipart.NewWriter(&empty)
	_ = mw.WriteField("note", "x")
	_ = mw.Close()
	req = human(httptest.NewRequest(http.MethodPost, PathPrefix+"doc-1/upload", &empty))
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if rec := serve(h, req); rec.Code != 400 || len(f.calls) != 0 {
		t.Fatalf("no file: %d", rec.Code)
	}

	gate := &fakeService{block: make(chan struct{})}
	gated := NewHandler(gate)
	done := make(chan int, MaxConcurrentUploads)
	for i := 0; i < MaxConcurrentUploads; i++ {
		b, ct := multipartBody(t, "image/png", []byte("x"))
		r := human(httptest.NewRequest(http.MethodPost, PathPrefix+"doc-1/upload", b))
		r.Header.Set("Content-Type", ct)
		go func() { done <- serve(gated, r).Code }()
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(gated.uploads) < MaxConcurrentUploads && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	b, ct := multipartBody(t, "image/png", []byte("x"))
	r := human(httptest.NewRequest(http.MethodPost, PathPrefix+"doc-1/upload", b))
	r.Header.Set("Content-Type", ct)
	if rec := serve(gated, r); rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("fifth concurrent upload: %d", rec.Code)
	}
	close(gate.block)
	for i := 0; i < MaxConcurrentUploads; i++ {
		if code := <-done; code != http.StatusCreated {
			t.Fatalf("gated upload finished %d", code)
		}
	}
}

func (f *fakeService) record(call string) {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
}
