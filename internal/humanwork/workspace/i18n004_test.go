package workspace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/i18n"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type i18n004CatalogStore struct {
	revision i18n.CatalogRevision
	pending  map[string]i18n.CatalogRevision
	scope    i18n.Scope
}

func (s *i18n004CatalogStore) Publish(_ context.Context, scope i18n.Scope, revision i18n.CatalogRevision) error {
	s.scope = scope
	if s.pending == nil {
		s.pending = make(map[string]i18n.CatalogRevision)
	}
	s.pending[revision.ID] = revision
	return nil
}
func (s *i18n004CatalogStore) Activate(_ context.Context, _ i18n.Scope, revisionID, _ string) error {
	revision, ok := s.pending[revisionID]
	if !ok {
		return i18n.ErrNoActiveRevision
	}
	s.revision = revision
	return nil
}
func (s *i18n004CatalogStore) Active(_ context.Context, scope i18n.Scope, locale string, at time.Time) (i18n.CatalogRevision, error) {
	if scope.Tenant != shellTenant || scope.Product != "workspace" {
		return i18n.CatalogRevision{}, i18n.ErrInvalidScope
	}
	if locale != "en-US" {
		return i18n.CatalogRevision{}, i18n.ErrNoActiveRevision
	}
	return s.revision, nil
}

func TestTodo_I18N_004_ServedPublication(t *testing.T) {
	at := shellNow.UTC().Add(-time.Minute)
	makeRevision := func(id, previous, text string) i18n.CatalogRevision {
		revision := i18n.CatalogRevision{ID: id, PreviousRevision: previous, Locale: "en-US", Version: id, CreatedAt: at, EffectiveFrom: at,
			Translations: []i18n.Translation{{Key: "page.home.title", Text: text, MeaningID: "page.home.title", Source: "reviewed/home-title", Classification: "UI", EffectiveFrom: at}}}
		revision.CanonicalDigest = i18n.DigestRevision(revision)
		revision.Digest = revision.CanonicalDigest
		return revision
	}
	store := &i18n004CatalogStore{}
	h, token := newShellHandler(t, false)
	h.catalogs = store
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: shellSigningKey, Issuer: shellIssuer, Audience: shellAudience, Now: func() time.Time { return shellNow }})
	if err != nil {
		t.Fatal(err)
	}
	token, err = verifier.Issue(trust.Claims{Issuer: shellIssuer, Audience: shellAudience, Subject: "catalog-publisher", SubjectKind: "human", Tenant: shellTenant,
		OrganizationScopeID: "org-north-america", Roles: []string{productui.RoleHCMAdmin}, Purposes: []string{shellPurpose}, AuthenticationMethod: "bearer_token", Assurance: "substantial",
		SessionRef: "catalog-publisher-session", IssuedAtUnix: shellNow.Add(-time.Minute).Unix(), ExpiresAtUnix: shellNow.Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	publish := func(revision i18n.CatalogRevision) {
		t.Helper()
		body, err := json.Marshal(revision)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "http://cell.test"+PathCatalogPublication, strings.NewReader(string(body)))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("POST catalog publication = %d: %s", response.Code, response.Body.String())
		}
	}
	publish(makeRevision("served-r1", "", "First reviewed title"))
	serve := func() string {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathProductHome, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET product page = %d", response.Code)
		}
		return response.Body.String()
	}
	first := serve()
	if !strings.Contains(first, "First reviewed title") || !strings.Contains(first, `data-hcm-catalog-revision="served-r1"`) {
		t.Fatalf("first activated presentation missing: %s", first)
	}
	publish(makeRevision("served-r2", "served-r1", "Second reviewed title"))
	second := serve()
	if !strings.Contains(second, "Second reviewed title") || !strings.Contains(second, `data-hcm-catalog-revision="served-r2"`) {
		t.Fatalf("served page did not resolve newly activated revision: %s", second)
	}
	if strings.Contains(second, "First reviewed title") {
		t.Fatal("served page retained stale presentation text after activation")
	}
}

func TestTodo_I18N_004_PublicationSecurity(t *testing.T) {
	h, _ := newShellHandler(t, false)
	h.catalogs = &i18n004CatalogStore{}
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: shellSigningKey, Issuer: shellIssuer, Audience: shellAudience, Now: func() time.Time { return shellNow }})
	if err != nil {
		t.Fatal(err)
	}
	issue := func(tenant string, roles []string) string {
		t.Helper()
		token, err := verifier.Issue(trust.Claims{Issuer: shellIssuer, Audience: shellAudience, Subject: "catalog-viewer", SubjectKind: "human", Tenant: tenant,
			OrganizationScopeID: "org-north-america", Roles: roles, Purposes: []string{shellPurpose}, AuthenticationMethod: "bearer_token", Assurance: "substantial",
			SessionRef: "catalog-viewer-session", IssuedAtUnix: shellNow.Add(-time.Minute).Unix(), ExpiresAtUnix: shellNow.Add(time.Hour).Unix()})
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	at := shellNow.UTC()
	revision := i18n.CatalogRevision{ID: "secure-revision", Locale: "en-US", Version: "secure-revision", CreatedAt: at, EffectiveFrom: at,
		Translations: []i18n.Translation{{Key: "page.home.title", Text: "Secure title", MeaningID: "page.home.title", Source: "reviewed/home-title", Classification: "UI", EffectiveFrom: at}}}
	revision.CanonicalDigest = i18n.DigestRevision(revision)
	revision.Digest = revision.CanonicalDigest
	body, err := json.Marshal(revision)
	if err != nil {
		t.Fatal(err)
	}
	request := func(tenant string, roles []string, target, payload string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "http://cell.test"+target, strings.NewReader(payload))
		r.Header.Set("Authorization", "Bearer "+issue(tenant, roles))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if denied := request(shellTenant, []string{"worker_self"}, PathCatalogPublication, string(body)); denied.Code != http.StatusForbidden {
		t.Fatalf("non-admin publication status = %d, want 403: %s", denied.Code, denied.Body.String())
	}
	if forged := request("other-tenant", []string{productui.RoleHCMAdmin}, PathCatalogPublication, strings.TrimSuffix(string(body), "}")+`,"tenant":"`+shellTenant+`"}`); forged.Code != http.StatusBadRequest {
		t.Fatalf("caller-selected tenant publication status = %d, want 400: %s", forged.Code, forged.Body.String())
	}
	if store := h.catalogs.(*i18n004CatalogStore); store.revision.ID != "" || len(store.pending) != 0 {
		t.Fatal("denied or forged catalog publication mutated the store")
	}
	if accepted := request(shellTenant, []string{productui.RoleHCMAdmin}, PathCatalogPublication+"?tenant=other-tenant&product=other", string(body)); accepted.Code != http.StatusCreated {
		t.Fatalf("scoped admin publication status = %d, want 201: %s", accepted.Code, accepted.Body.String())
	}
	if store := h.catalogs.(*i18n004CatalogStore); store.scope != (i18n.Scope{Tenant: shellTenant, Product: "workspace"}) {
		t.Fatalf("published catalog scope = %+v, want trusted tenant and fixed product", store.scope)
	}
}
