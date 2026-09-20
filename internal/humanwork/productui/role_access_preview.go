package productui

import (
	"strconv"
	"strings"

	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// RoleAccessPreview is the server-resolved answer to "what would this role
// reveal if I saved this visibility draft?" (UXSCAN-008, REV-093-01). The
// browser renders it; it never computes or widens it. It carries role and
// unit names and counts only, never a worker record.
type RoleAccessPreview struct {
	RoleID   string
	RoleName string
	// ExplicitRoles is the role being changed; InheritedRoles are the other
	// roles its saved holders also carry, whose access they keep regardless.
	ExplicitRoles  []string
	InheritedRoles []string
	// AdministratorOverride is true for hcm_admin and comp_admin: their
	// holders see every unit whatever the saved visibility policy says.
	AdministratorOverride bool
	CurrentMode           string
	CurrentUnits          []string
	CurrentRelative       bool
	ProposedMode          string
	ProposedUnits         []string
	ProposedRelative      bool
	AddedUnits            []string
	RemovedUnits          []string
	HolderCount           int
	OverriddenHolderCount int
}

// RoleAccessPreviewRequest asks the server to resolve a preview for policy
// and reports the answer through done. done may run on another goroutine.
type RoleAccessPreviewRequest func(policy OrganizationVisibilityPolicy, done func(RoleAccessPreview, error))

const (
	roleAccessPreviewIdle    = "idle"
	roleAccessPreviewLoading = "loading"
	roleAccessPreviewReady   = "ready"
	roleAccessPreviewFailed  = "failed"
)

type roleAccessPreviewState struct {
	Status    string
	Seq       int
	Requested OrganizationVisibilityPolicy
	Preview   RoleAccessPreview
}

type roleAccessPreviewPanelProps struct {
	Page    OrganizationVisibilityPageProps
	Role    AccessRole
	Draft   OrganizationVisibilityPolicy
	Invalid bool
}

// RoleAccessPreviewPanel is the inline current-versus-proposed preview in a
// role's visibility editor. Without a connected preview service (server
// render before the client starts) it keeps the honest unavailable notice.
func RoleAccessPreviewPanel(props roleAccessPreviewPanelProps) ui.Node {
	state := ui.UseState(roleAccessPreviewState{Status: roleAccessPreviewIdle})
	current := state.Get()
	request := props.Page.OnPreview
	onClick := ui.UseEvent(func(ui.MouseEvent) {
		if request == nil || props.Invalid {
			return
		}
		next := state.Get()
		next.Seq++
		next.Status = roleAccessPreviewLoading
		next.Requested = organizationVisibilityPolicySnapshot(props.Draft)
		seq := next.Seq
		state.Set(next)
		request(next.Requested, func(preview RoleAccessPreview, err error) {
			latest := state.Get()
			if latest.Seq != seq {
				return
			}
			if err != nil || !strings.EqualFold(preview.RoleID, props.Role.ID) {
				latest.Status = roleAccessPreviewFailed
			} else {
				latest.Status, latest.Preview = roleAccessPreviewReady, preview
			}
			state.Set(latest)
		})
	})
	if request == nil {
		return organizationVisibilityPreviewUnavailable(props.Page)
	}
	return roleAccessPreviewNode(props, current, onClick)
}

func roleAccessPreviewNode(props roleAccessPreviewPanelProps, current roleAccessPreviewState, onClick ui.Handler) ui.Node {
	locale := props.Page.Locale
	text := func(key string) string { return roleAccessPreviewText(locale, key) }
	titleID := "role-access-preview-title-" + props.Role.ID
	loading := current.Status == roleAccessPreviewLoading
	buttonLabel := text("preview")
	if current.Status == roleAccessPreviewReady || current.Status == roleAccessPreviewFailed {
		buttonLabel = text("preview_again")
	}
	button := html.Button(html.Props{Class: "button secondary role-access-preview-action", Type: "button", Disabled: loading || props.Invalid, OnClick: onClick,
		Aria: map[string]string{"describedby": "role-access-preview-help-" + props.Role.ID}}, ui.Text(buttonLabel))
	head := html.Div(html.Props{Class: "role-access-preview-head"},
		html.Div(html.Props{},
			html.H3(html.Props{ID: titleID}, ui.Text(text("title"))),
			html.P(html.Props{ID: "role-access-preview-help-" + props.Role.ID, Class: "muted"}, ui.Text(text("help"))),
		),
		button,
	)
	statusRaw := map[string]any{"role": "status", "aria-live": "polite"}
	var body []ui.Node
	switch current.Status {
	case roleAccessPreviewLoading:
		statusRaw["aria-busy"] = "true"
		body = append(body, html.P(html.Props{Class: "role-access-preview-message"}, ui.Text(text("loading"))))
	case roleAccessPreviewFailed:
		statusRaw = map[string]any{"role": "alert", "aria-live": "assertive"}
		body = append(body, html.P(html.Props{Class: "role-access-preview-message role-access-preview-error"}, ui.Text(text("failed"))))
	case roleAccessPreviewReady:
		if !organizationVisibilityPolicyEqual(current.Requested, props.Draft) {
			body = append(body, html.P(html.Props{Class: "role-access-preview-message role-access-preview-stale"}, ui.Text(text("stale"))))
		}
		body = append(body, roleAccessPreviewResult(props, current.Preview)...)
	}
	sectionProps := html.Props{Class: "role-access-preview", DataAttr: html.DataAttribute{Name: "preview-state", Value: current.Status}, Raw: map[string]any{"aria-labelledby": titleID}}
	return html.Section(sectionProps, head, html.Div(html.Props{Class: "role-access-preview-body", Raw: statusRaw}, body...))
}

func roleAccessPreviewResult(props roleAccessPreviewPanelProps, preview RoleAccessPreview) []ui.Node {
	locale := props.Page.Locale
	text := func(key string) string { return roleAccessPreviewText(locale, key) }
	var nodes []ui.Node
	if preview.AdministratorOverride {
		nodes = append(nodes, html.Div(html.Props{Class: "role-access-preview-override"},
			html.Strong(html.Props{}, ui.Text(text("override_title"))),
			html.P(html.Props{}, ui.Text(text("override_detail")))))
	}
	nodes = append(nodes, html.Div(html.Props{Class: "role-access-preview-compare"},
		roleAccessPreviewScope(props, "current", preview.CurrentMode, preview.CurrentUnits, preview.CurrentRelative, preview.AdministratorOverride, nil, nil),
		roleAccessPreviewScope(props, "proposed", preview.ProposedMode, preview.ProposedUnits, preview.ProposedRelative, preview.AdministratorOverride, preview.AddedUnits, preview.RemovedUnits),
	))
	inherited := text("none")
	if len(preview.InheritedRoles) > 0 {
		inherited = strings.Join(preview.InheritedRoles, ", ")
	}
	explicit := preview.RoleID
	if len(preview.ExplicitRoles) > 0 {
		explicit = strings.Join(preview.ExplicitRoles, ", ")
	}
	facts := []ui.Node{
		html.Tag("dt", html.Props{}, ui.Text(text("explicit"))), html.Tag("dd", html.Props{}, ui.Text(explicit)),
		html.Tag("dt", html.Props{}, ui.Text(text("inherited"))), html.Tag("dd", html.Props{}, ui.Text(inherited)),
		html.Tag("dt", html.Props{}, ui.Text(text("holders"))), html.Tag("dd", html.Props{}, ui.Text(strconv.Itoa(preview.HolderCount))),
	}
	if preview.OverriddenHolderCount > 0 && !preview.AdministratorOverride {
		facts = append(facts, html.Tag("dt", html.Props{}, ui.Text(text("overridden"))), html.Tag("dd", html.Props{}, ui.Text(strconv.Itoa(preview.OverriddenHolderCount))))
	}
	nodes = append(nodes, html.Tag("dl", html.Props{Class: "role-access-preview-facts"}, facts...))
	nodes = append(nodes, html.P(html.Props{Class: "muted role-access-preview-withheld"}, ui.Text(text("withheld"))))
	return nodes
}

func roleAccessPreviewScope(props roleAccessPreviewPanelProps, which, mode string, units []string, relative, override bool, added, removed []string) ui.Node {
	locale := props.Page.Locale
	text := func(key string) string { return roleAccessPreviewText(locale, key) }
	modeLabel := localizedVisibilityModeLabel(props.Page, mode)
	if override {
		modeLabel = text("override_scope")
	}
	children := []ui.Node{
		html.H4(html.Props{}, ui.Text(text(which))),
		html.P(html.Props{Class: "role-access-preview-mode"}, ui.Text(modeLabel)),
	}
	switch {
	case len(units) > 0:
		chips := make([]ui.Node, 0, len(units))
		for _, unit := range units {
			chips = append(chips, html.Li(html.Props{Class: "organization-scope-unit"}, ui.Text(unit)))
		}
		label := text("units_count")
		if relative {
			label = text("relative_units")
		}
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(strings.ReplaceAll(label, "{count}", strconv.Itoa(len(units))))),
			html.Ul(html.Props{Class: "organization-scope-units", Aria: map[string]string{"label": text(which)}}, chips...))
	case relative:
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(text("relative_empty"))))
	default:
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(text("no_units"))))
	}
	if len(added) > 0 {
		children = append(children, html.P(html.Props{Class: "role-access-preview-change role-access-preview-added"}, html.Strong(html.Props{}, ui.Text(text("added"))), ui.Text(" "+strings.Join(added, ", "))))
	}
	if len(removed) > 0 {
		children = append(children, html.P(html.Props{Class: "role-access-preview-change role-access-preview-removed"}, html.Strong(html.Props{}, ui.Text(text("removed"))), ui.Text(" "+strings.Join(removed, ", "))))
	}
	return html.Div(html.Props{Class: "role-access-preview-scope role-access-preview-" + which}, children...)
}

