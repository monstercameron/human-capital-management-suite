package workspace

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type i18n005Preferences struct {
	bySubject map[string]string
	loadErr   error
}

func (s i18n005Preferences) Load(_ context.Context, _ values.TenantId, _ string, subject string) (preferences.Snapshot, error) {
	if s.loadErr != nil {
		return preferences.Snapshot{}, s.loadErr
	}
	snapshot := preferences.DefaultSnapshot()
	snapshot.User.Locale = s.bySubject[subject]
	return snapshot, nil
}

func (i18n005Preferences) SaveUser(context.Context, values.TenantId, string, preferences.User) (preferences.User, error) {
	panic("not used")
}
func (i18n005Preferences) SaveTheme(context.Context, values.TenantId, string, string, preferences.TenantTheme) (preferences.TenantTheme, error) {
	panic("not used")
}
func (i18n005Preferences) SaveOrganizationVisibility(context.Context, values.TenantId, string, string, preferences.OrganizationVisibility) (preferences.OrganizationVisibility, error) {
	panic("not used")
}
func (i18n005Preferences) RecordWorkflowUse(context.Context, values.TenantId, string, string) (preferences.User, error) {
	panic("not used")
}

func newI18N005Handler(t *testing.T, locales map[string]string) *Handler {
	t.Helper()
	h, _ := newShellHandler(t, false)
	h.preferences = i18n005Preferences{bySubject: locales}
	return h
}

func i18n005Request(t *testing.T, h *Handler, subject string, roles []string, path string) *httptest.ResponseRecorder {
	t.Helper()
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: shellSigningKey, Issuer: shellIssuer, Audience: shellAudience, Now: func() time.Time { return shellNow }})
	if err != nil {
		t.Fatal(err)
	}
	token, err := verifier.Issue(trust.Claims{Issuer: shellIssuer, Audience: shellAudience, Subject: subject, SubjectKind: "human", Tenant: shellTenant, OrganizationScopeID: "org:acme:people", Roles: roles, Purposes: []string{"self_service_view"}, AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: "session-" + subject, IssuedAtUnix: shellNow.Add(-time.Minute).Unix(), ExpiresAtUnix: shellNow.Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://cell.test"+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)
	return recorder
}

func TestTodo_I18N_005(t *testing.T) {
	h := newI18N005Handler(t, map[string]string{"user-journey": "ar"})
	recorder := i18n005Request(t, h, "user-journey", []string{"comp_admin"}, PathProductHome)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET product shell = %d: %s", recorder.Code, recorder.Body.String())
	}
	doc := recorder.Body.String()
	for _, want := range []string{`<html lang="ar" dir="rtl" data-hcm-locale="ar" data-hcm-catalog="product-ui.v1"`, `<title>الرئيسية</title>`, "جارٍ تحميل بيانات مساحة عملك"} {
		if !strings.Contains(doc, want) {
			t.Errorf("Arabic first paint missing %q", want)
		}
	}
}

func TestTodo_I18N_005_Integration(t *testing.T) {
	h := newI18N005Handler(t, map[string]string{"alice": "ar", "bob": "de-DE"})
	arabic := i18n005Request(t, h, "alice", []string{"comp_admin"}, PathProductHome)
	german := i18n005Request(t, h, "bob", []string{"comp_admin"}, PathProductHome)
	for _, test := range []struct {
		name, document string
		want           []string
		forbid         string
	}{
		{"Alice", arabic.Body.String(), []string{`<html lang="ar" dir="rtl" data-hcm-locale="ar"`, `<title>الرئيسية</title>`, "جارٍ تحميل بيانات مساحة عملك"}, "Ihre Arbeitsbereichsdaten"},
		{"Bob", german.Body.String(), []string{`<html lang="de-DE" dir="ltr" data-hcm-locale="de-DE"`, `<title>Start</title>`, "Ihre Arbeitsbereichsdaten werden geladen"}, "جارٍ تحميل بيانات مساحة عملك"},
	} {
		if test.document == "" {
			t.Fatalf("%s received an empty document", test.name)
		}
		for _, want := range test.want {
			if !strings.Contains(test.document, want) {
				t.Errorf("%s first paint missing %q", test.name, want)
			}
		}
		if strings.Contains(test.document, test.forbid) {
			t.Errorf("%s document contains another principal's loading language %q", test.name, test.forbid)
		}
	}
	if arabic.Code != http.StatusOK || german.Code != http.StatusOK {
		t.Fatalf("same-tenant principal statuses = %d and %d", arabic.Code, german.Code)
	}

	explicit := i18n005Request(t, h, "alice", []string{"comp_admin"}, PathProductHome+"?locale=de-DE")
	if explicit.Code != http.StatusOK || !strings.Contains(explicit.Body.String(), `<html lang="de-DE" dir="ltr" data-hcm-locale="de-DE"`) || !strings.Contains(explicit.Body.String(), `<title>Start</title>`) {
		t.Fatal("explicit supported URL locale did not take precedence over saved Arabic locale")
	}
}

