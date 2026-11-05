package manifest

import (
	"strings"
	"testing"
)

// TestTodo_PM_026 proves the GREEN coverage line: the generated manifest
// serves typed RPCs for project settings, membership, tasks, comments,
// views, config preview/publish, and allowlisted links.
func TestTodo_PM_026(t *testing.T) {
	m, err := Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	served := map[string]bool{}
	for _, endpoint := range m.Endpoints {
		if strings.HasPrefix(endpoint.EndpointID, "hcmnext.project.v1.ProjectService/") && endpoint.Disposition == DispositionServed {
			served[endpoint.EndpointID] = true
		}
	}
	const prefix = "hcmnext.project.v1.ProjectService/"
	surfaces := []struct {
		name    string
		methods []string
	}{
		{"project settings", []string{"GetProject", "UpdateProjectSettings"}},
		{"membership", []string{"InviteProjectMember", "AcceptProjectInvitation", "ChangeProjectMemberRole", "RevokeProjectMember", "ListProjectMembers"}},
		{"tasks", []string{"CreateTask", "GetTask", "ListTasks", "SearchTasks", "MoveTask", "PatchTask", "ArchiveTask", "RestoreTask"}},
		{"comments", []string{"AddTaskComment", "EditTaskComment", "DeleteTaskComment", "ListTaskComments"}},
		{"views", []string{"SaveBoardView", "ListBoardViews", "GetBoard"}},
		{"config preview/publish", []string{"SaveWorkflowDraft", "PreviewWorkflowDraft", "PublishWorkflowDraft", "GetWorkflowConfiguration"}},
		{"allowlisted links", []string{"AddTaskLink", "ListTaskLinks", "ListTaskLinksByTarget", "RemoveTaskLink"}},
	}
	for _, surface := range surfaces {
		for _, method := range surface.methods {
			if !served[prefix+method] {
				t.Errorf("%s: %s is not a served manifest method", surface.name, method)
			}
		}
	}
}
