package workspace

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestTodo_UXBLIND_045(t *testing.T) {
	h := newCompanyLoginHandler(t)
	recorder, body := getCompanyLoginPage(t, h, PathLogin+"?company=ironridge-demo")
	for _, want := range []string{
		`data-hcm-company="ironridge-demo"`,
		`<img class="login-logo"`,
		`alt="Ironridge Builders"`,
		`data-company="ironridge-demo" aria-current="true"`,
		`body[data-hcm-company="ironridge-demo"]`,
		"c2410c",
		"prefers-color-scheme:dark",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Ironridge sign-in page missing %q", want)
		}
	}
	if cookie := recorder.Result().Cookies(); len(cookie) == 0 {
		t.Fatal("selected company did not produce a remembered-company cookie")
	}

	var remembered *http.Cookie
	for _, candidate := range recorder.Result().Cookies() {
		if candidate.Name == loginCompanyCookie {
			remembered = candidate
		}
	}
	if remembered == nil || remembered.Value != "ironridge-demo" {
		t.Fatalf("remembered company = %+v", remembered)
	}
	_, rememberedPage := getCompanyLoginPage(t, h, PathLogin, remembered)
	if !strings.Contains(rememberedPage, `data-hcm-company="ironridge-demo"`) {
		t.Fatal("sign-in did not retain the last selected company")
	}
}

func TestTodo_UXBLIND_045_Browser(t *testing.T) {
	// The browser journey is represented by the server-rendered document here:
	// this lane cannot start the shared dev server or rebuild the WASM asset.
	h := newCompanyLoginHandler(t)
	_, body := getCompanyLoginPage(t, h, PathLogin+"?company=ironridge-demo")
	brandStart := strings.Index(body, `<div class="login-brand">`)
	brandEnd := strings.Index(body[brandStart:], `</div>`)
	if brandStart < 0 || brandEnd < 0 {
		t.Fatal("rendered sign-in page has no bounded brand block")
	}
	brand := body[brandStart : brandStart+brandEnd]
	if !strings.Contains(brand, `alt="Ironridge Builders"`) || strings.Contains(brand, "HarborCare") {
		t.Fatalf("selected-company brand block = %q", brand)
	}
}

func TestTodo_UXBLIND_045_Accessibility(t *testing.T) {
	h := newCompanyLoginHandler(t)
	_, body := getCompanyLoginPage(t, h, PathLogin+"?company=ironridge-demo&locale=de-DE")
	for _, want := range []string{
		`aria-current="true"`,
		`alt="Ironridge Builders"`,
		`width="180"`,
		`height="40"`,
		`width="144"`,
		`height="32"`,
		"1f2328",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("accessible branded sign-in page missing %q", want)
		}
	}
}

func TestTodo_UXBLIND_046(t *testing.T) {
	h := newCompanyLoginHandler(t)
	config := JourneyConfig{Tenant: "ironridge-demo"}
	h.applyCompanyBrand(&config)
	theme := productui.DefaultCustomerTheme()
	theme.BrandName, theme.BrandMark, theme.BrandLogoURL = config.TenantName, config.TenantMark, config.TenantLogo
	doc, err := productShellDocumentForRouteStateWithTheme(config, true, productui.ResolveProductLocale("en-US"), productui.PageHome, "", "", theme, productStylesheet())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"tenant_name":"Ironridge Builders"`,
		`"tenant_mark":"IB"`,
		`"tenant_logo":"/workspace/assets/ironridge-logo.svg"`,
		`height="40"`,
		`width="180"`,
		`src="/workspace/assets/ironridge-logo.svg"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("initial branded shell missing %q", want)
		}
	}
	if strings.Contains(doc, `class="tenant"`) {
		t.Fatal("initial branded shell still has a second, shifting tenant label")
	}
}

