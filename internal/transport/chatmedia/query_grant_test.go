package chatmedia

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	core "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatmedia"
)

// TestTodo_CHAT_036_QueryGrantLoadsMediaInAnImageTag proves the grant can travel
// in the URL. An <img src> cannot send a header, so header-only grants meant a
// post's attachment could never be rendered by the browser at all — the client
// had to fetch and blob every image itself.
func TestTodo_CHAT_036_QueryGrantLoadsMediaInAnImageTag(t *testing.T) {
	now := time.Unix(100, 0)
	h, s := testHandler(&now)
	content := pngBytes("attachment-bytes")
	ref, err := s.Upload(context.Background(), core.UploadRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", DeclaredType: "image/png", Content: content, EvidenceID: "query-grant"})
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.Authorize(context.Background(), core.AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", ArtifactID: ref.ArtifactID}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	get := func(url string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
		return w
	}

	// The whole point: one URL, no header, and the bytes come back.
	w := get("/v1/media/" + ref.ArtifactID + "?grant=" + g.Token)
	if w.Code != http.StatusOK {
		t.Fatalf("query grant status = %d body = %s", w.Code, w.Body)
	}
	if w.Body.Len() != len(content) {
		t.Fatalf("body = %d bytes, want %d", w.Body.Len(), len(content))
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content type = %q", ct)
	}
	// The protections do not weaken because the token moved into the URL.
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache-control = %q, want no-store", w.Header().Get("Cache-Control"))
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("headers = %v", w.Header())
	}

	// A header grant still wins, so a caller that can send one is unaffected by a
	// stale or wrong query parameter.
	r := httptest.NewRequest("GET", "/v1/media/"+ref.ArtifactID+"?grant=not-a-token", nil)
	r.Header.Set("X-Chat-Media-Grant", g.Token)
	headerFirst := httptest.NewRecorder()
	h.ServeHTTP(headerFirst, r)
	if headerFirst.Code != http.StatusOK {
		t.Fatalf("header grant beside a bad query grant = %d", headerFirst.Code)
	}

	// A forged token in the URL is refused exactly as a forged header is.
	if forged := get("/v1/media/" + ref.ArtifactID + "?grant=forged"); forged.Code == http.StatusOK {
		t.Fatal("a forged query grant served bytes")
	}

	// And expiry is still enforced: a URL that leaks is a token that has run out.
	short, err := s.Authorize(context.Background(), core.AccessRequest{TenantID: "t", ConversationID: "c", PrincipalID: "p", ArtifactID: ref.ArtifactID}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if expired := get("/v1/media/" + ref.ArtifactID + "?grant=" + short.Token); expired.Code == http.StatusOK {
		t.Fatalf("an expired query grant served bytes: %d", expired.Code)
	}
}