// roleAccessPreviewCopy is the preview's reviewed copy in the three product
// locales. It lives beside the component so the shared catalog stays
// untouched by this feature; unknown locales fall back to English.
var roleAccessPreviewCopy = map[string]map[string]string{
	"en": {
		"title": "Access preview", "help": "See what this role reveals now and after your changes. Nothing is saved.",
		"preview": "Preview access", "preview_again": "Preview again",
		"loading": "Checking what this role would reveal…", "failed": "We couldn't load the access preview. Try again.",
		"stale":   "You changed this draft after the preview. Preview again to update it.",
		"current": "Current (saved)", "proposed": "Proposed (this draft)",
		"units_count": "Organization units: {count}", "relative_units": "Each person sees their own unit. Units among people with this role: {count}",
		"relative_empty": "Each person sees their own unit. Nobody has a saved assignment to this role yet.",
		"no_units":       "No organization units",
		"added":          "Newly visible:", "removed": "No longer visible:",
		"explicit": "Role being changed", "inherited": "Other roles its people also hold", "holders": "People with this role saved",
		"overridden": "Of those, also administrators (see every unit)", "none": "None",
		"override_title":         "Administrator override",
		"override_detail":        "People with this role see every organization unit. The visibility setting is saved but does not narrow their access.",
		"override_scope":         "Every organization unit",
		"override_summary":       "Everyone (administrator override)",
		"override_editor_detail": "People with this role always see every worker. The saved visibility setting does not narrow administrators, so there is nothing to change here.",
		"withheld":               "Everyone always sees their own record, and people managers also see their reporting line. People who get this role only from sign-in are not counted.",
	},
	"de": {
		"title": "Zugriffsvorschau", "help": "Sehen Sie, was diese Rolle jetzt und nach Ihren Änderungen freigibt. Es wird nichts gespeichert.",
		"preview": "Zugriff prüfen", "preview_again": "Erneut prüfen",
		"loading": "Freigaben dieser Rolle werden geprüft…", "failed": "Die Zugriffsvorschau konnte nicht geladen werden. Versuchen Sie es erneut.",
		"stale":   "Sie haben den Entwurf nach der Vorschau geändert. Prüfen Sie erneut, um sie zu aktualisieren.",
		"current": "Aktuell (gespeichert)", "proposed": "Vorgeschlagen (dieser Entwurf)",
		"units_count": "Organisationseinheiten: {count}", "relative_units": "Jede Person sieht ihre eigene Einheit. Einheiten der Personen mit dieser Rolle: {count}",
		"relative_empty": "Jede Person sieht ihre eigene Einheit. Diese Rolle ist noch niemandem gespeichert zugewiesen.",
		"no_units":       "Keine Organisationseinheiten",
		"added":          "Neu sichtbar:", "removed": "Nicht mehr sichtbar:",
		"explicit": "Geänderte Rolle", "inherited": "Weitere Rollen dieser Personen", "holders": "Personen mit gespeicherter Rolle",
		"overridden": "Davon auch Administratoren (sehen alle Einheiten)", "none": "Keine",
		"override_title":         "Administratorzugriff",
		"override_detail":        "Personen mit dieser Rolle sehen alle Organisationseinheiten. Die Sichtbarkeitseinstellung wird gespeichert, schränkt ihren Zugriff aber nicht ein.",
		"override_scope":         "Alle Organisationseinheiten",
		"override_summary":       "Alle (Administratorzugriff)",
		"override_editor_detail": "Personen mit dieser Rolle sehen immer alle Beschäftigten. Die gespeicherte Sichtbarkeitseinstellung schränkt Administratoren nicht ein, daher gibt es hier nichts zu ändern.",
		"withheld":               "Jede Person sieht immer ihre eigene Akte, Führungskräfte zusätzlich ihre Berichtslinie. Personen, die diese Rolle nur über die Anmeldung erhalten, werden nicht gezählt.",
	},
	"ar": {
		"title": "معاينة الوصول", "help": "اطّلع على ما يكشفه هذا الدور الآن وبعد تغييراتك. لا يُحفظ أي شيء.",
		"preview": "معاينة الوصول", "preview_again": "المعاينة مرة أخرى",
		"loading": "جارٍ التحقق مما سيكشفه هذا الدور…", "failed": "تعذّر تحميل معاينة الوصول. حاول مرة أخرى.",
		"stale":   "غيّرت المسودة بعد المعاينة. عاين مرة أخرى لتحديثها.",
		"current": "الحالي (محفوظ)", "proposed": "المقترح (هذه المسودة)",
		"units_count": "الوحدات التنظيمية: {count}", "relative_units": "يرى كل شخص وحدته. وحدات أصحاب هذا الدور: {count}",
		"relative_empty": "يرى كل شخص وحدته. لا يوجد إسناد محفوظ لهذا الدور بعد.",
		"no_units":       "لا وحدات تنظيمية",
		"added":          "مرئي حديثاً:", "removed": "لم يعد مرئياً:",
		"explicit": "الدور الذي يتغير", "inherited": "أدوار أخرى يحملها أصحابه", "holders": "أشخاص لديهم هذا الدور محفوظاً",
		"overridden": "منهم مسؤولون أيضاً (يرون كل الوحدات)", "none": "لا شيء",
		"override_title":         "تجاوز المسؤول",
		"override_detail":        "يرى أصحاب هذا الدور كل الوحدات التنظيمية. يُحفظ إعداد الرؤية لكنه لا يقيّد وصولهم.",
		"override_scope":         "كل الوحدات التنظيمية",
		"override_summary":       "الجميع (تجاوز المسؤول)",
		"override_editor_detail": "يرى أصحاب هذا الدور جميع الموظفين دائماً. لا يقيّد إعداد الرؤية المحفوظ المسؤولين، لذا لا يوجد ما يمكن تغييره هنا.",
		"withheld":               "يرى كل شخص سجله دائماً، ويرى المديرون أيضاً خط التقارير لديهم. لا يُحتسب من يحصل على هذا الدور من تسجيل الدخول فقط.",
	},
}

