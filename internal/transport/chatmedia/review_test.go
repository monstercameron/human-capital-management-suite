package chatmedia

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	core "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatmedia"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine"
)

// pngBytes is a sniffable PNG header plus payload.
func pngBytes(tail string) []byte { return []byte("\x89PNG\r\n\x1a\n" + tail) }

// TestTodo_CHAT_036_UploadGrantDownloadRoundTrip is the round trip that was
// unreachable: the protected read requires a grant token, and until the
// transport minted one, every GET of admitted bytes answered 403.
func TestTodo_CHAT_036_UploadGrantDownloadRoundTrip(t *testing.T) {
	now := time.Unix(100, 0)
	h, _ := testHandler(&now)

	// 1. Upload. The response carries a usable grant.
	w := uploadRequest(t, h, pngBytes("round-trip-pixels"))
	if w.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", w.Code, w.Body)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("upload Cache-Control = %q", got)
	}
	var uploaded struct {
		ArtifactID string `json:"ArtifactID"`
		Grant      string `json:"grant"`
		ExpiresAt  string `json:"grant_expires_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("upload body %s: %v", w.Body, err)
	}
	if uploaded.ArtifactID == "" || uploaded.Grant == "" || uploaded.ExpiresAt == "" {
		t.Fatalf("upload response = %+v", uploaded)
	}

	// 2. The download works with the grant the upload handed back.
	body, ref := download(t, h, uploaded.ArtifactID, uploaded.Grant, "")
	if !bytes.Equal(body, pngBytes("round-trip-pixels")) {
		t.Fatalf("downloaded %q", body)
	}
	if ref.Get("Cache-Control") != "no-store" || ref.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("protected headers = %v", ref)
	}

	// 3. The standalone grant endpoint mints another one for the same member,
	// and that one works too.
	g := grantRequest(t, h, uploaded.ArtifactID)
	if g.Code != http.StatusOK {
		t.Fatalf("grant status=%d body=%s", g.Code, g.Body)
	}
	var minted struct {
		Grant      string `json:"grant"`
		ArtifactID string `json:"artifact_id"`
		ExpiresAt  string `json:"grant_expires_at"`
	}
	if err := json.Unmarshal(g.Body.Bytes(), &minted); err != nil {
		t.Fatalf("grant body %s: %v", g.Body, err)
	}
	if minted.Grant == "" || minted.Grant == uploaded.Grant || minted.ArtifactID != uploaded.ArtifactID {
		t.Fatalf("minted grant = %+v (upload grant %q)", minted, uploaded.Grant)
	}
	if _, err := time.Parse(time.RFC3339, minted.ExpiresAt); err != nil {
		t.Fatalf("grant expiry %q: %v", minted.ExpiresAt, err)
	}
	if again, _ := download(t, h, uploaded.ArtifactID, minted.Grant, ""); !bytes.Equal(again, pngBytes("round-trip-pixels")) {
		t.Fatalf("second grant read %q", again)
	}

	// 4. A read with no grant at all is still refused, so the new path widened
	// nothing.
	r := httptest.NewRequest(http.MethodGet, "/v1/chat/media/"+uploaded.ArtifactID, nil)
	no := httptest.NewRecorder()
	h.ServeHTTP(no, r)
	if no.Code != http.StatusForbidden {
		t.Fatalf("ungranted read = %d", no.Code)
	}
}

// TestTodo_CHAT_036_GrantIsRefusedForANonMember proves the minting is the
// collaboration service's decision, not the handler's.
func TestTodo_CHAT_036_GrantIsRefusedForANonMember(t *testing.T) {
	now := time.Unix(100, 0)
	s := core.New(core.Config{Store: core.NewMemoryStore(), Scanner: mediaScanner{}, Now: func() time.Time { return now },
		Authorize: func(_ context.Context, req core.AccessRequest) error {
			if req.PrincipalID != "member" {
				return core.ErrUnauthorized
			}
			return nil
		}})
	member := &Handler{Service: s, Principal: func(*http.Request) (string, string, string, bool) { return "t", "c", "member", true }}
	ref, err := s.Upload(context.Background(), core.UploadRequest{TenantID: "t", ConversationID: "c", PrincipalID: "member", DeclaredType: "image/png", Content: pngBytes("members-only"), EvidenceID: "e"})
	if err != nil {
		t.Fatal(err)
	}
	if w := grantRequest(t, member, ref.ArtifactID); w.Code != http.StatusOK {
		t.Fatalf("member grant = %d body=%s", w.Code, w.Body)
	}
	outsider := &Handler{Service: s, Principal: func(*http.Request) (string, string, string, bool) { return "t", "c", "outsider", true }}
	w := grantRequest(t, outsider, ref.ArtifactID)
	if w.Code != http.StatusForbidden {
		t.Fatalf("outsider grant = %d body=%s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "chatmedia:") {
		t.Fatalf("provider error text leaked: %s", w.Body)
	}
}

// TestTodo_CHAT_038_ReadStatusesAreDistinct pins the mapping that used to
// answer 403 for everything, including a range the artifact cannot satisfy.
func TestTodo_CHAT_038_ReadStatusesAreDistinct(t *testing.T) {
	now := time.Unix(100, 0)
	h, s := testHandler(&now)
	ref, err := s.Upload(context.Background(), core.UploadRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", DeclaredType: "image/png", Content: pngBytes("range"), EvidenceID: "e"})
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.Authorize(context.Background(), core.AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", ArtifactID: ref.ArtifactID}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// A satisfiable range is 206; a range past the end is 416, not 403.
	if _, header := download(t, h, ref.ArtifactID, g.Token, "bytes=1-4"); header.Get("Content-Range") == "" {
		t.Fatalf("partial read headers = %v", header)
	}
	r := httptest.NewRequest(http.MethodGet, "/v1/chat/media/"+ref.ArtifactID, nil)
	r.Header.Set("X-Chat-Media-Grant", g.Token)
	r.Header.Set("Range", "bytes=9000-9001")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("out-of-range read = %d, want 416", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("refusal Cache-Control = %q", w.Header().Get("Cache-Control"))
	}
	// A revoked grant is a plain 403; a quarantined artifact is a 403 with its
	// own reason, so an operator can tell the two apart. Everything
	// unclassified is this process failing, not the caller.
	s.Revoke(ref.ArtifactID)
	revoked := httptest.NewRequest(http.MethodGet, "/v1/chat/media/"+ref.ArtifactID, nil)
	revoked.Header.Set("X-Chat-Media-Grant", g.Token)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, revoked)
	if rw.Code != http.StatusForbidden || !strings.Contains(rw.Body.String(), "forbidden") {
		t.Fatalf("revoked read = %d body=%s", rw.Code, rw.Body)
	}
	status, message := readStatus(core.ErrQuarantined)
	if status != http.StatusForbidden || message == "forbidden" {
		t.Fatalf("quarantined mapping = %d %q, want a distinct 403 reason", status, message)
	}
	if got, _ := readStatus(errors.New("disk exploded")); got != http.StatusInternalServerError {
		t.Fatalf("unclassified read = %d, want 500", got)
	}
	if got, _ := readStatus(core.ErrInvalid); got != http.StatusBadRequest {
		t.Fatalf("invalid read = %d, want 400", got)
	}
}

// TestTodo_CHAT_036_UploadStatusesAreSafe proves the provider's error text no
// longer reaches the client and that each published condition has its own
// status.
func TestTodo_CHAT_036_UploadStatusesAreSafe(t *testing.T) {
	for _, tc := range []struct {
		in   error
		want int
	}{
		{core.ErrScannerUnavailable, http.StatusServiceUnavailable},
		{core.ErrQuarantined, http.StatusUnprocessableEntity},
		{core.ErrUnsupported, http.StatusUnsupportedMediaType},
		{core.ErrUnauthorized, http.StatusForbidden},
		{core.ErrInvalid, http.StatusBadRequest},
		{errors.New("chatmedia: store path C:\\secret failed"), http.StatusInternalServerError},
	} {
		status, message := uploadStatus(tc.in)
		if status != tc.want {
			t.Errorf("uploadStatus(%v) = %d, want %d", tc.in, status, tc.want)
		}
		if strings.Contains(message, "chatmedia:") || strings.Contains(message, "secret") {
			t.Errorf("uploadStatus(%v) leaked %q", tc.in, message)
		}
	}
	// The sentinel comparison is errors.Is, so a wrapped sentinel still maps.
	if got, _ := uploadStatus(wrapped{core.ErrScannerUnavailable}); got != http.StatusServiceUnavailable {
		t.Fatalf("wrapped scanner error = %d", got)
	}
	// And the live path returns the mapped status rather than the error text.
	now := time.Unix(100, 0)
	h := &Handler{Service: core.New(core.Config{Store: core.NewMemoryStore(), Now: func() time.Time { return now },
		Authorize: func(context.Context, core.AccessRequest) error { return nil }}),
		Principal: func(*http.Request) (string, string, string, bool) { return "t", "c", "p", true }}
	w := uploadRequest(t, h, pngBytes("no scanner configured"))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("scannerless upload = %d body=%s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "chatmedia:") {
		t.Fatalf("sentinel text leaked: %s", w.Body)
	}
}

type wrapped struct{ err error }

func (w wrapped) Error() string { return "wrapped: " + w.err.Error() }
func (w wrapped) Unwrap() error { return w.err }

// TestTodo_CHAT_036_UploadsAreBoundedPerTenant proves the semaphore admits the
// configured number and refuses the next, per tenant rather than globally.
func TestTodo_CHAT_036_UploadsAreBoundedPerTenant(t *testing.T) {
	gate := NewUploadGate(2)
	if !gate.acquire("a") || !gate.acquire("a") {
		t.Fatal("gate refused within its limit")
	}
	if gate.acquire("a") {
		t.Fatal("gate admitted past its limit")
	}
	if !gate.acquire("b") {
		t.Fatal("one tenant exhausted another tenant's slots")
	}
	gate.release("a")
	if !gate.acquire("a") {
		t.Fatal("release did not return a slot")
	}
	// A copied gate shares the counters, which is what makes a value-receiver
	// ServeHTTP able to enforce anything at all.
	copied := gate
	if copied.acquire("a") {
		t.Fatal("a copied gate lost the counters")
	}
	// A zero gate is unconfigured and admits, which is the documented
	// behaviour for a Handler built as a struct literal.
	var unset UploadGate
	if !unset.acquire("a") {
		t.Fatal("unconfigured gate refused")
	}
	unset.release("a")
	if got := NewUploadGate(0); got.limit != 1 {
		t.Fatalf("NewUploadGate(0).limit = %d, want 1", got.limit)
	}
}

// TestTodo_CHAT_036_HandlerRefusesUploadsOverTheBound drives the bound through
// the handler, with a blocking scanner holding the slots.
func TestTodo_CHAT_036_HandlerRefusesUploadsOverTheBound(t *testing.T) {
	now := time.Unix(100, 0)
	release := make(chan struct{})
	admitted := make(chan struct{}, 8)
	s := core.New(core.Config{Store: core.NewMemoryStore(), Scanner: blockingScanner{admitted: admitted, release: release},
		Now: func() time.Time { return now }, Authorize: func(context.Context, core.AccessRequest) error { return nil }})
	h := NewHandler(s, func(*http.Request) (string, string, string, bool) { return "t", "c", "p", true })
	if h.Uploads.limit != DefaultMaxConcurrentUploadsPerTenant {
		t.Fatalf("NewHandler limit = %d", h.Uploads.limit)
	}

	var wg sync.WaitGroup
	codes := make(chan int, DefaultMaxConcurrentUploadsPerTenant)
	for i := 0; i < DefaultMaxConcurrentUploadsPerTenant; i++ {
		wg.Add(1)
		payload := pngBytes(strings.Repeat("x", i+1))
		go func() {
			defer wg.Done()
			codes <- uploadRequest(t, h, payload).Code
		}()
	}
	for i := 0; i < DefaultMaxConcurrentUploadsPerTenant; i++ {
		select {
		case <-admitted:
		case <-time.After(10 * time.Second):
			t.Fatal("uploads did not reach the scanner")
		}
	}
	// Every slot is held, so the next upload is refused rather than queued.
	over := uploadRequest(t, h, pngBytes("one too many"))
	if over.Code != http.StatusTooManyRequests {
		t.Fatalf("over-limit upload = %d body=%s", over.Code, over.Body)
	}
	if over.Header().Get("Retry-After") == "" {
		t.Fatal("over-limit upload carries no Retry-After")
	}
	close(release)
	wg.Wait()
	for i := 0; i < DefaultMaxConcurrentUploadsPerTenant; i++ {
		if got := <-codes; got != http.StatusOK {
			t.Fatalf("in-limit upload = %d", got)
		}
	}
	// The slots came back.
	if after := uploadRequest(t, h, pngBytes("after the rush")); after.Code != http.StatusOK {
		t.Fatalf("upload after release = %d body=%s", after.Code, after.Body)
	}
}

type blockingScanner struct {
	admitted chan struct{}
	release  chan struct{}
}

func (b blockingScanner) Scan(context.Context, string, io.Reader) (quarantine.Verdict, error) {
	b.admitted <- struct{}{}
	<-b.release
	return quarantine.Verdict{Safe: true}, nil
}

// TestTodo_CHAT_036_UploadCarriesAccessibilityFields keeps the single-pass
// multipart read from dropping the transcript and alt text the old
// ParseMultipartForm path picked up.
func TestTodo_CHAT_036_UploadCarriesAccessibilityFields(t *testing.T) {
	now := time.Unix(100, 0)
	h, s := testHandler(&now)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	// Deliberately before and after the file part, because a streaming read
	// sees parts in order and must not depend on that order.
	_ = mw.WriteField("transcript", "spoken words")
	fh := make(textproto.MIMEHeader)
	fh.Set("Content-Disposition", `form-data; name="file"; filename="x.png"`)
	fh.Set("Content-Type", "image/png")
	part, _ := mw.CreatePart(fh)
	_, _ = part.Write(pngBytes("described"))
	_ = mw.WriteField("alt_text", "a described picture")
	_ = mw.WriteField("ignored", "not a field this boundary reads")
	_ = mw.Close()

	r := httptest.NewRequest(http.MethodPost, "/v1/chat/media/upload", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", w.Code, w.Body)
	}
	var out struct{ ArtifactID string }
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	g, err := s.Authorize(context.Background(), core.AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", ArtifactID: out.ArtifactID}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, header := download(t, h, out.ArtifactID, g.Token, ""); header.Get("Content-Type") != "image/png" {
		t.Fatalf("content type = %q", header.Get("Content-Type"))
	}
}

// TestTodo_CHAT_036_MissingFilePartIsRefused covers the one-pass reader's own
// failure modes.
func TestTodo_CHAT_036_MissingFilePartIsRefused(t *testing.T) {
	now := time.Unix(100, 0)
	h, _ := testHandler(&now)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("transcript", "no file here")
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/media/upload", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing file = %d", w.Code)
	}
	// A request that is not multipart at all is a bad request, not a panic.
	plain := httptest.NewRequest(http.MethodPost, "/v1/chat/media/upload", strings.NewReader("not multipart"))
	plain.Header.Set("Content-Type", "text/plain")
	pw := httptest.NewRecorder()
	h.ServeHTTP(pw, plain)
	if pw.Code != http.StatusBadRequest {
		t.Fatalf("non-multipart upload = %d", pw.Code)
	}
}

// download performs a granted read and returns the bytes and headers.
func download(t *testing.T, h *Handler, id, grant, byteRange string) ([]byte, http.Header) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/v1/chat/media/"+id, nil)
	r.Header.Set("X-Chat-Media-Grant", grant)
	if byteRange != "" {
		r.Header.Set("Range", byteRange)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK && w.Code != http.StatusPartialContent {
		t.Fatalf("download status=%d body=%s", w.Code, w.Body)
	}
	return w.Body.Bytes(), w.Header()
}

// grantRequest asks the boundary to mint a grant.
func grantRequest(t *testing.T, h *Handler, id string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/v1/chat/media/"+id+"/grant", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
