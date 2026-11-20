package project

import (
	"testing"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	domainproject "github.com/monstercameron/human-capital-management-suite/internal/domains/project"
)

// TestIssue3750_ArchivedPreviewIsTitledWhenReadable pins that an archived
// ticket or board the caller may read keeps its title, that nothing else leaks
// with it, and that a refused or missing one still carries no detail.
func TestIssue3750_ArchivedPreviewIsTitledWhenReadable(t *testing.T) {
	ctx, _ := testContext(t)
	fake := newPreviewFake()
	fake.projects["live"] = activeProject("live", "Live")
	fake.projects["old"] = projectservice.ProjectRecord{ID: "old", Name: "Old board", State: domainproject.LifecycleArchived}
	fake.tasks["live/arch"] = projectservice.TaskRecord{ID: "arch", ProjectID: "live", Title: "Retired task", Archived: true, AssigneeID: "u1"}
	s := &server{service: fake}
	response, err := s.GetProjectPreviews(ctx, &projectv1.GetProjectPreviewsRequest{Refs: []*projectv1.ProjectPreviewRef{
		{ProjectId: "live", TaskId: "arch"}, {ProjectId: "old"}, {ProjectId: "old", TaskId: "x"}, {ProjectId: "live", TaskId: "gone"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*projectv1.ProjectPreview{}
	for _, item := range response.GetPreviews() {
		got[item.GetProjectId()+"/"+item.GetTaskId()] = item
	}
	if a := got["live/arch"]; a.GetState() != projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_ARCHIVED || a.GetTitle() != "Retired task" || a.GetAssigneeId() != "" {
		t.Errorf("archived task = %+v, want ARCHIVED with its title only", a)
	}
	if b := got["old/"]; b.GetState() != projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_ARCHIVED || b.GetTitle() != "Old board" {
		t.Errorf("archived board = %+v, want ARCHIVED with its name", b)
	}
	if c := got["old/x"]; c.GetState() != projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_ARCHIVED || c.GetTitle() != "" {
		t.Errorf("task in an archived project = %+v, want ARCHIVED without a title", c)
	}
	if d := got["live/gone"]; d.GetTitle() != "" {
		t.Errorf("missing task leaked detail: %+v", d)
	}
}
