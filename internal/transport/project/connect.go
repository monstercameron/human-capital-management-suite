package project

import (
	"context"
	"net/http"

	"connectrpc.com/connect"
	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
)

// NewConnectHandler projects the same ProjectService handlers used by gRPC
// onto Connect HTTP. Admission is supplied by the owning edge; all calls then
// enter the existing handlers, which derive the actor from trusted context and
// share the application service, error mapping, revision fences and replay
// behavior with gRPC.
func NewConnectHandler(deps Dependencies, opts ...connect.HandlerOption) http.Handler {
	s := &server{service: deps.Service, activity: deps.Activity, search: deps.Search}
	mux := http.NewServeMux()
	registerProjectUnary(mux, opts, projectv1.ProjectService_GetProjectMembership_FullMethodName, s.GetProjectMembership)
	registerProjectUnary(mux, opts, projectv1.ProjectService_ListProjectMembers_FullMethodName, s.ListProjectMembers)
	registerProjectUnary(mux, opts, projectv1.ProjectService_InviteProjectMember_FullMethodName, s.InviteProjectMember)
	registerProjectUnary(mux, opts, projectv1.ProjectService_AcceptProjectInvitation_FullMethodName, s.AcceptProjectInvitation)
	registerProjectUnary(mux, opts, projectv1.ProjectService_ChangeProjectMemberRole_FullMethodName, s.ChangeProjectMemberRole)
	registerProjectUnary(mux, opts, projectv1.ProjectService_RevokeProjectMember_FullMethodName, s.RevokeProjectMember)
	registerProjectUnary(mux, opts, projectv1.ProjectService_TransferProjectOwnership_FullMethodName, s.TransferProjectOwnership)
	registerProjectUnary(mux, opts, projectv1.ProjectService_GetProject_FullMethodName, s.GetProject)
	registerProjectUnary(mux, opts, projectv1.ProjectService_ListProjects_FullMethodName, s.ListProjects)
	registerProjectUnary(mux, opts, projectv1.ProjectService_CreateProject_FullMethodName, s.CreateProject)
	registerProjectUnary(mux, opts, projectv1.ProjectService_UpdateProjectSettings_FullMethodName, s.UpdateProjectSettings)
	registerProjectUnary(mux, opts, projectv1.ProjectService_ArchiveProject_FullMethodName, s.ArchiveProject)
	registerProjectUnary(mux, opts, projectv1.ProjectService_RestoreProject_FullMethodName, s.RestoreProject)
	registerProjectUnary(mux, opts, projectv1.ProjectService_CreateTask_FullMethodName, s.CreateTask)
	registerProjectUnary(mux, opts, projectv1.ProjectService_GetTask_FullMethodName, s.GetTask)
	registerProjectUnary(mux, opts, projectv1.ProjectService_ListTasks_FullMethodName, s.ListTasks)
	registerProjectUnary(mux, opts, projectv1.ProjectService_SearchTasks_FullMethodName, s.SearchTasks)
	registerProjectUnary(mux, opts, projectv1.ProjectService_MoveTask_FullMethodName, s.MoveTask)
	registerProjectUnary(mux, opts, projectv1.ProjectService_PatchTask_FullMethodName, s.PatchTask)
	registerProjectUnary(mux, opts, projectv1.ProjectService_ArchiveTask_FullMethodName, s.ArchiveTask)
	registerProjectUnary(mux, opts, projectv1.ProjectService_RestoreTask_FullMethodName, s.RestoreTask)
	registerProjectUnary(mux, opts, projectv1.ProjectService_SaveWorkflowDraft_FullMethodName, s.SaveWorkflowDraft)
	registerProjectUnary(mux, opts, projectv1.ProjectService_GetWorkflowDraft_FullMethodName, s.GetWorkflowDraft)
	registerProjectUnary(mux, opts, projectv1.ProjectService_PreviewWorkflowDraft_FullMethodName, s.PreviewWorkflowDraft)
	registerProjectUnary(mux, opts, projectv1.ProjectService_PublishWorkflowDraft_FullMethodName, s.PublishWorkflowDraft)
	registerProjectUnary(mux, opts, projectv1.ProjectService_GetWorkflowConfiguration_FullMethodName, s.GetWorkflowConfiguration)
	registerProjectUnary(mux, opts, projectv1.ProjectService_SaveBoardView_FullMethodName, s.SaveBoardView)
	registerProjectUnary(mux, opts, projectv1.ProjectService_ListBoardViews_FullMethodName, s.ListBoardViews)
	registerProjectUnary(mux, opts, projectv1.ProjectService_GetBoard_FullMethodName, s.GetBoard)
	registerProjectUnary(mux, opts, projectv1.ProjectService_AddTaskLink_FullMethodName, s.AddTaskLink)
	registerProjectUnary(mux, opts, projectv1.ProjectService_ListTaskLinks_FullMethodName, s.ListTaskLinks)
	registerProjectUnary(mux, opts, projectv1.ProjectService_ListTaskLinksByTarget_FullMethodName, s.ListTaskLinksByTarget)
	registerProjectUnary(mux, opts, projectv1.ProjectService_RemoveTaskLink_FullMethodName, s.RemoveTaskLink)
	registerProjectUnary(mux, opts, projectv1.ProjectService_AddTaskComment_FullMethodName, s.AddTaskComment)
	registerProjectUnary(mux, opts, projectv1.ProjectService_EditTaskComment_FullMethodName, s.EditTaskComment)
	registerProjectUnary(mux, opts, projectv1.ProjectService_DeleteTaskComment_FullMethodName, s.DeleteTaskComment)
	registerProjectUnary(mux, opts, projectv1.ProjectService_ListTaskComments_FullMethodName, s.ListTaskComments)
	registerProjectUnary(mux, opts, projectv1.ProjectService_ListTaskActivity_FullMethodName, s.ListTaskActivity)
	return mux
}

func registerProjectUnary[Req, Res any](mux *http.ServeMux, opts []connect.HandlerOption, procedure string, fn func(context.Context, *Req) (*Res, error)) {
	mux.Handle(procedure, connect.NewUnaryHandler(procedure, func(ctx context.Context, req *connect.Request[Req]) (*connect.Response[Res], error) {
		res, err := fn(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
}
