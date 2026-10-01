package workspace

import (
	"strings"
	"sync"

	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

// typedSheetMu serializes typed-stylesheet builds. The css package emits into
// a process-wide, content-deduped sink, so each stylesheet is built as one
// atomic Reset -> declare -> Harvest sequence; without the mutex, another
// package's declarations could land in this sheet depending on call order.
var typedSheetMu sync.Mutex

// buildTypedSheet runs declare and returns exactly the CSS it emitted, in
// declaration order. Declarations must be deterministic: same calls, same
// bytes, so the CSP hash over the final stylesheet is stable.
func buildTypedSheet(declare func()) string {
	typedSheetMu.Lock()
	defer typedSheetMu.Unlock()
	gwccss.Reset()
	declare()
	return gwccss.Harvest()
}

// declareGlobal emits one literal selector. Variant helpers (Hover, Media,
// …) return rule slices, so every call funnels through Rules, which accepts
// both single rules and slices.
func declareGlobal(selector string, parts ...any) {
	gwccss.Global(selector, gwccss.Rules(parts...)...)
}

// mediaRule scopes parts inside one @media query. The single-spread form
// keeps every call site clear of fixed-arg-plus-spread mixing.
func mediaRule(query gwccss.MediaQuery, parts ...any) []gwccss.Rule {
	return gwccss.Media(query, gwccss.Rules(parts...)...)
}

// loginSpecificStylesheet is the login page's own CSS, kept separate from
// tokens.WorkspaceCSS (owned by internal/experience/tokens) so this package converts
// only its own literal. loginStylesheet concatenates the two.
func loginSpecificStylesheet() string {
	return buildTypedSheet(declareLoginStyles)
}

func declareLoginStyles() {
	declareGlobal("body",
		gwccss.Bg(gwccss.Hex("f4f7f5")),
		gwccss.TextColor(gwccss.Hex("17231d")),
	)
	declareGlobal(".login-shell",
		gwccss.Display.Block,
		gwccss.W(gwccss.RawLength("min(60rem,calc(100% - 2rem))")),
		gwccss.MarginY(gwccss.Vh(7)), gwccss.MarginX(gwccss.RawLength("auto")),
		gwccss.Padding(gwccss.Zero),
	)
	declareGlobal(".login-card",
		gwccss.Display.Block,
		gwccss.Bg(gwccss.Hex("fff")),
		gwccss.Border(gwccss.Px(1), gwccss.Hex("dbe5df")),
		gwccss.Rounded(gwccss.Rem(1.125)),
		gwccss.Raw("box-shadow", "0 1.125rem 3.5rem rgba(24,57,40,.10)"),
		gwccss.Padding(gwccss.RawLength("clamp(1.5rem,4vw,3rem)")),
	)
	declareGlobal(".login-brand",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Rem(.75)),
		gwccss.Raw("margin-bottom", "1.75rem"),
	)
	declareGlobal(".login-mark",
		gwccss.Display.Grid,
		gwccss.Raw("place-items", "center"),
		gwccss.W(gwccss.Rem(2.375)),
		gwccss.H(gwccss.Rem(2.375)),
		gwccss.Rounded(gwccss.Rem(.6875)),
		gwccss.Bg(gwccss.Hex("147a4a")),
		gwccss.TextColor(gwccss.Hex("fff")),
		gwccss.Raw("font-weight", "800"),
	)
	// The sign-in document has no authenticated preference record yet, so its
	// first paint follows the same device color preference as the workspace.
	declareGlobal("body",
		mediaRule(gwccss.RawMedia("(prefers-color-scheme:dark)"), gwccss.Bg(gwccss.Hex("111315")), gwccss.TextColor(gwccss.Hex("f2f0ec"))),
	)
	// The unauthenticated selector has no person preference to read, so it
	// follows the same device color preference as the default workspace and
	// selects the public company accent before a credential exists.
	declareGlobal(`body[data-hcm-company="ironridge-demo"]`,
		gwccss.Bg(gwccss.Hex("f4f4f2")), gwccss.TextColor(gwccss.Hex("1f2328")),
	)
	declareGlobal(`body[data-hcm-company="ironridge-demo"] .login-mark,body[data-hcm-company="ironridge-demo"] .persona button`,
		gwccss.Bg(gwccss.Hex("c2410c")),
	)
	declareGlobal(`body[data-hcm-company="ironridge-demo"] .persona button:hover`,
		gwccss.Bg(gwccss.Hex("9a3412")),
	)
	declareGlobal(`body[data-hcm-company="ironridge-demo"] .persona-access,body[data-hcm-company="ironridge-demo"] .company-headcount`,
		gwccss.TextColor(gwccss.Hex("9a3412")),
	)
	declareGlobal(`body[data-hcm-company="ironridge-demo"] .company-option:hover,body[data-hcm-company="ironridge-demo"] .company-option:focus-visible`,
		gwccss.Raw("border-color", "#c2410c"),
	)
	declareGlobal(`body[data-hcm-company="ironridge-demo"] .company-option[aria-current="true"]`,
		gwccss.Raw("border-color", "#c2410c"), gwccss.Bg(gwccss.Hex("fdeee5")),
	)
	declareGlobal(`body[data-hcm-company="ironridge-demo"] .login-card`,
		gwccss.Bg(gwccss.Hex("ffffff")),
	)
	declareGlobal(`body[data-hcm-company="ironridge-demo"]`,
		mediaRule(gwccss.RawMedia("(prefers-color-scheme:dark)"), gwccss.Bg(gwccss.Hex("111315")), gwccss.TextColor(gwccss.Hex("f2f0ec"))),
	)
	declareGlobal(`body[data-hcm-company="ironridge-demo"] .persona button`,
		mediaRule(gwccss.RawMedia("(prefers-color-scheme:dark)"), gwccss.Bg(gwccss.Hex("fb923c")), gwccss.TextColor(gwccss.Hex("1f2328"))),
	)
	declareGlobal(".login-card h1",
		gwccss.MarginY(gwccss.Rem(.15)), gwccss.MarginX(gwccss.Zero),
		gwccss.FontSize(gwccss.RawLength("clamp(1.8rem,4vw,2.5rem)")),
	)
	declareGlobal(".login-intro",
		gwccss.MaxWidth(gwccss.Ch(62)),
		gwccss.TextColor(gwccss.Hex("53645b")),
	)
	declareGlobal(".persona-grid",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Repeat(2, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
		gwccss.Gap(gwccss.Rem(.875)),
		gwccss.MarginY(gwccss.Rem(1.75)), gwccss.MarginX(gwccss.Zero),
	)
	declareGlobal(".persona",
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Raw("align-items", "flex-start"),
		gwccss.Gap(gwccss.Rem(.4375)),
		gwccss.Padding(gwccss.Rem(1.125)),
		gwccss.Border(gwccss.Px(1), gwccss.Hex("dbe5df")),
		gwccss.Rounded(gwccss.Rem(.875)),
		gwccss.Bg(gwccss.Hex("fbfcfb")),
	)
	declareGlobal(".persona strong",
		gwccss.FontSize(gwccss.Rem(1.05)),
	)
	declareGlobal(".persona span",
		gwccss.Display.Block,
	)
	declareGlobal(".persona-access",
		gwccss.TextColor(gwccss.Hex("147a4a")),
		gwccss.FontSize(gwccss.Rem(.78)),
		gwccss.Raw("font-weight", "800"),
		gwccss.Tracking(gwccss.Ems(.06)),
		gwccss.Raw("text-transform", "uppercase"),
	)
	declareGlobal(".persona button",
		gwccss.Raw("margin-top", "auto"),
		gwccss.W(gwccss.Percent(100)),
		gwccss.Bg(gwccss.Hex("147a4a")),
		gwccss.TextColor(gwccss.Hex("fff")),
	)
	declareGlobal(".persona button:hover",
		gwccss.Bg(gwccss.Hex("0e623a")),
	)
	declareGlobal(".advanced",
		gwccss.BorderTop(gwccss.Px(1), gwccss.Hex("e7eeea")),
		gwccss.Raw("padding-top", "1rem"),
		gwccss.TextColor(gwccss.Hex("53645b")),
	)
	declareGlobal(".advanced form",
		gwccss.Raw("margin-top", ".875rem"),
	)
	declareLoginDirectoryStyles()
	declareGlobal(".login-shell",
		mediaRule(gwccss.RawMedia("(max-width:42.5rem)"), gwccss.MarginY(gwccss.Rem(1)), gwccss.MarginX(gwccss.RawLength("auto"))),
	)
	declareGlobal(".persona-grid",
		mediaRule(gwccss.RawMedia("(max-width:42.5rem)"), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".login-card",
		mediaRule(gwccss.RawMedia("(max-width:42.5rem)"), gwccss.Padding(gwccss.Rem(1.375))),
	)
	declareLoginDarkStyles(false)
}

// darkScoped returns each selector bare and, for the multi-company page,
// again under the company body attribute, so a dark rule outranks the company
// accent rules (`body[data-hcm-company=...] .x`) that would otherwise keep the
// light surface in dark mode.
func darkScoped(company bool, selectors ...string) string {
	out := make([]string, 0, len(selectors)*2)
	for _, selector := range selectors {
		out = append(out, selector)
		if company {
			out = append(out, "body[data-hcm-company] "+selector)
		}
	}
	return strings.Join(out, ",")
}

// declareLoginDarkStyles is every dark-mode surface and text colour of the
// sign-in page. It must be the LAST thing emitted in every stylesheet that
// declares a card: the light base rules (.persona, .company-option, the
// selected card, the directory entry) share or exceed the specificity of a
// bare dark selector, so a dark rule declared before them loses the cascade
// and leaves a light surface under light text. loginSpecificStylesheet ends
// with the shared rules; loginCompanyStylesheet ends with the company ones
// (company is true), which keeps the single-company stylesheet free of
// company selectors.
func declareLoginDarkStyles(company bool) {
	dark := gwccss.RawMedia("(prefers-color-scheme:dark)")
	// The workspace token sheet paints every bare <section> with a light
	// surface, which would sit under the directory's light-on-dark text.
	declareGlobal(".directory",
		mediaRule(dark, gwccss.Raw("background-color", "transparent")),
	)
	if !company {
		declareGlobal(darkScoped(false, ".login-card", ".persona", ".directory-entry"),
			mediaRule(dark, gwccss.Bg(gwccss.Hex("1b1e22")), gwccss.BorderColor(gwccss.Hex("3d4249")), gwccss.TextColor(gwccss.Hex("f2f0ec"))),
		)
		declareGlobal(darkScoped(false, ".persona strong", ".directory-name", ".directory-roles"),
			mediaRule(dark, gwccss.TextColor(gwccss.Hex("f2f0ec"))),
		)
		declareGlobal(darkScoped(false, ".login-intro", ".persona span", ".directory-ident", ".directory-meta", ".directory-summary", ".org-count", ".advanced"),
			mediaRule(dark, gwccss.TextColor(gwccss.Hex("b5b1aa"))),
		)
		declareGlobal(darkScoped(false, ".persona-access", ".directory-clear"),
			mediaRule(dark, gwccss.TextColor(gwccss.Hex("4ade80"))),
		)
		declareGlobal(".directory-search input,.directory-search select",
			mediaRule(dark, gwccss.Bg(gwccss.Hex("111315")), gwccss.TextColor(gwccss.Hex("f2f0ec")), gwccss.BorderColor(gwccss.Hex("3d4249"))),
		)
		return
	}
	declareGlobal(darkScoped(true, ".login-card", ".persona", ".company-option", ".directory-entry"),
		mediaRule(dark, gwccss.Bg(gwccss.Hex("1b1e22")), gwccss.BorderColor(gwccss.Hex("3d4249")), gwccss.TextColor(gwccss.Hex("f2f0ec"))),
	)
	declareGlobal(darkScoped(true, `.company-option[aria-current="true"]`),
		mediaRule(dark, gwccss.Bg(gwccss.Hex("1d2b23")), gwccss.Raw("border-color", "#4ade80")),
	)
	declareGlobal(`body[data-hcm-company="ironridge-demo"] .company-option[aria-current="true"]`,
		mediaRule(dark, gwccss.Bg(gwccss.Hex("2b2023")), gwccss.Raw("border-color", "#fb923c")),
	)
	declareGlobal(darkScoped(true, ".company-option .company-name", ".persona strong", ".directory-name", ".directory-roles"),
		mediaRule(dark, gwccss.TextColor(gwccss.Hex("f2f0ec"))),
	)
	declareGlobal(darkScoped(true, ".login-intro", ".company-picker h2", ".company-option .company-description", ".persona span", ".directory-ident", ".directory-meta", ".directory-summary", ".org-count", ".advanced"),
		mediaRule(dark, gwccss.TextColor(gwccss.Hex("b5b1aa"))),
	)
	declareGlobal(darkScoped(true, ".persona-access", ".company-headcount", ".directory-clear"),
		mediaRule(dark, gwccss.TextColor(gwccss.Hex("4ade80"))),
	)
	declareGlobal(`body[data-hcm-company="ironridge-demo"] .persona-access,body[data-hcm-company="ironridge-demo"] .company-headcount`,
		mediaRule(dark, gwccss.TextColor(gwccss.Hex("fb923c"))),
	)
	declareGlobal(".directory-search input,.directory-search select",
		mediaRule(dark, gwccss.Bg(gwccss.Hex("111315")), gwccss.TextColor(gwccss.Hex("f2f0ec")), gwccss.BorderColor(gwccss.Hex("3d4249"))),
	)
}

// declareLoginDirectoryStyles is the employee search and organization tree's
// own CSS (login_directory.go). It is declared from declareLoginStyles, in
// order, so the single CSP style hash still covers the whole sign-in
// stylesheet - the page is served under one style hash and no script source,
// so every visual affordance here has to be reachable from CSS alone.
//
// The tree's indentation shrinks at 42.5rem and again at 30rem, because at
// 390px a four-level indent leaves a job title nothing to wrap into. Nothing
// depends on hover: a keyboard focus ring is the same affordance, so
// :focus-visible is styled next to :hover everywhere below.
func declareLoginDirectoryStyles() {
	declareGlobal(".directory",
		gwccss.BorderTop(gwccss.Px(1), gwccss.Hex("e7eeea")),
		gwccss.Raw("padding-top", "1.25rem"),
		gwccss.Raw("margin-bottom", "1.5rem"),
	)
	declareGlobal(".directory h2",
		gwccss.Raw("margin", "0 0 .35rem"),
		gwccss.FontSize(gwccss.Rem(1.25)),
	)
	declareGlobal(".directory-search",
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Rem(.5)),
		gwccss.Raw("margin", "1rem 0 .5rem"),
	)
	declareGlobal(".directory-search label",
		gwccss.Raw("flex", "0 0 100%"),
		gwccss.Raw("font-weight", "700"),
		gwccss.FontSize(gwccss.Rem(.85)),
	)
	declareGlobal(".directory-search input",
		gwccss.Raw("flex", "1 1 16rem"),
		gwccss.Raw("min-width", "0"),
		gwccss.Padding(gwccss.Rem(.55)),
		gwccss.Border(gwccss.Px(1), gwccss.Hex("c7d6ce")),
		gwccss.Rounded(gwccss.Rem(.5)),
		gwccss.FontSize(gwccss.Rem(1)),
	)
	declareGlobal(".directory-search select",
		gwccss.Raw("flex", "0 1 12rem"),
		gwccss.Raw("min-width", "0"),
		gwccss.Padding(gwccss.Rem(.55)),
		gwccss.Border(gwccss.Px(1), gwccss.Hex("c7d6ce")),
		gwccss.Rounded(gwccss.Rem(.5)),
		gwccss.Bg(gwccss.Hex("fff")),
		gwccss.FontSize(gwccss.Rem(1)),
	)
	// The role control's own label sits beside it rather than on the form's
	// full-width first line, which belongs to the search field.
	declareGlobal(".directory-search label[for=\"directory-role\"]",
		gwccss.Raw("flex", "0 0 auto"),
	)
	declareGlobal(".directory-search button",
		gwccss.Bg(gwccss.Hex("147a4a")),
		gwccss.TextColor(gwccss.Hex("fff")),
	)
	declareGlobal(".directory-clear",
		gwccss.TextColor(gwccss.Hex("0e623a")),
	)
	declareGlobal(".directory-summary",
		gwccss.TextColor(gwccss.Hex("53645b")),
		gwccss.Raw("margin", ".25rem 0 1rem"),
	)
	declareGlobal(".directory-results",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.RepeatFill(gwccss.MinMax(gwccss.TrackLen(gwccss.Rem(14)), gwccss.Fr(1)))),
		gwccss.Gap(gwccss.Rem(.75)),
		gwccss.Raw("list-style", "none"),
		gwccss.Raw("padding", "0"),
		gwccss.Raw("margin", "0 0 1.5rem"),
	)
	// tokens.WorkspaceCSS caps every `li` at --measure-prose (65ch) for
	// readable prose. A directory card is not prose: inside a grid cell that
	// cap silently held each person to ~585px and left the rest of a 1280px
	// column empty, which is what made sixty people a metre-long page.
	declareGlobal(".directory-results li,.org-tree li",
		gwccss.Raw("max-inline-size", "none"),
	)
	declareGlobal(".directory-entry",
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Raw("align-items", "flex-start"),
		gwccss.Gap(gwccss.Rem(.2)),
		gwccss.Padding(gwccss.Rem(.625)),
		gwccss.Border(gwccss.Px(1), gwccss.Hex("dbe5df")),
		gwccss.Rounded(gwccss.Rem(.625)),
		gwccss.Bg(gwccss.Hex("fbfcfb")),
		gwccss.Raw("height", "100%"),
	)
	declareGlobal(".directory-name",
		gwccss.Display.Block,
		gwccss.Raw("font-weight", "700"),
		gwccss.Raw("line-height", "1.3"),
	)
	// The worker number and job title ride the name's line and wrap under it
	// only when the column is too narrow to hold both.
	declareGlobal(".directory-ident",
		gwccss.TextColor(gwccss.Hex("53645b")),
		gwccss.FontSize(gwccss.Rem(.85)),
		gwccss.Raw("font-weight", "400"),
		gwccss.Raw("margin-left", ".4rem"),
	)
	declareGlobal(".directory-meta",
		gwccss.Display.Block,
		gwccss.TextColor(gwccss.Hex("53645b")),
		gwccss.FontSize(gwccss.Rem(.85)),
	)
	declareGlobal(".directory-roles",
		gwccss.Display.Block,
		gwccss.TextColor(gwccss.Hex("17231d")),
		gwccss.FontSize(gwccss.Rem(.85)),
	)
	declareGlobal(".directory-unavailable",
		gwccss.Display.Block,
		gwccss.TextColor(gwccss.Hex("7a5a12")),
		gwccss.FontSize(gwccss.Rem(.85)),
	)
	declareGlobal(".directory-signin",
		gwccss.Raw("margin-top", "auto"),
		gwccss.Raw("padding-top", ".5rem"),
		gwccss.W(gwccss.Percent(100)),
	)
	declareGlobal(".directory-signin button",
		gwccss.W(gwccss.Percent(100)),
		gwccss.Bg(gwccss.Hex("147a4a")),
		gwccss.TextColor(gwccss.Hex("fff")),
	)
	declareGlobal(".directory-signin button:hover",
		gwccss.Bg(gwccss.Hex("0e623a")),
	)
	declareGlobal(".directory-signin button:focus-visible",
		gwccss.Raw("outline", "3px solid #0e623a"),
		gwccss.Raw("outline-offset", "2px"),
	)
	declareGlobal(".org-tree",
		gwccss.Raw("list-style", "none"),
		gwccss.Raw("padding", "0"),
		gwccss.Raw("margin", "0"),
	)
	declareGlobal(".org-tree ul",
		gwccss.Raw("list-style", "none"),
		gwccss.Raw("margin", ".4rem 0 .4rem 0"),
		gwccss.Raw("padding-left", "1rem"),
		gwccss.Raw("border-left", "2px solid #e7eeea"),
	)
	declareGlobal(".org-unit > details > summary",
		gwccss.Raw("cursor", "pointer"),
		gwccss.Raw("padding", ".4rem .25rem"),
		gwccss.Raw("font-weight", "700"),
		gwccss.Rounded(gwccss.Rem(.375)),
	)
	declareGlobal(".org-unit > details > summary:hover",
		gwccss.Bg(gwccss.Hex("eef4f0")),
	)
	declareGlobal(".org-unit > details > summary:focus-visible",
		gwccss.Raw("outline", "3px solid #0e623a"),
		gwccss.Raw("outline-offset", "2px"),
	)
	// UXBLIND-053: the disclosure keeps a visible affordance once the native
	// marker is replaced, and the result summary shows where focus landed.
	declareGlobal(".org-unit > details > summary::-webkit-details-marker",
		gwccss.Display.None,
	)
	declareGlobal(".org-unit > details > summary::before",
		gwccss.Raw("content", `"\25B8"`),
		gwccss.Raw("display", "inline-block"),
		gwccss.Raw("margin-inline-end", ".375rem"),
		gwccss.Raw("transition", "transform .15s"),
	)
	declareGlobal(".org-unit > details[open] > summary::before",
		gwccss.Raw("transform", "rotate(90deg)"),
	)
	declareGlobal(".directory-summary:focus-visible",
		gwccss.Raw("outline", "3px solid #0e623a"),
		gwccss.Raw("outline-offset", "2px"),
	)
	declareGlobal(".org-count",
		gwccss.TextColor(gwccss.Hex("53645b")),
		gwccss.Raw("font-weight", "400"),
		gwccss.FontSize(gwccss.Rem(.8)),
	)
	// A unit's contents are one list, so the ARIA group stays one group, but
	// it lays out as a grid: child units take a full row of their own and the
	// people fill 2-3 columns at desktop and one at 390px, which is what lets
	// a unit's whole staff fit on a screen.
	declareGlobal(".org-tree ul[role=\"group\"]",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.RepeatFill(gwccss.MinMax(gwccss.TrackLen(gwccss.Rem(14)), gwccss.Fr(1)))),
		gwccss.Gap(gwccss.Rem(.5)),
	)
	declareGlobal(".org-tree ul[role=\"group\"] > .org-unit",
		gwccss.Raw("grid-column", "1 / -1"),
	)
	declareGlobal(".org-person",
		gwccss.Raw("margin", "0"),
	)
	declareGlobal(".org-tree ul",
		mediaRule(gwccss.RawMedia("(max-width:42.5rem)"), gwccss.Raw("padding-left", ".625rem")),
	)
	declareGlobal(".directory-results",
		mediaRule(gwccss.RawMedia("(max-width:30rem)"), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".org-tree ul",
		mediaRule(gwccss.RawMedia("(max-width:30rem)"), gwccss.Raw("padding-left", ".4rem")),
	)
	declareGlobal(".org-tree ul[role=\"group\"]",
		mediaRule(gwccss.RawMedia("(max-width:30rem)"), gwccss.GridCols(gwccss.Fr(1))),
	)
}
