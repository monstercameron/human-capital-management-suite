package chat

const (
	PermissionRemoveMessages        = "Remove messages"
	PermissionReviewRemovedMessages = "Review removed messages"
	PermissionManageFilters         = "Manage filters"
	PermissionReport                = "Report"
)

func ValidModerationPermission(permission string) bool {
	switch permission {
	case PermissionRemoveMessages, PermissionReviewRemovedMessages, PermissionManageFilters, PermissionReport:
		return true
	}
	return false
}
