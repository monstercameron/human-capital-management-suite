package main

import (
	"net/url"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// CHATMOD-005: the Permissions tab of the Moderation page. Each switch of its
// table is one assignment (this role does, or does not, hold this permission
// here), sent to the server as it is pressed; the page is then read again, so
// what is shown is what was stored.

// chatmod005PermissionInput is the assignment a pressed switch asks for, from
// the attributes the server rendered on it. ok is false for a switch that
// names no role or a permission the product does not have.
func chatmod005PermissionInput(role, permission, allowed, conversation string) (chatremoveClientInput, bool) {
	role = strings.TrimSpace(role)
	if role == "" || !chat.ValidModerationPermission(permission) || (allowed != "true" && allowed != "false") {
		return chatremoveClientInput{}, false
	}
	return chatremoveClientInput{ConversationID: conversation, Role: role, Permission: permission, Allowed: allowed == "true"}, true
}

// chatmod005RoleHref is the address of the Permissions tab with a row for one
// more role. Nothing is stored for the role until one of its switches is pressed.
func chatmod005RoleHref(pageHref, role string) string {
	u, err := url.Parse(pageHref)
	if err != nil || u.Path == "" {
		return pageHref
	}
	q := u.Query()
	q.Set("tab", "permissions")
	if role = strings.TrimSpace(role); role == "" {
		q.Del("role")
	} else {
		q.Set("role", role)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
