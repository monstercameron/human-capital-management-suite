package projectclient

import projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"

// StatusCategory groups workflow statuses for ticket filters and progress.
// A workflow can allow a completed task to be reopened, so transitions do
// not determine whether its current status is done.
func StatusCategory(status *projectv1.ProjectStatus, index int) string {
	if status != nil {
		switch status.GetCategory() {
		case projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_DONE:
			return CategoryDone
		case projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_NOT_STARTED:
			return CategoryTodo
		case projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_ACTIVE,
			projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_BLOCKED,
			projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_CANCELLED:
			return CategoryActive
		}
	}
	// Older configurations can omit the category. Their first status is
	// still the starting state; no unspecified state is assumed completed.
	if index == 0 {
		return CategoryTodo
	}
	return CategoryActive
}
