package workspace

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// stubCompanyDirectory is a two-company directory: the multi-company port
// the composition root supplies when one process serves both demo tenants.
type stubCompanyDirectory struct {
	companies []DevCompany
	snapshots map[string]DevDirectorySnapshot
}

func (stub stubCompanyDirectory) DevDirectorySnapshot() DevDirectorySnapshot {
	return stub.snapshots[stub.companies[0].Key]
}

func (stub stubCompanyDirectory) DevCompanies() []DevCompany { return stub.companies }

func (stub stubCompanyDirectory) DevCompanyDirectorySnapshot(company string) (DevDirectorySnapshot, bool) {
	snapshot, ok := stub.snapshots[company]
	return snapshot, ok
}

func newCompanyLoginHandler(t *testing.T) *Handler {
	t.Helper()
	h, _ := newShellHandler(t, true)
	h.directory = stubCompanyDirectory{
		companies: []DevCompany{
			{Key: "harborcare-demo", Name: "HarborCare Health Services", ShortName: "HarborCare", Description: "Community care", Headcount: 60, Logo: "harborcare-logo.svg"},
			{Key: "ironridge-demo", Name: "Ironridge Builders", ShortName: "Ironridge Builders", Description: "Commercial general contractor", Headcount: 38, Logo: "ironridge-logo.svg"},
		},
		snapshots: map[string]DevDirectorySnapshot{
			"harborcare-demo": {
				Units:     []DevDirectoryUnit{{Code: "harborcare", Name: "HarborCare Health Services"}},
				Employees: []DevDirectoryEmployee{{PersonaID: DevEmployeePersonaID("hc-001-amina-rahman"), WorkerKey: "hc-001-amina-rahman", Name: "Amina Rahman", WorkerNumber: "HC-21001", JobTitle: "Chief Executive Officer", UnitCode: "harborcare"}},
			},
			"ironridge-demo": {
				Units:     []DevDirectoryUnit{{Code: "ironridge", Name: "Ironridge Builders"}},
				Employees: []DevDirectoryEmployee{{PersonaID: DevEmployeePersonaID("ir-001-walt-brennan"), WorkerKey: "ir-001-walt-brennan", Name: "Walt Brennan", WorkerNumber: "IR-00001", JobTitle: "Owner & President", UnitCode: "ironridge"}},
			},
		},
	}
	roles, _ := DevPersonaRoles("admin")
	h.devPersonas = map[string]DevPersona{
		"admin": {ID: "admin", Name: "Rafael Torres", Access: "HCM administrator", Roles: roles, Company: "harborcare-demo", Slot: "admin",
			WorkerRef: "hc-050-rafael-torres", Token: frontendE2EToken(t, "hc-050-rafael-torres", roles)},
		"ironridge-demo:admin": {ID: "ironridge-demo:admin", Name: "Walt Brennan", Access: "HCM administrator", Roles: roles, Company: "ironridge-demo", Slot: "admin",
			WorkerRef: "ir-001-walt-brennan", Token: frontendE2EToken(t, "ir-001-walt-brennan", roles)},
	}
	for _, key := range []string{"hc-001-amina-rahman", "ir-001-walt-brennan"} {
		id := DevEmployeePersonaID(key)
		h.devPersonas[id] = DevPersona{ID: id, Name: key, Roles: roles, WorkerRef: key, Token: frontendE2EToken(t, key, roles)}
	}
	return h
}

func getCompanyLoginPage(t *testing.T, h *Handler, target string, cookies ...*http.Cookie) (*httptest.ResponseRecorder, string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", target, recorder.Code)
	}
	return recorder, recorder.Body.String()
}

