package edge

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTodo_EDGE_004(t *testing.T) {
	policy := BrowserPolicy(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), BrowserPolicyOptions{
		AllowedOrigins: []string{"https://trusted.example"},
		AllowedHosts:   []string{"trusted.example"},
	})

	for _, tc := range []struct {
		name   string
		origin string
		host   string
	}{
		{name: "cross-origin", origin: "https://evil.example", host: "trusted.example"},
		{name: "forged-host", origin: "https://trusted.example", host: "evil.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "https://trusted.example/workspace/promotion/simulate", nil)
			req.Host = tc.host
			req.Header.Set("Origin", tc.origin)
			rec := httptest.NewRecorder()
			policy.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "edge.browser_") {
				t.Fatalf("typed refusal body = %q", rec.Body.String())
			}
		})
	}
}

func FuzzTodo_EDGE_004(f *testing.F) {
	f.Add("https://trusted.example", "trusted.example")
	f.Add("null", "trusted.example")
	f.Fuzz(func(t *testing.T, origin, host string) {
		policy := BrowserPolicy(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}), BrowserPolicyOptions{
			AllowedOrigins: []string{"https://trusted.example"},
			AllowedHosts:   []string{"trusted.example"},
		})
		req := httptest.NewRequest(http.MethodPost, "https://trusted.example/workspace/promotion/simulate", nil)
		req.Host = host
		req.Header.Set("Origin", origin)
		policy.ServeHTTP(httptest.NewRecorder(), req)
	})
}

func TestTodo_EDGE_004_Integration(t *testing.T) {
	var called bool
	policy := BrowserPolicy(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}), BrowserPolicyOptions{
		AllowedOrigins: []string{"https://trusted.example"},
		AllowedHosts:   []string{"trusted.example"},
	})

	get := httptest.NewRequest(http.MethodGet, "https://trusted.example/workspace/promotion?worker=w-1", nil)
	get.Host = "trusted.example"
	getRec := httptest.NewRecorder()
	policy.ServeHTTP(getRec, get)
	cookies := getRec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != BrowserCSRFCookieName {
		t.Fatalf("GET cookies = %v, want one browser CSRF cookie", cookies)
	}
	if !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie security attributes = %+v", cookies[0])
	}

	post := httptest.NewRequest(http.MethodPost, "https://trusted.example/workspace/promotion/simulate", nil)
	post.Host = "trusted.example"
	post.Header.Set("Origin", "https://trusted.example")
	post.AddCookie(cookies[0])
	postRec := httptest.NewRecorder()
	policy.ServeHTTP(postRec, post)
	if postRec.Code != http.StatusNoContent || !called {
		t.Fatalf("same-origin journey POST = %d, called=%v; want 204 and called", postRec.Code, called)
	}

	login := httptest.NewRequest(http.MethodPost, "https://trusted.example/workspace/login", nil)
	login.Host = "trusted.example"
	login.Header.Set("Origin", "https://trusted.example")
	login.AddCookie(cookies[0])
	loginRec := httptest.NewRecorder()
	policy.ServeHTTP(loginRec, login)
	if loginRec.Code != http.StatusNoContent {
		t.Fatalf("same-origin dev-login POST = %d, want 204", loginRec.Code)
	}
}

// TestTodo_EDGE_004_NullOrigin proves the opaque-origin POSTs a real browser
// sends back from this edge's own no-referrer documents are governed by the
// SameSite browser token, not refused outright: with the token they pass,
// without it they still fail closed, and a cross-site report stays refused.
func TestTodo_EDGE_004_NullOrigin(t *testing.T) {
	policy := BrowserPolicy(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), BrowserPolicyOptions{})

	cookie := &http.Cookie{Name: BrowserCSRFCookieName, Value: "browser-token"}

	allowed := httptest.NewRequest(http.MethodPost, "https://trusted.example/workspace/login", nil)
	allowed.Host = "trusted.example"
	allowed.Header.Set("Origin", "null")
	allowed.Header.Set("Sec-Fetch-Site", "same-origin")
	allowed.AddCookie(cookie)
	allowedRec := httptest.NewRecorder()
	policy.ServeHTTP(allowedRec, allowed)
	if allowedRec.Code != http.StatusNoContent {
		t.Fatalf("null-origin same-site POST with the browser token = %d, want 204", allowedRec.Code)
	}

	anonymous := httptest.NewRequest(http.MethodPost, "https://trusted.example/workspace/login", nil)
	anonymous.Host = "trusted.example"
	anonymous.Header.Set("Origin", "null")
	anonymousRec := httptest.NewRecorder()
	policy.ServeHTTP(anonymousRec, anonymous)
	if anonymousRec.Code != http.StatusForbidden || !strings.Contains(anonymousRec.Body.String(), "edge.browser_csrf_rejected") {
		t.Fatalf("null-origin POST without the browser token = %d %q, want 403 edge.browser_csrf_rejected",
			anonymousRec.Code, anonymousRec.Body.String())
	}

	crossSite := httptest.NewRequest(http.MethodPost, "https://trusted.example/workspace/login", nil)
	crossSite.Host = "trusted.example"
	crossSite.Header.Set("Origin", "null")
	crossSite.Header.Set("Sec-Fetch-Site", "cross-site")
	crossSite.AddCookie(cookie)
	crossSiteRec := httptest.NewRecorder()
	policy.ServeHTTP(crossSiteRec, crossSite)
	if crossSiteRec.Code != http.StatusForbidden || !strings.Contains(crossSiteRec.Body.String(), "edge.browser_origin_rejected") {
		t.Fatalf("null-origin cross-site POST = %d %q, want 403 edge.browser_origin_rejected",
			crossSiteRec.Code, crossSiteRec.Body.String())
	}
}

func TestTodo_EDGE_004_Security(t *testing.T) {
	policy := BrowserPolicy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/steal", http.StatusSeeOther)
	}), BrowserPolicyOptions{})
	req := httptest.NewRequest(http.MethodGet, "https://trusted.example/", nil)
	rec := httptest.NewRecorder()
	policy.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign redirect status = %d, want 403", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "" {
		t.Fatalf("foreign redirect leaked Location %q", got)
	}
}

func TestTodo_EDGE_004_Browser(t *testing.T) {
	policy := BrowserPolicy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/workspace/app/home", http.StatusSeeOther)
	}), BrowserPolicyOptions{})
	req := httptest.NewRequest(http.MethodGet, "https://trusted.example/", nil)
	rec := httptest.NewRecorder()
	policy.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/workspace/app/home" {
		t.Fatalf("same-origin relative redirect = %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestTodo_EDGE_004_Recovery(t *testing.T) {
	policy := BrowserPolicy(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		w.WriteHeader(http.StatusInternalServerError)
	}), BrowserPolicyOptions{})
	req := httptest.NewRequest(http.MethodGet, "https://trusted.example/workspace/promotion", nil)
	rec := httptest.NewRecorder()
	policy.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("duplicate WriteHeader changed status to %d", rec.Code)
	}
}
