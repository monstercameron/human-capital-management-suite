package chatui

import (
	"net/url"
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// CHATMOD-005: the four moderation permissions are "each assignable to a role
// per workspace and per channel". The server could store an assignment and no
// page could make one. This is the third tab of the Moderation page, for a
// workspace administrator: one table, a row for each role and a column for each
// permission. Every cell is a switch that says in words what the role may do
// and where that answer comes from: the product's default, the workspace's
// setting, or this channel's own.

// ModerationPermissionsModel is the table's data: the stored answers for the
// workspace and, when a channel is chosen, for that channel.
type ModerationPermissionsModel struct {
	Rows []chat.ModerationPermissionRow
	// ConversationID is the chosen channel ("" for the whole workspace) and
	// ConversationName its name.
	ConversationID, ConversationName string
	// Channels is what the administrator can choose from.
	Channels []ModerationChannelChoice
	// ExtraRole adds a row for a role that has no stored answer yet.
	ExtraRole string
}

// ModerationChannelChoice is one channel the table can be shown for.
type ModerationChannelChoice struct{ ID, Name string }

// moderationRoleOrder is the roles the table always shows: the three the
// product's defaults are written for.
var moderationRoleOrder = []string{chat.WorkspaceAdministratorModerationRole, string(chat.Manager), string(chat.Member)}

// ModerationPermissionRoles is the table's rows: the three standing roles,
// then every other role with a stored answer or asked for by name.
func ModerationPermissionRoles(m ModerationPermissionsModel) []string {
	roles := append([]string(nil), moderationRoleOrder...)
	seen := map[string]bool{}
	for _, role := range roles {
		seen[role] = true
	}
	var others []string
	add := func(role string) {
		if role = strings.TrimSpace(role); role != "" && !seen[role] {
			seen[role] = true
			others = append(others, role)
		}
	}
	for _, row := range m.Rows {
		add(row.Role)
	}
	add(m.ExtraRole)
	sort.Strings(others)
	return append(roles, others...)
}

// ModerationPermissionCell is what one role holds for one permission where the
// table is looking, and where that answer comes from: "own" (this channel's
// row), "workspace" (the workspace's row) or "default".
type ModerationPermissionCell struct {
	Allowed bool
	Source  string
}

// ModerationPermissionOf resolves one cell the way the server decides: the
// channel's row, else the workspace's, else the default.
func ModerationPermissionOf(m ModerationPermissionsModel, role, permission string) ModerationPermissionCell {
	cell := ModerationPermissionCell{Allowed: chat.DefaultModerationPermission(role, role == string(chat.Manager) || role == string(chat.Member), permission), Source: "default"}
	for _, row := range m.Rows {
		if row.Role != role || row.Permission != permission || row.ConversationID != "" {
			continue
		}
		cell = ModerationPermissionCell{Allowed: row.Allowed, Source: "workspace"}
	}
	if m.ConversationID != "" {
		for _, row := range m.Rows {
			if row.Role == role && row.Permission == permission && row.ConversationID == m.ConversationID {
				cell = ModerationPermissionCell{Allowed: row.Allowed, Source: "own"}
			}
		}
	}
	return cell
}

var moderationPermissionKeys = map[string]string{
	chat.PermissionReport:                "perm_report",
	chat.PermissionRemoveMessages:        "perm_remove",
	chat.PermissionReviewRemovedMessages: "perm_review",
	chat.PermissionManageFilters:         "perm_filters",
}

// moderationRoleName is a role in words. A role the product has no name for is
// shown by the name the workspace gave it.
func moderationRoleName(locale, role string) string {
	if text := chatremoveText(locale, "role_"+role); text != "" {
		return text
	}
	return role
}

func moderationPermissionsHref(locale, conversation, role string) string {
	q := url.Values{"tab": {"permissions"}, "locale": {locale}}
	if conversation != "" {
		q.Set("conversation", conversation)
	}
	if role != "" {
		q.Set("role", role)
	}
	return ModerationPageHref + "?" + q.Encode()
}

// moderationPermissionsView is the Permissions tab.
func moderationPermissionsView(locale string, m ModerationPermissionsModel) []ui.Node {
	t := func(key string) string { return chatremoveText(locale, key) }
	scope := t("perm_scope_ws")
	if m.ConversationID != "" {
		scope = strings.ReplaceAll(t("perm_scope_ch"), "{name}", m.ConversationName)
	}
	nodes := []ui.Node{
		html.P(html.Props{Class: "chatmod005-muted", Text: t("perm_intro")}),
		html.H3(html.Props{ID: "chatmod005-perm-scope", Class: "chatmod005-subheading", Dir: "auto", Text: scope}),
	}
	// The scope: the workspace, or one of the channels.
	var choices []ui.Node
	if m.ConversationID != "" {
		choices = append(choices, html.Button(html.Props{Type: "button", Class: "chatremove-link", Text: t("perm_back_ws"), Data: map[string]string{"chatremove-open": moderationPermissionsHref(locale, "", m.ExtraRole)}}))
	}
	if len(m.Channels) > 0 {
		var list []ui.Node
		for _, c := range m.Channels {
			if c.ID == m.ConversationID {
				continue
			}
			list = append(list, html.Li(html.Props{}, html.Button(html.Props{Type: "button", Class: "chatremove-link", Dir: "auto", Text: "#" + c.Name, Data: map[string]string{"chatremove-open": moderationPermissionsHref(locale, c.ID, m.ExtraRole)}})))
		}
		if len(list) > 0 {
			choices = append(choices, html.Details(html.Props{Class: "chatmod005-context chatmod005-perm-channels"}, html.Summary(html.Props{Text: t("perm_choose")}), html.Ul(html.Props{Class: "chatmod005-perm-list"}, list...)))
		}
	}
	if len(choices) > 0 {
		nodes = append(nodes, html.Div(html.Props{Class: "chatmod005-perm-scope"}, choices...))
	}
	head := []ui.Node{html.Th(html.Props{Raw: map[string]any{"scope": "col"}, Text: t("perm_role")})}
	for _, permission := range chat.ModerationPermissions {
		head = append(head, html.Th(html.Props{Raw: map[string]any{"scope": "col"}, Text: t(moderationPermissionKeys[permission])}))
	}
	var rows []ui.Node
	for _, role := range ModerationPermissionRoles(m) {
		name := moderationRoleName(locale, role)
		cells := []ui.Node{html.Th(html.Props{Raw: map[string]any{"scope": "row"}, Dir: "auto", Text: name})}
		for _, permission := range chat.ModerationPermissions {
			cell := ModerationPermissionOf(m, role, permission)
			state := t("perm_no")
			if cell.Allowed {
				state = t("perm_yes")
			}
			label := strings.NewReplacer("{permission}", t(moderationPermissionKeys[permission]), "{role}", name).Replace(t("perm_cell"))
			cells = append(cells, html.Td(html.Props{},
				html.Button(html.Props{Type: "button", Role: "switch", Class: "chatmod005-perm-switch",
					Aria: map[string]string{"checked": boolString(cell.Allowed), "label": label},
					Data: map[string]string{"chatremove-act": "permission", "role": role, "permission": permission, "allowed": boolString(!cell.Allowed), "conversation": m.ConversationID}},
					html.Span(html.Props{Class: "chatmod005-perm-state", Text: state})),
				html.Span(html.Props{Class: "chatmod005-muted chatmod005-perm-source", Text: t("perm_src_" + cell.Source)})))
		}
		rows = append(rows, html.Tr(html.Props{}, cells...))
	}
	nodes = append(nodes, html.Div(html.Props{Class: "chatmod005-perm-scroll", TabIndex: html.TabIndexZero, Role: "region", Aria: map[string]string{"labelledby": "chatmod005-perm-scope"}},
		html.Table(html.Props{Class: "chatmod005-perm-table", Data: map[string]string{"chatremove-items": "permissions"}}, html.Thead(html.Props{}, html.Tr(html.Props{}, head...)), html.Tbody(html.Props{}, rows...))),
		html.P(html.Props{Class: "chatmod005-error chatmod005-perm-error", Role: "alert", Hidden: true}),
		html.P(html.Props{Class: "chatmod005-muted", Text: t("perm_note")}),
		// A role the table does not list yet: its row appears, and nothing is
		// stored until one of its switches is pressed.
		html.Form(html.Props{Class: "chatmod005-perm-role", Data: map[string]string{"chatremove": "role", "conversation-id": m.ConversationID}},
			html.Label(html.Props{For: "chatremove-role", Text: t("perm_other")}),
			html.Input(html.Props{ID: "chatremove-role", Name: "role", Type: "text", AutoComplete: "off", MaxLength: 120, Required: true, Aria: map[string]string{"describedby": "chatremove-role-hint"}}),
			html.P(html.Props{ID: "chatremove-role-hint", Class: "chatmod005-muted", Text: t("perm_other_hint")}),
			html.Button(html.Props{Type: "submit", Text: t("perm_other_add")})))
	return nodes
}

// chatmod005PermissionsStyles: a table that scrolls sideways inside the page on
// a narrow screen, with switches a finger can press.
const chatmod005PermissionsStyles = `
.chatmod005-perm-scroll{overflow:auto;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control)}.chatmod005-perm-scroll:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}
.chatmod005-perm-table{inline-size:100%;border-collapse:collapse;font-size:.875rem}.chatmod005-perm-table th,.chatmod005-perm-table td{padding:8px;text-align:start;vertical-align:top;border-block-end:1px solid var(--hcm-color-border)}.chatmod005-perm-table thead th{background:var(--hcm-color-surface);font-weight:650}.chatmod005-perm-table tbody tr:last-child>*{border-block-end:0}
.chatremove .chatmod005-perm-switch{min-block-size:44px;min-inline-size:44px;padding:6px 12px;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface);color:var(--hcm-color-text);font:inherit;cursor:pointer}.chatremove .chatmod005-perm-switch[aria-checked=true]{border-color:var(--hcm-color-brand-primary);background:var(--hcm-color-brand-soft);font-weight:650}
.chatmod005-perm-source{display:block;font-size:.75rem;margin-block-start:2px}.chatmod005-perm-scope{display:flex;flex-wrap:wrap;align-items:center;gap:8px}.chatmod005-perm-list{list-style:none;margin:0;padding:0;display:flex;flex-wrap:wrap;gap:4px 8px}
.chatremove .chatmod005-perm-role{grid-template-columns:minmax(0,1fr);border:0;padding:0;margin-block:16px}.chatmod005-perm-error[hidden]{display:none}
`