func TestTodo_UXBLIND_046_Browser(t *testing.T) {
	h := newCompanyLoginHandler(t)
	config := JourneyConfig{Tenant: "ironridge-demo"}
	h.applyCompanyBrand(&config)
	theme := productui.DefaultCustomerTheme()
	theme.BrandName, theme.BrandMark, theme.BrandLogoURL = config.TenantName, config.TenantMark, config.TenantLogo
	doc, err := productShellDocumentForRouteStateWithTheme(config, true, productui.ResolveProductLocale("en-US"), productui.PagePeople, "", "", theme, productStylesheet())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `data-hcm-brand-logo-slot`) || !strings.Contains(doc, `height="40"`) || !strings.Contains(doc, `width="180"`) {
		t.Fatal("browser-facing loading shell does not reserve the brand slot")
	}
}

func TestTodo_UXBLIND_046_Regression(t *testing.T) {
	if got := brandMarkForName("Ironridge Builders"); got != "IB" {
		t.Fatalf("tenant mark = %q, want IB", got)
	}
	if got := loginReturnTarget("https://evil.invalid" + PathProductHome); got != "" {
		t.Fatalf("external return target accepted: %q", got)
	}
}

func TestTodo_UXBLIND_050(t *testing.T) {
	h, _ := newShellHandler(t, true)
	roles := []string{"hcm_admin"}
	for _, persona := range []struct{ id, subject string }{
		{"darius", "uxblind-050-darius"},
		{"linh", "uxblind-050-linh"},
		{"thomas", "uxblind-050-thomas"},
		{"walt", "uxblind-050-walt"},
	} {
		h.devPersonas[persona.id] = DevPersona{ID: persona.id, Name: persona.id, WorkerRef: persona.subject, Roles: roles, Token: frontendE2EToken(t, persona.subject, roles)}
		form := url.Values{paramLoginPersona: {persona.id}}
		request := httptest.NewRequest(http.MethodPost, PathLogin, strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != PathProductHome {
			t.Errorf("%s landing = %d %q, want %q", persona.id, recorder.Code, recorder.Header().Get("Location"), PathProductHome)
		}
	}

	logout := httptest.NewRecorder()
	h.ServeHTTP(logout, httptest.NewRequest(http.MethodGet, PathLogout, nil))
	if logout.Code != http.StatusSeeOther || logout.Header().Get("Location") != PathLogin {
		t.Fatalf("logout redirect = %d %q, want bare login", logout.Code, logout.Header().Get("Location"))
	}
}

func TestTodo_UXBLIND_050_Browser(t *testing.T) {
	h, _ := newShellHandler(t, true)
	roles := []string{"hcm_admin"}
	subject := "uxblind-050-deep-link"
	h.devPersonas["deep"] = DevPersona{ID: "deep", Name: "Deep link", WorkerRef: subject, Roles: roles, Token: frontendE2EToken(t, subject, roles)}
	returnTo := PathProductPrefix + "people"
	initial := httptest.NewRecorder()
	h.ServeHTTP(initial, httptest.NewRequest(http.MethodGet, returnTo, nil))
	if initial.Code != http.StatusSeeOther || !strings.Contains(initial.Header().Get("Location"), paramLoginReturnTo+"=") {
		t.Fatalf("unauthenticated deep link = %d %q", initial.Code, initial.Header().Get("Location"))
	}
	loginURL, err := url.Parse(initial.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	loginPage := httptest.NewRecorder()
	h.ServeHTTP(loginPage, httptest.NewRequest(http.MethodGet, loginURL.String(), nil))
	if loginPage.Code != http.StatusOK || !strings.Contains(loginPage.Body.String(), `name="`+paramLoginReturnTo+`" value="`+returnTo+`"`) {
		t.Fatal("deep-link return target was not carried into the sign-in form")
	}
	form := url.Values{paramLoginPersona: {"deep"}, paramLoginReturnTo: {returnTo}}
	request := httptest.NewRequest(http.MethodPost, PathLogin, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != returnTo {
		t.Fatalf("authorized deep-link landing = %d %q, want %q", recorder.Code, recorder.Header().Get("Location"), returnTo)
	}
}