func TestLoginCompanySelectorSwitchesPersonasAndDirectory(t *testing.T) {
	h := newCompanyLoginHandler(t)

	_, harbor := getCompanyLoginPage(t, h, PathLogin)
	for _, want := range []string{`class="company-picker"`, `data-company="harborcare-demo" aria-current="true"`, "Ironridge Builders", "38 people", `value="admin"`, "Amina Rahman"} {
		if !strings.Contains(harbor, want) {
			t.Errorf("default company page lacks %q", want)
		}
	}
	if strings.Contains(harbor, `value="ironridge-demo:admin"`) || strings.Contains(harbor, "Sign in as Walt Brennan") {
		t.Error("the HarborCare page offers an Ironridge credential")
	}

	recorder, iron := getCompanyLoginPage(t, h, PathLogin+"?company=ironridge-demo")
	for _, want := range []string{`data-company="ironridge-demo" aria-current="true"`, `value="ironridge-demo:admin"`, "Continue as Walt Brennan", "Sign in as Walt Brennan", `name="company" value="ironridge-demo"`} {
		if !strings.Contains(iron, want) {
			t.Errorf("Ironridge page lacks %q", want)
		}
	}
	if strings.Contains(iron, `value="admin"`) || strings.Contains(iron, "Amina Rahman") {
		t.Error("the Ironridge page offers a HarborCare credential")
	}
	if csp := recorder.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "img-src data:") {
		t.Errorf("multi-company login CSP = %q, want inline logos admitted", csp)
	}
	var remembered *http.Cookie
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == loginCompanyCookie {
			remembered = cookie
		}
	}
	if remembered == nil || remembered.Value != "ironridge-demo" || !remembered.HttpOnly {
		t.Fatalf("company choice not remembered: %+v", remembered)
	}

	// Coming back without the parameter (signing out redirects to the bare
	// login path) preselects the remembered company.
	_, again := getCompanyLoginPage(t, h, PathLogin, remembered)
	if !strings.Contains(again, `data-company="ironridge-demo" aria-current="true"`) {
		t.Error("the remembered company is not preselected")
	}
	// An unknown company falls back to the default rather than failing.
	_, unknown := getCompanyLoginPage(t, h, PathLogin+"?company=nope")
	if !strings.Contains(unknown, `data-company="harborcare-demo" aria-current="true"`) {
		t.Error("an unknown company did not fall back to the default")
	}
}

func TestLoginCompanyPersonaSignInRemembersItsCompany(t *testing.T) {
	h := newCompanyLoginHandler(t)
	request := httptest.NewRequest(http.MethodPost, PathLogin, strings.NewReader(url.Values{paramLoginPersona: {"ironridge-demo:admin"}}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("persona sign-in = %d: %s", recorder.Code, recorder.Body.String())
	}
	found := false
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == loginCompanyCookie && cookie.Value == "ironridge-demo" {
			found = true
		}
	}
	if !found {
		t.Fatal("signing in did not remember the persona's company")
	}
}

func TestLoginSingleCompanyPageIsUnchanged(t *testing.T) {
	h := newDirectoryHandler(t)
	page := getLoginPage(t, h, "")
	if strings.Contains(page, "company-picker") || !strings.Contains(page, `<strong>HarborCare</strong>`) {
		t.Fatal("a single-company composition renders the company selector")
	}
}

func TestCompanyBrandAppliesOnlyToServedCompanies(t *testing.T) {
	h := newCompanyLoginHandler(t)
	config := JourneyConfig{Tenant: "ironridge-demo"}
	h.applyCompanyBrand(&config)
	if config.TenantName != "Ironridge Builders" || config.TenantLogo != PathAssetPrefix+"ironridge-logo.svg" {
		t.Fatalf("Ironridge brand = %q %q", config.TenantName, config.TenantLogo)
	}
	foreign := JourneyConfig{Tenant: "another-tenant"}
	h.applyCompanyBrand(&foreign)
	if foreign.TenantName != "" || foreign.TenantLogo != "" {
		t.Fatalf("a foreign tenant acquired a brand: %+v", foreign)
	}
	single, _ := newShellHandler(t, true)
	plain := JourneyConfig{Tenant: "ironridge-demo"}
	single.applyCompanyBrand(&plain)
	if plain.TenantName != "" {
		t.Fatal("a single-company composition rebranded its shell")
	}
	if !validCompanyKey("ironridge-demo") || validCompanyKey(`x"><script>`) || validCompanyKey("") {
		t.Fatal("company key validation admits unsafe values")
	}
	if copy := loginCompanyCopy("de"); copy.heading != "Unternehmen" {
		t.Fatalf("German selector copy = %+v", copy)
	}
	if copy := loginCompanyCopy("ar"); copy.heading != "الشركة" {
		t.Fatalf("Arabic selector copy = %+v", copy)
	}
}
