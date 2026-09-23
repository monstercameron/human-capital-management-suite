package application

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatmedia"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type mediaVerifier struct{ principal *trust.Principal }

func (v mediaVerifier) Verify(_ context.Context, c trust.Credential) (*trust.Principal, error) {
	if c.Token != "valid" {
		return nil, trust.ErrNoCredential
	}
	return v.principal, nil
}
func mediaAdmission(t *testing.T) transport.Config {
	t.Helper()
	now := time.Now()
	p, e := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant"), Subject: "member", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "digest"})
	if e != nil {
		t.Fatal(e)
	}
	return transport.Config{Verifier: mediaVerifier{principal: p}}
}

func TestTodo_CHAT_036_ComposeMediaRequiresArtifactRoot(t *testing.T) {
	if _, err := ComposeChatMedia(ChatMediaConfig{}); err == nil {
		t.Fatal("media composed without durable artifact root")
	}
}

func TestTodo_CHAT_036_MediaHandlerReportsUnavailable(t *testing.T) {
	r := httptest.NewRequest("GET", "/v1/chat/media/id", nil)
	w := httptest.NewRecorder()
	ChatMediaHandler(ChatMediaConfig{}).ServeHTTP(w, r)
	if w.Code != 503 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d headers=%v", w.Code, w.Header())
	}
}

func TestTodo_CHAT_036_MediaOverlayMountsUnavailableRoute(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := OverlayChatMedia(next, ChatMediaConfig{}, mediaAdmission(t))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/v1/chat/media/id", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("media route=%d", w.Code)
	}
	w = httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/chat/media/id", nil)
	r.Header.Set("Authorization", "Bearer valid")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("authenticated unavailable media=%d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/other", nil))
	if w.Code != http.StatusTeapot {
		t.Fatalf("fallback route=%d", w.Code)
	}
}

func TestTodo_CHAT_036_ComposeMediaWithoutScannerIsUnavailable(t *testing.T) {
	h, err := ComposeChatMedia(ChatMediaConfig{ArtifactRoot: filepath.Join(t.TempDir(), "chat_store"), Authorize: func(context.Context, chatmedia.AccessRequest) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/v1/media/id", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("unauthenticated media status=%d", w.Code)
	}
}

// TestTodo_CHAT_036_MediaDefaultsFollowTheArtifactRootConvention proves the
// media root no longer has to be supplied by hand for the route to work: it is
// derived from the artifact-root convention, an explicit root still wins, and
// the configured principal reader is honored instead of being ignored.
func TestTodo_CHAT_036_MediaDefaultsFollowTheArtifactRootConvention(t *testing.T) {
	// Ambient process configuration cannot override the resolved inputs.
	t.Setenv(EnvChatMediaRoot, "ambient-must-not-leak")
	t.Setenv(EnvArtifactRoot, "ambient-must-not-leak")
	if got := DefaultChatMediaArtifactRoot("", ""); got != filepath.Join(".artifacts", "chat-media") {
		t.Fatalf("default root=%q", got)
	}
	if got := DefaultChatMediaArtifactRoot("", filepath.Join("run", "artifacts")); got != filepath.Join("run", "artifacts", "chat-media") {
		t.Fatalf("artifact-root derived media root=%q", got)
	}
	if got := DefaultChatMediaArtifactRoot(filepath.Join("srv", "chat"), "other"); got != filepath.Join("srv", "chat") {
		t.Fatalf("explicit media root=%q", got)
	}
	filled := ChatMediaConfig{}.WithDefaults(filepath.Join("srv", "chat"), "other")
	if filled.ArtifactRoot != filepath.Join("srv", "chat") || filled.Principal == nil {
		t.Fatalf("WithDefaults=%+v", filled)
	}
	if filled.Scanner != nil {
		t.Fatal("WithDefaults supplied a scanner; media must keep failing closed")
	}
	explicit := ChatMediaConfig{ArtifactRoot: "kept"}.WithDefaults("other", "unused")
	if explicit.ArtifactRoot != "kept" {
		t.Fatalf("explicit root overwritten: %q", explicit.ArtifactRoot)
	}

	// The configured principal reader decides the scope, not a hardcoded one.
	used := false
	cfg := ChatMediaConfig{ArtifactRoot: filepath.Join(t.TempDir(), "media"), Authorize: func(context.Context, chatmedia.AccessRequest) error { return nil },
		Principal: func(*http.Request) (string, string, string, bool) {
			used = true
			return "", "", "", false
		}}
	h, err := ComposeChatMedia(cfg)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/v1/chat/media/id", nil))
	if !used {
		t.Fatalf("configured principal reader ignored; status=%d", w.Code)
	}
}
