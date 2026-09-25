package workspace

import (
	"encoding/base64"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

// This file is the dev sign-in page's company selector: when one process
// serves several demo companies, the page offers each company, and the quick
// picks and the employee directory below it are that company's.
//
// The choice is a plain GET link ("?company=ironridge-demo"), so it needs no
// script under the page's no-script CSP, is linkable, and survives the back
// button. The last choice is remembered in a cookie, which is how signing out
// returns to the login page with the same company preselected. The cookie is
// a display preference, not authority: the tenant a session belongs to is the
// one the selected persona's server-held credential was minted for.

// paramLoginCompany names the company selector's query parameter.
const paramLoginCompany = "company"

// loginCompanyCookie remembers the last company chosen on this browser.
const loginCompanyCookie = "hcmnext_company"

// devCompanies lists the companies the sign-in page offers, or nil when the
// composition serves one company (or none) and the page renders exactly as
// it always has.
func (h *Handler) devCompanies() []DevCompany {
	lister, ok := h.directory.(DevCompanyDirectory)
	if !ok {
		return nil
	}
	companies := lister.DevCompanies()
	if len(companies) < 2 {
		return nil
	}
	return companies
}

// selectedCompany resolves the company the page renders: the query's, else
// the remembered cookie's, else the first (default) company. An unknown key
// resolves to the default rather than to an error: the selector is a view
// preference and a stale link must still render a usable page.
func selectedCompany(companies []DevCompany, requested string) DevCompany {
	requested = strings.TrimSpace(requested)
	for _, company := range companies {
		if company.Key == requested {
			return company
		}
	}
	if len(companies) == 0 {
		return DevCompany{}
	}
	return companies[0]
}

// requestedCompany is the company key a GET of the sign-in page asks for.
func requestedCompany(r *http.Request) string {
	if value := strings.TrimSpace(r.URL.Query().Get(paramLoginCompany)); value != "" {
		return value
	}
	if cookie, err := r.Cookie(loginCompanyCookie); err == nil {
		return strings.TrimSpace(cookie.Value)
	}
	return ""
}

// rememberCompany records the chosen company for the next visit.
func (h *Handler) rememberCompany(w http.ResponseWriter, company string) {
	company = strings.TrimSpace(company)
	if company == "" || !validCompanyKey(company) {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: loginCompanyCookie, Value: company, Path: RoutePrefix, HttpOnly: true,
		Secure: h.secure, SameSite: http.SameSiteLaxMode, Expires: h.now().Add(180 * 24 * time.Hour),
	})
}

