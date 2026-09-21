package main

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/edge"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/devprofile"
)

func TestFrontendDevForwardsToLiveCellWithoutRenderingFixtures(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Live-Cell", "true")
		_, _ = w.Write([]byte(strings.Join([]string{
			r.URL.RequestURI(), r.Header.Get("Authorization"), r.Host,
			r.Header.Get("Origin"), r.Header.Get("X-Forwarded-Host"),
			r.Header.Get("X-Forwarded-Proto"),
		}, " ")))
	}))
	t.Cleanup(upstream.Close)
	target, _ := url.Parse(upstream.URL)
	server := httptest.NewServer(frontendHandler(target, "live-token", false))
	t.Cleanup(server.Close)

	request, err := http.NewRequest(http.MethodGet, server.URL+"/workspace/app/work?nav=collapsed", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", server.URL)
	request.Header.Set("X-Forwarded-Host", "spoofed.invalid")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	want := "/workspace/app/work?nav=collapsed Bearer live-token " +
		strings.TrimPrefix(server.URL, "http://") + " " + server.URL + " " +
		strings.TrimPrefix(server.URL, "http://") + " http"
	if response.Header.Get("X-Live-Cell") != "true" || string(body) != want {
		t.Fatalf("gateway did not return the live cell response: header=%q body=%q", response.Header.Get("X-Live-Cell"), body)
	}
	if strings.Contains(string(body), "Maya Chen") {
		t.Fatal("development gateway rendered fixture content")
	}
}

func TestFrontendDevPreservesPublicOriginForBrowserPolicy(t *testing.T) {
	upstream := httptest.NewServer(edge.BrowserPolicy(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), edge.BrowserPolicyOptions{}))
	t.Cleanup(upstream.Close)
	target, _ := url.Parse(upstream.URL)
	gateway := httptest.NewServer(frontendHandler(target, "", false))
	t.Cleanup(gateway.Close)

	jar := newCookieJar(t)
	client := &http.Client{Jar: jar}
	getResponse, err := client.Get(gateway.URL + "/workspace/login")
	if err != nil {
		t.Fatal(err)
	}
	getResponse.Body.Close()
	post, err := http.NewRequest(http.MethodPost, gateway.URL+"/workspace/login", strings.NewReader("persona=admin"))
	if err != nil {
		t.Fatal(err)
	}
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("Origin", gateway.URL)
	post.Header.Set("Sec-Fetch-Site", "same-origin")
	postResponse, err := client.Do(post)
	if err != nil {
		t.Fatal(err)
	}
	defer postResponse.Body.Close()
	if postResponse.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(postResponse.Body)
		t.Fatalf("same-origin browser POST through gateway = %d %s", postResponse.StatusCode, body)
	}
}

func TestFrontendDevPreservesOpaqueOriginPolicyAcrossProfiles(t *testing.T) {
	upstream := httptest.NewServer(edge.BrowserPolicy(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), edge.BrowserPolicyOptions{}))
	t.Cleanup(upstream.Close)
	target, _ := url.Parse(upstream.URL)

	for _, tc := range []struct {
		name       string
		localDev   bool
		crossSite  bool
		wantStatus int
	}{
		{name: "local dev loopback", localDev: true, wantStatus: http.StatusNoContent},
		{name: "standard profile with CSRF cookie", localDev: false, wantStatus: http.StatusNoContent},
		{name: "standard profile cross-site", localDev: false, crossSite: true, wantStatus: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gateway := httptest.NewServer(frontendHandler(target, "", tc.localDev))
			defer gateway.Close()
			client := &http.Client{Jar: newCookieJar(t)}
			getResponse, err := client.Get(gateway.URL + "/workspace/login")
			if err != nil {
				t.Fatal(err)
			}
			getResponse.Body.Close()

			post, err := http.NewRequest(http.MethodPost, gateway.URL+"/workspace/login", strings.NewReader("persona=admin"))
			if err != nil {
				t.Fatal(err)
			}
			post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			post.Header.Set("Origin", "null")
			if tc.crossSite {
				post.Header.Set("Sec-Fetch-Site", "cross-site")
			}
			postResponse, err := client.Do(post)
			if err != nil {
				t.Fatal(err)
			}
			defer postResponse.Body.Close()
			if postResponse.StatusCode != tc.wantStatus {
				body, _ := io.ReadAll(postResponse.Body)
				t.Fatalf("opaque-origin POST = %d %s, want %d", postResponse.StatusCode, body, tc.wantStatus)
			}
		})
	}
}

func newCookieJar(t *testing.T) http.CookieJar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return jar
}

func TestFrontendDevRedirectsLegacyPreviewRoutesToProductionWorkspace(t *testing.T) {
	target, _ := url.Parse("http://cell.invalid")
	request := httptest.NewRequest(http.MethodGet, "/app/home?nav=collapsed", nil)
	recorder := httptest.NewRecorder()
	frontendHandler(target, "", false).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTemporaryRedirect || recorder.Header().Get("Location") != "/workspace/app/home?nav=collapsed" {
		t.Fatalf("legacy redirect = %d %q", recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestLocalDevProfileMintsTheRealAuthenticatedPrincipal(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	token, err := developmentBearer(devprofile.Name, "", "", devprofile.Tenant, now)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key: []byte(devprofile.HMACKey), Issuer: devprofile.Issuer, Audience: devprofile.Audience,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: token})
	if err != nil {
		t.Fatalf("profile credential does not pass the production verifier: %v", err)
	}
	if principal.Subject() != devprofile.Subject || principal.Tenant() != devprofile.Tenant {
		t.Fatalf("profile principal = subject %q tenant %q", principal.Subject(), principal.Tenant())
	}
}

func TestGatewayBearerLeavesLocalDevBrowserForPersonaLogin(t *testing.T) {
	got, err := gatewayBearer(devprofile.Name, "", "", devprofile.Tenant, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatal("local-dev gateway injected a bearer instead of showing persona login")
	}
	explicit, err := gatewayBearer(devprofile.Name, " supplied ", "", devprofile.Tenant, time.Now())
	if err != nil || explicit != "supplied" {
		t.Fatalf("explicit bearer = %q, %v", explicit, err)
	}
}

func TestLocalDevProfilePreservesAnExplicitBearerAndLoopbackBoundary(t *testing.T) {
	got, err := developmentBearer(devprofile.Name, " supplied ", "bad", "", time.Time{})
	if err != nil || got != "supplied" {
		t.Fatalf("explicit bearer = %q, %v", got, err)
	}
	if !devprofile.IsLoopbackAddress("127.0.0.1:8768") || !devprofile.IsLoopbackHost("::1") || devprofile.IsLoopbackAddress("0.0.0.0:8768") || devprofile.IsLoopbackHost("dev.example") {
		t.Fatal("local-dev loopback boundary classification is incorrect")
	}
	if _, err := developmentBearer("production-ish", "", "", devprofile.Tenant, time.Now()); err == nil {
		t.Fatal("unknown profile was accepted")
	}
}