func roleAccessPreviewText(locale LocaleContext, key string) string {
	language := strings.ToLower(locale.normalized().Resolved)
	if index := strings.IndexByte(language, '-'); index > 0 {
		language = language[:index]
	}
	if value, ok := roleAccessPreviewCopy[language][key]; ok {
		return value
	}
	return roleAccessPreviewCopy["en"][key]
}

func roleAccessPreviewStylesheet() string {
	return buildTypedSheet(declareRoleAccessPreviewStyles)
}

// declareRoleAccessPreviewStyles lays the preview out inside the editor's
// form padding. The compare grid is one column on a phone and two side by
// side once each card has room, so 390 px never scrolls sideways.
func declareRoleAccessPreviewStyles() {
	declareAdministratorOverrideStyles()
	declareGlobal(".role-access-preview",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(12)),
		gwccss.Raw("padding", "0 22px 18px"),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".role-access-preview-head",
		gwccss.Display.Flex, gwccss.Raw("flex-wrap", "wrap"), gwccss.Raw("align-items", "flex-start"),
		gwccss.Raw("justify-content", "space-between"), gwccss.Gap(gwccss.Px(12)),
	)
	declareGlobal(".role-access-preview-head h3",
		gwccss.Margin(gwccss.Zero), gwccss.FontSize(gwccss.Rem(0.9375)),
	)
	declareGlobal(".role-access-preview-head p",
		gwccss.Raw("margin", "4px 0 0"), gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".role-access-preview-head>div",
		gwccss.Raw("flex", "1 1 16rem"), gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".role-access-preview-body",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(12)), gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".role-access-preview-message",
		gwccss.Margin(gwccss.Zero), gwccss.FontSize(gwccss.Rem(0.875)),
	)
	declareGlobal(".role-access-preview-error",
		gwccss.Raw("color", "var(--danger, #b42318)"),
	)
	declareGlobal(".role-access-preview-override",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(4)),
		gwccss.Raw("padding", "10px 12px"), gwccss.Raw("border-radius", "var(--radius, 8px)"),
		gwccss.Raw("border", "1px solid var(--line, rgba(0,0,0,.14))"),
		gwccss.Raw("background", "var(--surface-muted, transparent)"),
	)
	declareGlobal(".role-access-preview-override p",
		gwccss.Margin(gwccss.Zero), gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".role-access-preview-compare",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(12)),
		gwccss.Raw("grid-template-columns", "repeat(auto-fit, minmax(min(100%, 14rem), 1fr))"),
	)
	declareGlobal(".role-access-preview-scope",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(6)), gwccss.Raw("align-content", "start"), gwccss.MinWidth(gwccss.Zero),
		gwccss.Raw("padding", "12px"), gwccss.Raw("border-radius", "var(--radius, 8px)"),
		gwccss.Raw("border", "1px solid var(--line, rgba(0,0,0,.14))"),
	)
	declareGlobal(".role-access-preview-scope h4",
		gwccss.Margin(gwccss.Zero), gwccss.FontSize(gwccss.Rem(0.8125)), gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(".role-access-preview-scope p",
		gwccss.Margin(gwccss.Zero), gwccss.FontSize(gwccss.Rem(0.8125)), gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(".role-access-preview-facts",
		gwccss.Display.Grid, gwccss.Margin(gwccss.Zero), gwccss.Raw("gap", "4px 12px"),
		gwccss.Raw("grid-template-columns", "minmax(0, max-content) minmax(0, 1fr)"), gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".role-access-preview-facts dt", gwccss.Raw("font-weight", "600"))
	declareGlobal(".role-access-preview-facts dd", gwccss.Margin(gwccss.Zero), gwccss.Raw("overflow-wrap", "anywhere"))
	declareGlobal(".role-access-preview-withheld", gwccss.Margin(gwccss.Zero), gwccss.FontSize(gwccss.Rem(0.8125)))
	declareGlobal(".role-access-preview .organization-scope-units",
		gwccss.Display.Flex, gwccss.Raw("flex-wrap", "wrap"), gwccss.Gap(gwccss.Px(6)),
		gwccss.Margin(gwccss.Zero), gwccss.Raw("padding", "0"), gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".role-access-preview .organization-scope-unit", gwccss.MinWidth(gwccss.Zero), gwccss.Raw("overflow-wrap", "anywhere"))
}
