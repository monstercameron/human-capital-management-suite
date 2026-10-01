package application

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

type localBootstrapSurfaceFake struct{ calls int }

func (s *localBootstrapSurfaceFake) Bootstrap(context.Context) (LocalDevPersonaBootstrapReceipt, error) {
	s.calls++
	return LocalDevPersonaBootstrapReceipt{}, nil
}
func TestTodo_AGENTP_023_LocalPolicyBootstrapHTTPBoundary(t *testing.T) {
	surface := &localBootstrapSurfaceFake{}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := OverlayLocalDevPersonaBootstrap(next, surface, transport.Config{})
	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, LocalDevPersonaBootstrapPath, nil))
	if get.Code != http.StatusMethodNotAllowed || get.Header().Get("Allow") != http.MethodPost || surface.calls != 0 {
		t.Fatalf("method rejection=%d calls=%d", get.Code, surface.calls)
	}
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, httptest.NewRequest(http.MethodPost, LocalDevPersonaBootstrapPath, nil))
	if denied.Code < 400 || surface.calls != 0 {
		t.Fatalf("unauthenticated bootstrap=%d calls=%d", denied.Code, surface.calls)
	}
	forwarded := httptest.NewRecorder()
	handler.ServeHTTP(forwarded, httptest.NewRequest(http.MethodGet, "/ordinary-route", nil))
	if forwarded.Code != http.StatusNoContent || surface.calls != 0 {
		t.Fatalf("ordinary route=%d calls=%d", forwarded.Code, surface.calls)
	}
	if _, err := (*personaServeWiring)(nil).localDevBootstrap(ServeProfileLocalDev, nil, nil, nil, nil); err == nil {
		t.Fatal("incomplete bootstrap composition accepted")
	}
	if _, err := (&localDevPersonaBootstrapSurface{}).Bootstrap(context.Background()); err == nil {
		t.Fatal("incomplete bootstrap surface accepted")
	}
	browser := OverlayLocalDevPersonaBootstrap(next, surface, transport.Config{}, LocalDevPersonaBootstrapBrowserOptions{PublicOrigin: "http://persona.test", BrowserLogin: true})
	request := httptest.NewRequest(http.MethodPost, "http://persona.test"+LocalDevPersonaBootstrapPath, nil)
	request.AddCookie(&http.Cookie{Name: "hcmnext_session", Value: "presented-credential"})
	proof := httptest.NewRecorder()
	browser.ServeHTTP(proof, request)
	if proof.Code != http.StatusForbidden || !strings.Contains(proof.Body.String(), "browser proof required") || surface.calls != 0 {
		t.Fatalf("cookie mutation without proof code=%d body=%s calls=%d", proof.Code, proof.Body.String(), surface.calls)
	}
	request = httptest.NewRequest(http.MethodPost, "http://persona.test"+LocalDevPersonaBootstrapPath, nil)
	request.Header.Set("Origin", "http://other.test")
	request.Header.Set("Authorization", "Bearer presented-credential")
	cross := httptest.NewRecorder()
	browser.ServeHTTP(cross, request)
	if cross.Code != http.StatusForbidden || surface.calls != 0 {
		t.Fatalf("cross-origin bootstrap code=%d calls=%d", cross.Code, surface.calls)
	}
}
