package productui

import (
	"strings"

	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// IsAdministratorVisibilityRole reports whether roleID is one of the roles
// the workforce directory admits to every worker before any role visibility
// policy runs (hcm_admin, comp_admin). It mirrors
// roleaccess.IsAdministratorRole: the wasm client may not link that server
// package, and PreviewRoleAccess reports the same override from the server.
func IsAdministratorVisibilityRole(roleID string) bool {
	switch strings.ToLower(strings.TrimSpace(roleID)) {
	case "hcm_admin", "comp_admin":
		return true
	}
	return false
}

// administratorOverrideNotice explains, at the top of an administrator
// role's editor, why its visibility choices are disabled.
func administratorOverrideNotice(props OrganizationVisibilityPageProps, role AccessRole) ui.Node {
	return html.Div(html.Props{Class: "organization-visibility-override", ID: "organization-visibility-override-" + role.ID, Raw: map[string]any{"role": "note"}},
		html.Strong(html.Props{}, ui.Text(roleAccessPreviewText(props.Locale, "override_title"))),
		html.P(html.Props{}, ui.Text(roleAccessPreviewText(props.Locale, "override_editor_detail"))),
	)
}

func declareAdministratorOverrideStyles() {
	declareGlobal(".organization-visibility-override",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(4)),
		gwccss.Raw("margin", "0 22px 12px"), gwccss.Raw("padding", "10px 12px"),
		gwccss.Raw("border-radius", "var(--radius, 8px)"),
		gwccss.Raw("border", "1px solid var(--line, rgba(0,0,0,.14))"),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".organization-visibility-override p",
		gwccss.Margin(gwccss.Zero), gwccss.FontSize(gwccss.Rem(0.8125)), gwccss.Raw("overflow-wrap", "anywhere"),
	)
}