// validCompanyKey admits a tenant-key-shaped value only, so a cookie can
// never carry markup or separators into a header.
func validCompanyKey(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// companyPersona finds the persona filling one quick-pick slot for a
// company. A persona composed without a company belongs to the default
// company, which is how a single-company composition is read.
func (h *Handler) companyPersona(company DevCompany, defaultCompany, slot string) (DevPersona, bool) {
	for _, persona := range h.devPersonas {
		owner := persona.Company
		if owner == "" {
			owner = defaultCompany
		}
		personaSlot := persona.Slot
		if personaSlot == "" {
			personaSlot = persona.ID
		}
		if owner == company.Key && personaSlot == slot {
			return persona, true
		}
	}
	return DevPersona{}, false
}

// companyLogoDataURI inlines a company's embedded logo, because the sign-in
// page runs before any credential exists and the asset route admits only a
// signed-in request.
func companyLogoDataURI(logo string) string {
	if _, ok := FrontendAssetContentType(logo); !ok || !strings.HasSuffix(logo, ".svg") {
		return ""
	}
	body, ok := embeddedAsset(logo)
	if !ok {
		return ""
	}
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(body)
}

// loginCompanyText is the selector's own copy in the three shipped locales.
// The rest of the dev sign-in page is English-only; these strings are new
// and ship in every locale the product does.
type loginCompanyText struct {
	heading, people, current, choose string
}

func loginCompanyCopy(locale string) loginCompanyText {
	switch strings.ToLower(strings.TrimSpace(locale)) {
	case "de", "de-de":
		return loginCompanyText{heading: "Unternehmen", people: "Mitarbeitende", current: "Ausgewählt", choose: "Wechseln zu"}
	case "ar", "ar-sa", "ar-eg":
		return loginCompanyText{heading: "الشركة", people: "موظفًا", current: "محددة", choose: "التبديل إلى"}
	default:
		return loginCompanyText{heading: "Company", people: "people", current: "Selected", choose: "Switch to"}
	}
}

// companySelector renders the company choice as a group of links, the
// selected one marked aria-current.
func companySelector(companies []DevCompany, selected DevCompany, locale string) string {
	if len(companies) < 2 {
		return ""
	}
	copy := loginCompanyCopy(locale)
	var out strings.Builder
	out.WriteString(`<nav class="company-picker" aria-labelledby="company-heading">`)
	out.WriteString(`<h2 id="company-heading">` + html.EscapeString(copy.heading) + `</h2><ul>`)
	for _, company := range companies {
		href := PathLogin + "?" + paramLoginCompany + "=" + url.QueryEscape(company.Key)
		if locale != "" {
			href += "&locale=" + url.QueryEscape(locale)
		}
		current := company.Key == selected.Key
		out.WriteString(`<li><a class="company-option" href="` + html.EscapeString(href) + `" data-company="` + html.EscapeString(company.Key) + `"`)
		if current {
			out.WriteString(` aria-current="true"`)
		} else {
			out.WriteString(` aria-label="` + html.EscapeString(copy.choose+" "+company.Name) + `"`)
		}
		out.WriteString(`>`)
		if logo := companyLogoDataURI(company.Logo); logo != "" {
			out.WriteString(`<img class="company-logo" src="` + logo + `" alt="" width="144" height="32">`)
		}
		out.WriteString(`<span class="company-name">` + html.EscapeString(company.Name) + `</span>`)
		out.WriteString(`<span class="company-description">` + html.EscapeString(company.Description) + `</span>`)
		out.WriteString(`<span class="company-headcount">` + html.EscapeString(strconv.Itoa(company.Headcount)+" "+copy.people))
		if current {
			out.WriteString(` · ` + html.EscapeString(copy.current))
		}
		out.WriteString(`</span></a></li>`)
	}
	out.WriteString(`</ul></nav>`)
	return out.String()
}

// companyBrand renders the login card's brand line for the selected company.
func companyBrand(company DevCompany) string {
	if logo := companyLogoDataURI(company.Logo); logo != "" {
		return `<div class="login-brand"><img class="login-logo" src="` + logo + `" alt="` + html.EscapeString(company.Name) + `" width="180" height="40"></div>`
	}
	mark := "?"
	if name := strings.TrimSpace(company.ShortName); name != "" {
		mark = strings.ToUpper(name[:1])
	}
	return `<div class="login-brand"><span class="login-mark" aria-hidden="true">` + html.EscapeString(mark) + `</span><strong>` + html.EscapeString(company.ShortName) + `</strong></div>`
}

// companyForTenant is the served company a signed-in tenant belongs to, when
// the composition offers several.
func (h *Handler) companyForTenant(tenant string) (DevCompany, bool) {
	for _, company := range h.devCompanies() {
		if company.Key == tenant {
			return company, true
		}
	}
	return DevCompany{}, false
}

// applyCompanyBrand gives a multi-company workspace's shell the signed-in
// company's own name and logo. A single-company composition is untouched,
// which keeps its header exactly as it always rendered.
func (h *Handler) applyCompanyBrand(config *JourneyConfig) {
	company, ok := h.companyForTenant(config.Tenant)
	if !ok {
		return
	}
	config.TenantName = company.Name
	if _, known := FrontendAssetContentType(company.Logo); known {
		config.TenantLogo = PathAssetPrefix + company.Logo
	}
}

// loginCompanyStylesheet is the company selector's CSS. It is appended to
// the sign-in stylesheet only on the multi-company page, so a
// single-company page's stylesheet, and the CSP hash that pins it, are
// unchanged.
func loginCompanyStylesheet() string {
	return buildTypedSheet(declareLoginCompanyStyles)
}

func declareLoginCompanyStyles() {
	declareGlobal(".company-picker",
		gwccss.Raw("margin", "0 0 1.5rem"),
	)
	declareGlobal(".company-picker h2",
		gwccss.Raw("margin", "0 0 .6rem"),
		gwccss.FontSize(gwccss.Rem(.8)),
		gwccss.Raw("font-weight", "800"),
		gwccss.Tracking(gwccss.Ems(.06)),
		gwccss.Raw("text-transform", "uppercase"),
		gwccss.TextColor(gwccss.Hex("53645b")),
	)
	declareGlobal(".company-picker ul",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Repeat(2, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
		gwccss.Gap(gwccss.Rem(.75)),
		gwccss.Raw("margin", "0"),
		gwccss.Padding(gwccss.Zero),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".company-option",
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.Rem(.35)),
		gwccss.Raw("height", "100%"),
		gwccss.Raw("box-sizing", "border-box"),
		gwccss.Padding(gwccss.Rem(1)),
		gwccss.Border(gwccss.Px(1), gwccss.Hex("dbe5df")),
		gwccss.Rounded(gwccss.Rem(.875)),
		gwccss.Bg(gwccss.Hex("fbfcfb")),
		gwccss.TextColor(gwccss.Hex("17231d")),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".company-option:hover, .company-option:focus-visible",
		gwccss.Raw("border-color", "#147a4a"),
		gwccss.Raw("outline", "none"),
		gwccss.Raw("box-shadow", "0 0 0 3px rgba(20,122,74,.18)"),
	)
	declareGlobal(".company-option[aria-current=\"true\"]",
		gwccss.Raw("border", "2px solid #147a4a"),
		gwccss.Bg(gwccss.Hex("f1f8f4")),
	)
	declareGlobal(".company-logo",
		gwccss.Raw("height", "2rem"),
		gwccss.Raw("width", "auto"),
		gwccss.Raw("max-width", "100%"),
		gwccss.Raw("align-self", "flex-start"),
	)
	declareGlobal(".company-name",
		gwccss.Raw("font-weight", "800"),
		gwccss.FontSize(gwccss.Rem(1.02)),
	)
	declareGlobal(".company-description",
		gwccss.TextColor(gwccss.Hex("53645b")),
		gwccss.FontSize(gwccss.Rem(.88)),
		gwccss.Raw("line-height", "1.4"),
	)
	declareGlobal(".company-headcount",
		gwccss.Raw("margin-top", "auto"),
		gwccss.TextColor(gwccss.Hex("147a4a")),
		gwccss.FontSize(gwccss.Rem(.8)),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".login-logo",
		gwccss.Raw("height", "2.5rem"),
		gwccss.Raw("width", "auto"),
	)
	declareGlobal(".company-picker ul",
		mediaRule(gwccss.RawMedia("(max-width:42.5rem)"), gwccss.GridCols(gwccss.Fr(1))),
	)
}
