package projectclient

import (
	"testing"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
)

func TestStatusCategoryUsesDeclaredOutcomeAcrossReopenableWorkflows(t *testing.T) {
	tests := []struct {
		name     string
		status   *projectv1.ProjectStatus
		index    int
		category string
	}{
		{"done with a reopen transition", &projectv1.ProjectStatus{Category: projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_DONE, AllowedNextStatusIds: []string{"active"}}, 2, CategoryDone},
		{"active without transitions", &projectv1.ProjectStatus{Category: projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_ACTIVE}, 2, CategoryActive},
		{"first blocked", &projectv1.ProjectStatus{Category: projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_BLOCKED}, 0, CategoryActive},
		{"cancelled is not completed work", &projectv1.ProjectStatus{Category: projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_CANCELLED}, 3, CategoryActive},
		{"explicit not started after first", &projectv1.ProjectStatus{Category: projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_NOT_STARTED}, 2, CategoryTodo},
		{"legacy first", &projectv1.ProjectStatus{}, 0, CategoryTodo},
		{"legacy later", &projectv1.ProjectStatus{}, 1, CategoryActive},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := StatusCategory(test.status, test.index); got != test.category {
				t.Fatalf("StatusCategory() = %q, want %q", got, test.category)
			}
		})
	}
}