func TestTodo_I18N_005_Security(t *testing.T) {
	h := newI18N005Handler(t, map[string]string{"worker": "ar"})
	base := i18n005Request(t, h, "worker", []string{"worker_self"}, PathProductHome)
	forged := i18n005Request(t, h, "worker", []string{"worker_self"}, PathProductHome+"?locale=tenant-admin")
	for _, recorder := range []*httptest.ResponseRecorder{base, forged} {
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET workspace with locale variation = %d: %s", recorder.Code, recorder.Body.String())
		}
	}
	if !strings.Contains(forged.Body.String(), `<html lang="ar" dir="rtl" data-hcm-locale="ar"`) || !strings.Contains(forged.Body.String(), `data-hcm-locale-fallback="unsupported_locale"`) {
		t.Fatal("forged unsupported URL locale did not fall through to the authenticated user's saved locale")
	}
	baseConfig, forgedConfig := island(t, base.Body.String()), island(t, forged.Body.String())
	if baseConfig.Tenant != forgedConfig.Tenant || baseConfig.Subject != forgedConfig.Subject || baseConfig.Purpose != forgedConfig.Purpose || strings.Join(baseConfig.Roles, ",") != strings.Join(forgedConfig.Roles, ",") {
		t.Fatalf("locale changed authenticated context: base=%+v forged=%+v", baseConfig, forgedConfig)
	}
	for _, suffix := range []string{"", "?locale=de-DE", "?locale=tenant-admin"} {
		denied := i18n005Request(t, h, "worker", []string{"worker_self"}, "/workspace/app/admin"+suffix)
		if denied.Code != http.StatusForbidden {
			t.Fatalf("worker admin access with locale %q = %d, want 403", suffix, denied.Code)
		}
	}
}

func TestTodo_I18N_005_Recovery(t *testing.T) {
	h := newI18N005Handler(t, nil)
	h.preferences = i18n005Preferences{loadErr: errors.New("preferences unavailable")}
	localized := i18n005Request(t, h, "user-with-unavailable-preferences", []string{"comp_admin"}, PathProductHome+"?locale=ar")
	if localized.Code != http.StatusServiceUnavailable {
		t.Fatalf("preference store failure = %d: %s", localized.Code, localized.Body.String())
	}
	for _, want := range []string{`<html lang="ar" dir="rtl" data-hcm-locale="ar" data-hcm-catalog="product-ui.v1"`, `<title>الصفحة غير متاحة</title>`, "تعذر تحميل أحدث المعلومات", "عُد إلى الصفحة الرئيسية"} {
		if !strings.Contains(localized.Body.String(), want) {
			t.Errorf("localized SSR recovery page missing %q", want)
		}
	}

	defaulted := i18n005Request(t, h, "user-with-unavailable-preferences", []string{"comp_admin"}, PathProductHome)
	if defaulted.Code != http.StatusServiceUnavailable || !strings.Contains(defaulted.Body.String(), `<html lang="en-US" dir="ltr" data-hcm-locale="en-US"`) || !strings.Contains(defaulted.Body.String(), "load the latest information") {
		t.Fatalf("when the authenticated preference source is unavailable and URL has no supported locale, SSR did not use the documented English fallback: status=%d; expected language or recovery copy missing", defaulted.Code)
	}
}
