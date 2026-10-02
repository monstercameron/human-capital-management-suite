package chat

const (
	PermissionRemoveMessages        = "Remove messages"
	PermissionReviewRemovedMessages = "Review removed messages"
	PermissionManageFilters         = "Manage filters"
	PermissionReport                = "Report"
)

// ModerationOutcomeFilterNotify is the outcome of a notice a "notify" filter
// sends to the managers of the channel it names (CHATMOD-003).
const ModerationOutcomeFilterNotify = "filter_notify"

// WorkspaceAdministratorModerationRole is the role name the composition root
// projects for a current workspace administrator. It is the one role that
// holds every moderation permission by default.
const WorkspaceAdministratorModerationRole = "WORKSPACE_ADMIN"

// ModerationPermissions lists the four permissions in the order a table of
// them is read.
var ModerationPermissions = []string{PermissionReport, PermissionRemoveMessages, PermissionReviewRemovedMessages, PermissionManageFilters}

// DefaultModerationPermission is what a role holds when no row says otherwise
// (CHATMOD-005): a workspace administrator everything; a channel's manager
// removal and the filters of that channel; every member the right to report.
// member says the role is the person's role in the conversation, not one of
// their workspace roles.
func DefaultModerationPermission(role string, member bool, permission string) bool {
	switch {
	case role == WorkspaceAdministratorModerationRole:
		return true
	case role == string(Manager) && (permission == PermissionRemoveMessages || permission == PermissionManageFilters):
		return true
	}
	return member && role != "" && permission == PermissionReport
}

// ModerationPermissionRow is one stored answer: this role does, or does not,
// hold this permission in this conversation ("" is the whole workspace).
type ModerationPermissionRow struct {
	ConversationID, Role, Permission string
	Allowed                          bool
}

func ValidModerationPermission(permission string) bool {
	switch permission {
	case PermissionRemoveMessages, PermissionReviewRemovedMessages, PermissionManageFilters, PermissionReport:
		return true
	}
	return false
}
