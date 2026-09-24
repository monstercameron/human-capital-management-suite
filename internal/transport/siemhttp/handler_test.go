package siemhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/subscription"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/siem"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type readFixture struct {
	principal *trust.Principal
	cursor    siem.Cursor
	err       error
}

func (f *readFixture) ReadAuthenticated(_ context.Context, p *trust.Principal, _ string, cursor siem.Cursor, _ int, at time.Time, ring *subscription.CredentialRing) (siem.Feed, error) {
	f.principal, f.cursor = p, cursor
	if f.err != nil {
		return siem.Feed{}, f.err
	}
	return siem.SignPage(cursor.Tenant, cursor, nil, at, ring)
}

type ringFixture struct {
	ring   *subscription.CredentialRing
	called int
}

func (f *ringFixture) ResolveSIEMRing(_ context.Context, _ *trust.Principal, _ string) (*subscription.CredentialRing, error) {
	f.called++
	return f.ring, nil
}

func routeFixture(t *testing.T) (Handler, *readFixture, *ringFixture, string, time.Time) {
	t.Helper()
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: "partner:customer",
		SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceHigh, SessionRef: "session-a", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "verified-credential"})
	if err != nil {
		t.Fatal(err)
	}
	verifier := trust.VerifierFunc(func(_ context.Context, credential trust.Credential) (*trust.Principal, error) {
		if credential.Token != "route-token" {
			return nil, trust.ErrInvalidCredential
		}
		return principal, nil
	})
	provider, err := subscription.NewHMACProvider("siem-route-key", []byte("route-test-secret-material-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	ring, err := subscription.NewCredentialRing("endpoint:customer-siem", func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}
	if err := ring.Add(subscription.SigningCredential{Destination: ring.Destination(), Profile: "hmac-test", Version: "v1", KeyRef: "siem-route-key",
		NotBefore: at.Add(-time.Minute), NotAfter: at.Add(time.Hour), Provider: provider}); err != nil {
		t.Fatal(err)
	}
	if err := ring.Activate("v1", at); err != nil {
		t.Fatal(err)
	}
	reader := &readFixture{}
	rings := &ringFixture{ring: ring}
	handler := Handler{Config: transport.Config{Verifier: verifier, Now: func() time.Time { return at }}, Reader: reader, Rings: rings,
		ClassifyReadError: func(err error) int {
			if errors.Is(err, siem.ErrInvalidCursor) {
				return http.StatusConflict
			}
			return http.StatusForbidden
		}}
	return handler, reader, rings, "Bearer route-token", at
}

func TestTodo_REV_099_05_HTTPRouteRequiresAuthAndReturnsSignedCursor(t *testing.T) {
	handler, reader, rings, bearer, at := routeFixture(t)
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, Path+"?subscription_id=siem", nil))
	if unauthorized.Code != http.StatusUnauthorized || rings.called != 0 || reader.principal != nil {
		t.Fatalf("anonymous request status=%d rings=%d reader=%+v", unauthorized.Code, rings.called, reader)
	}
	cursorDigest := strings.Repeat("a", 64)
	request := httptest.NewRequest(http.MethodGet, Path+"?subscription_id=siem&after=2&digest="+cursorDigest+"&limit=9", nil)
	request.Header.Set(transport.AuthorizationMetadataKey, bearer)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("authenticated request status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	var feed siem.Feed
	if err := json.Unmarshal(response.Body.Bytes(), &feed); err != nil {
		t.Fatal(err)
	}
	wantTenant := pgstore.TenantID("tenant-a").String()
	if reader.principal == nil || reader.principal.Subject() != "partner:customer" || reader.cursor.Tenant != wantTenant ||
		reader.cursor.Sequence != 2 || reader.cursor.Digest != cursorDigest || feed.Tenant != wantTenant || feed.From.Sequence != 2 || rings.called != 1 {
		t.Fatalf("route did not bind auth and cursor: tenant=%s cursor=%+v feed=%+v rings=%d", wantTenant, reader.cursor, feed, rings.called)
	}
	if err := siem.Verify(feed, wantTenant, at, rings.ring); err != nil {
		t.Fatalf("response signature invalid: %v", err)
	}
}

func TestTodo_REV_099_05_HTTPRouteRejectsTenantInjectionAndDetectsCursorGap(t *testing.T) {
	handler, reader, rings, bearer, _ := routeFixture(t)
	for _, target := range []string{
		Path + "?subscription_id=siem&tenant=tenant-b",
		Path + "?subscription_id=siem&after=2",
	} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.Header.Set(transport.AuthorizationMetadataKey, bearer)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("malformed route query %q status=%d body=%s", target, response.Code, response.Body.String())
		}
	}
	if rings.called != 0 || reader.principal != nil {
		t.Fatal("invalid queries reached signer or feed reader")
	}
	reader.err = siem.ErrInvalidCursor
	request := httptest.NewRequest(http.MethodGet, Path+"?subscription_id=siem&after=3&digest="+strings.Repeat("b", 64), nil)
	request.Header.Set(transport.AuthorizationMetadataKey, bearer)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("cursor gap status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestOverlayMountsExactSIEMRouteAndPassesOtherPaths(t *testing.T) {
	feed := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := Overlay(next, feed)
	for _, tc := range []struct {
		path string
		want int
	}{{Path, http.StatusAccepted}, {Path + "/extra", http.StatusNoContent}, {"/other", http.StatusNoContent}} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if response.Code != tc.want {
			t.Fatalf("path %s status=%d want=%d", tc.path, response.Code, tc.want)
		}
	}
}
