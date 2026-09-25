//go:build js && wasm

package main

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/projectui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	projectclient "github.com/monstercameron/human-capital-management-suite/tools/uxqual/projectclient"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func parseProjectRoute(pathname, rawQuery string) (projectclient.State, bool, error) {
	if pathname != projectclient.ProjectsPath && pathname != projectclient.ProjectPath {
		return projectclient.State{}, false, nil
	}
	state, err := projectclient.ParseState(pathname, rawQuery)
	return state, true, err
}

// loadProjectPage reads every project surface from the authorized Project
// service. URL values are selectors only; each call is independently
// authorized by the server.
func loadProjectPage(ctx context.Context, cfg journeyclient.Config, service projectv1.ProjectServiceClient, pathname, rawQuery string) (projectui.Model, *projectui.DetailModel, []productui.ProjectSummaryProjection, productui.ProjectProjectionState, productui.ProjectProjectionState, productui.ProjectProjectionState, string, error) {
	state, err := projectclient.ParseState(pathname, rawQuery)
	if err != nil {
		return projectui.Model{}, nil, nil, productui.ProjectProjectionFailed, productui.ProjectProjectionFailed, productui.ProjectProjectionFailed, "", err
	}
	if state.Route == projectclient.RouteProjects {
		response, err := service.ListProjects(chatRPCContext(ctx, cfg), &projectv1.ListProjectsRequest{Page: &commonv1.PageRequest{PageSize: 100, Cursor: state.Cursor}})
		if err != nil {
			state := projectProjectionErrorState(err)
			return projectui.Model{}, nil, nil, state, productui.ProjectProjectionUnavailable, state, "", nil
		}
		projects := make([]productui.ProjectSummaryProjection, 0, len(response.GetProjects()))
		for _, project := range response.GetProjects() {
			if project == nil || strings.TrimSpace(project.GetProjectId()) == "" {
				continue
			}
			summary := productui.ProjectSummaryProjection{
				ID: project.GetProjectId(), Name: project.GetName(), OwnerID: project.GetOwnerId(),
				Status: projectLifecycleLabel(project.GetLifecycle()),
			}
			if len(projects) < projectProgressLimit {
				if progress, ok := projectProgressFor(cfg, service, summary.ID); ok {
					summary.TaskCount, summary.DoneCount, summary.TaskCountKnown = progress.total, progress.done, progress.known
				} else {
					summary.ProgressPending = true
				}
			}
			projects = append(projects, summary)
		}
		home := projectui.Model{}
		if state.Tab == projectclient.TabTickets {
			list := loadProjectTickets(ctx, cfg, service, state, response.GetProjects())
			home.HomeTab, home.Tickets = projectclient.TabTickets, &list
		}
		return home, nil, projects, productui.ProjectProjectionReady, productui.ProjectProjectionUnavailable, productui.ProjectProjectionUnavailable, "", nil
	}
	return loadSelectedProject(ctx, cfg, service, state)
}

func loadSelectedProject(ctx context.Context, cfg journeyclient.Config, service projectv1.ProjectServiceClient, state projectclient.State) (projectui.Model, *projectui.DetailModel, []productui.ProjectSummaryProjection, productui.ProjectProjectionState, productui.ProjectProjectionState, productui.ProjectProjectionState, string, error) {
	failed := func(err error) (projectui.Model, *projectui.DetailModel, []productui.ProjectSummaryProjection, productui.ProjectProjectionState, productui.ProjectProjectionState, productui.ProjectProjectionState, string, error) {
		projectionState := projectProjectionErrorState(err)
		return projectui.Model{}, nil, nil, productui.ProjectProjectionUnavailable, projectionState, projectionState, "", nil
	}
	projectResponse, err := service.GetProject(chatRPCContext(ctx, cfg), &projectv1.GetProjectRequest{ProjectId: state.ProjectID})
	if err != nil {
		return failed(err)
	}
	project := projectResponse.GetProject()
	if project == nil || project.GetProjectId() != state.ProjectID {
		return failed(errors.New("projectclient: project response identity mismatch"))
	}
	viewResponse, err := service.ListBoardViews(chatRPCContext(ctx, cfg), &projectv1.ListBoardViewsRequest{
		ProjectId: state.ProjectID, Page: &commonv1.PageRequest{PageSize: 100},
	})
	if err != nil {
		return failed(err)
	}
	selectedViewID := state.BoardViewID
	if selectedViewID == "" {
		// "default" is selected only when it is present in the authorized
		// view response; the client never fabricates a saved view.
		for _, candidate := range viewResponse.GetViews() {
			if candidate != nil && candidate.GetViewId() == "default" && candidate.GetProjectId() == state.ProjectID {
				selectedViewID = candidate.GetViewId()
				break
			}
		}
	}
	if selectedViewID == "" {
		return projectui.Model{}, nil, nil, productui.ProjectProjectionUnavailable, productui.ProjectProjectionUnavailable, productui.ProjectProjectionUnavailable, "", nil
	}
	var selectedView *projectv1.BoardView
	for _, candidate := range viewResponse.GetViews() {
		if candidate != nil && candidate.GetViewId() == selectedViewID && candidate.GetProjectId() == state.ProjectID {
			selectedView = candidate
			break
		}
	}
	if selectedView == nil {
		return projectui.Model{}, nil, nil, productui.ProjectProjectionUnavailable, productui.ProjectProjectionRestricted, productui.ProjectProjectionRestricted, "", nil
	}
	if project.GetLifecycle() == projectv1.ProjectLifecycle_PROJECT_LIFECYCLE_ARCHIVED {
		return projectui.Model{}, nil, nil, productui.ProjectProjectionUnavailable, productui.ProjectProjectionUnavailable, productui.ProjectProjectionUnavailable, "", nil
	}
	configurationResponse, err := service.GetWorkflowConfiguration(chatRPCContext(ctx, cfg), &projectv1.GetWorkflowConfigurationRequest{ProjectId: state.ProjectID})
	if err != nil {
		return failed(err)
	}
	configuration := configurationResponse.GetConfiguration()
	if configuration == nil || configuration.GetProjectId() != state.ProjectID {
		return failed(errors.New("projectclient: workflow response identity mismatch"))
	}
	boardResponse, err := service.GetBoard(chatRPCContext(ctx, cfg), &projectv1.GetBoardRequest{
		ProjectId: state.ProjectID, ViewId: selectedViewID,
		Page: &commonv1.PageRequest{PageSize: 100, Cursor: state.Cursor},
	})
	if err != nil {
		return failed(err)
	}
	mode := projectui.ViewBoard
	switch state.View {
	case projectclient.ViewList:
		mode = projectui.ViewList
	case projectclient.ViewTask:
		mode = projectui.ViewTask
	}
	names, photos := projectDirectory(ctx, cfg)
	options := projectclient.ProjectionOptions{
		ExpectedProjectID: state.ProjectID, ExpectedViewID: selectedViewID, Mode: mode,
		Statuses: configuration.GetStatuses(), Fields: configuration.GetFields(),
		AssigneeNames: projectAssigneeNames(boardResponse.GetTasks(), names),
		TaskHref: func(taskID string) string {
			linkState := state
			linkState.BoardViewID, linkState.TaskID = selectedViewID, taskID
			return projectclient.CanonicalHref(linkState)
		},
		LinkHref: projectTaskReferenceHref,
	}
	pendingMoves, moveConflicts, failedActions := projectActionProjectionState(cfg.Tenant, cfg.Subject, state.ProjectID)
	options.PendingTaskIDs, options.Conflicts, options.FailedActions = pendingMoves, moveConflicts, failedActions
	if project.GetLifecycle() == projectv1.ProjectLifecycle_PROJECT_LIFECYCLE_ACTIVE {
		options.CanMoveStatus = func(task *projectv1.ProjectTask) bool {
			if task == nil || boardResponse.GetWorkflowRevision() == 0 {
				return false
			}
			for _, taskStatus := range configuration.GetStatuses() {
				if taskStatus == nil || taskStatus.GetStatusId() != task.GetStatusId() {
					continue
				}
				for _, target := range taskStatus.GetAllowedNextStatusIds() {
					if target != "" && target != task.GetStatusId() {
						return true
					}
				}
			}
			return false
		}
		// Moving a card to another swimlane edits the field the lanes group
		// by; every grouping the board supports can be written.
		if laneKind := projectLaneKind(selectedView.GetSwimlaneGrouping()); laneKind != "" {
			options.CanMoveLane = func(task *projectv1.ProjectTask) bool {
				return task != nil && boardResponse.GetWorkflowRevision() != 0 && (laneKind != projectui.LaneKindField || selectedView.GetSwimlaneFieldId() != "")
			}
		}
	}
	board, err := projectclient.BoardModel(boardResponse, options)
	if err != nil {
		return failed(err)
	}
	board.ViewID = selectedView.GetViewId()
	board.ViewRevision = selectedView.GetRevision()
	board.SwimlaneGrouping = selectedView.GetSwimlaneGrouping().String()
	board.SwimlaneFieldID = selectedView.GetSwimlaneFieldId()
	board.ProjectID, board.ProjectName = state.ProjectID, project.GetName()
	if state.View == projectclient.ViewList {
		// Each list header links to the list sorted by that column; the
		// current column's link reverses the direction.
		board.ListSort = state.Sort
		board.SortHrefs = map[string]string{}
		// With no sort the list is in workflow order, which reads as
		// Status ascending; its header link reverses that.
		effective := state.Sort
		if effective == "" {
			effective = "status"
		}
		for _, key := range projectclient.SortKeys {
			next := state
			next.TaskID, next.Cursor, next.Sort = "", "", key
			if effective == key {
				next.Sort = "-" + key
			}
			board.SortHrefs[key] = projectclient.CanonicalHref(next)
		}
	}
	board.LaneKind = projectLaneKind(selectedView.GetSwimlaneGrouping())
	if board.LaneKind == projectui.LaneKindField {
		board.LaneFieldID = selectedView.GetSwimlaneFieldId()
	}
	setProjectLaneConfig(board.LaneKind, board.LaneFieldID)
	for _, field := range configuration.GetFields() {
		if field != nil && field.GetType() == projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_ENUM && field.GetFieldId() != "" {
			label := field.GetName()
			if label == "" {
				label = field.GetFieldId()
			}
			board.EnumFields = append(board.EnumFields, projectui.Status{ID: field.GetFieldId(), Label: label})
		}
	}
	// Members are the assignee choices for a new task; a failed read only
	// leaves the create form with "Unassigned".
	if response, memberErr := service.ListProjectMembers(chatRPCContext(ctx, cfg), &projectv1.ListProjectMembersRequest{ProjectId: state.ProjectID, PageSize: 100}); memberErr == nil {
		board.Members = projectclient.EnrichDetail(projectui.DetailModel{}, projectclient.DetailInputs{Task: &projectv1.ProjectTask{}, Members: response.GetMembers(), Names: names, Photos: photos}).Members
	}
	collapsed := projectCollapsedLanes(projectui.LaneStorageKey(board.ProjectID, board.ViewID))
	for index := range board.Lanes {
		// A remembered choice wins; otherwise an empty lane starts closed so
		// it does not spend a full row on nothing.
		if state, remembered := collapsed[board.Lanes[index].ID]; remembered {
			board.Lanes[index].Collapsed = state
		} else {
			board.Lanes[index].Collapsed = board.Lanes[index].Count == 0
		}
		// A lane accepts moves when its grouping field is writable, even
		// while it is empty; "unassigned" and "unset" remain targets too.
		if options.CanMoveLane != nil && (board.LaneKind != projectui.LaneKindField || board.Lanes[index].ID != "unset") {
			board.Lanes[index].MayEdit = true
		}
	}
	// An in-flight move draws the card where it is going; a failed one
	// drops the override, which is the rollback.
	targets := projectMoveTargetsFor(cfg.Tenant, cfg.Subject, state.ProjectID)
	assignees := map[string]string{}
	for _, task := range boardResponse.GetTasks() {
		if task != nil {
			assignees[task.GetTaskId()] = task.GetAssigneeId()
		}
	}
	for index := range board.Cards {
		card := &board.Cards[index]
		card.AssigneePhoto = photos[assignees[card.ID]]
		if target, ok := targets[card.ID]; ok {
			if target.Status != "" {
				card.StatusID = target.Status
			}
			if target.Lane != "" {
				card.LaneID = target.Lane
			}
		}
	}
	// GetBoard's task rows do not carry the planning fields yet; the
	// project's ListTasks read (cached with the home progress) does.
	if progress, ok := projectProgressFor(cfg, service, state.ProjectID); ok && progress.tasks != nil {
		byID := map[string]*projectv1.ProjectTask{}
		for _, task := range progress.tasks {
			byID[task.GetTaskId()] = task
		}
		for index := range board.Cards {
			card := &board.Cards[index]
			if task := byID[card.ID]; task != nil {
				if len(card.Labels) == 0 {
					card.Labels = append([]string(nil), task.GetLabels()...)
				}
				if card.StoryPoints == 0 {
					card.StoryPoints = int(task.GetStoryPoints())
				}
			}
		}
	}
	cardIDs := make([]string, 0, len(board.Cards))
	for index := range board.Cards {
		cardIDs = append(cardIDs, board.Cards[index].ID)
		if projectTaskHasWorkflow(cfg, service, state.ProjectID, board.Cards[index].ID) {
			board.Cards[index].Workflows = []string{projectWorkflowPromotion}
		}
	}
	board.Workflows = projectBoardWorkflows(cfg, service, state.ProjectID, cardIDs)
	if state.View != projectclient.ViewTask {
		applyProjectFilter(&board, state, assignees, photos)
	}
	if state.View == projectclient.ViewList {
		board.ListPager = projectListPager(board, state)
	}
	if next := boardResponse.GetPage().GetNextCursor(); next != "" {
		nextState := state
		nextState.BoardViewID = selectedViewID
		nextState.Cursor = next
		board.Page.HasNext = true
		board.Page.NextHref = projectclient.CanonicalHref(nextState)
	}
	boardState := productui.ProjectProjectionReady
	if project.GetLifecycle() == projectv1.ProjectLifecycle_PROJECT_LIFECYCLE_SUSPENDED {
		for index := range board.Cards {
			board.Cards[index].CanMoveStatus = false
			board.Cards[index].CanMoveLane = false
		}
	}
	var detail *projectui.DetailModel
	detailState := productui.ProjectProjectionUnavailable
	if state.TaskID != "" {
		taskResponse, taskErr := service.GetTask(chatRPCContext(ctx, cfg), &projectv1.GetTaskRequest{ProjectId: state.ProjectID, TaskId: state.TaskID})
		if taskErr != nil {
			detailState = projectProjectionErrorState(taskErr)
		} else {
			linkResponse, linkErr := service.ListTaskLinks(chatRPCContext(ctx, cfg), &projectv1.ListTaskLinksRequest{
				ProjectId: state.ProjectID, TaskId: state.TaskID, Page: &commonv1.PageRequest{PageSize: 100},
			})
			// Linked items are one section of the task; a failed link read
			// shows that section empty instead of hiding the whole task.
			if linkErr != nil {
				linkResponse = nil
			}
			{
				detailOptions := options
				detailOptions.ExpectedTaskID = state.TaskID
				projectedDetail, projectionErr := projectclient.TaskDetailModel(taskResponse, linkResponse, detailOptions)
				if projectionErr != nil {
					return failed(projectionErr)
				}
				callCtx := chatRPCContext(ctx, cfg)
				// Comments, activity and members are best-effort extras: a
				// failed read leaves that section empty instead of hiding
				// the task.
				var comments []*projectv1.ProjectTaskComment
				if response, commentErr := service.ListTaskComments(callCtx, &projectv1.ListTaskCommentsRequest{ProjectId: state.ProjectID, TaskId: state.TaskID, PageSize: 100}); commentErr == nil {
					comments = response.GetComments()
				}
				var activity []*projectv1.ProjectTaskActivity
				if mode == projectui.ViewTask {
					if response, activityErr := service.ListTaskActivity(callCtx, &projectv1.ListTaskActivityRequest{ProjectId: state.ProjectID, TaskId: state.TaskID, PageSize: 50}); activityErr == nil {
						activity = response.GetEntries()
					}
				}
				var members []*projectv1.ProjectMember
				if response, memberErr := service.ListProjectMembers(callCtx, &projectv1.ListProjectMembersRequest{ProjectId: state.ProjectID, PageSize: 100}); memberErr == nil {
					members = response.GetMembers()
				}
				projectedDetail = projectclient.EnrichDetail(projectedDetail, projectclient.DetailInputs{
					Task: taskResponse.GetTask(), ProjectID: state.ProjectID, ProjectName: project.GetName(),
					WorkflowRevision: configuration.GetRevision(), Statuses: configuration.GetStatuses(), TaskTypes: configuration.GetTaskTypes(),
					Comments: comments, Activity: activity, Members: members, Names: names, Photos: photos, Viewer: cfg.Subject,
					CanEdit: project.GetLifecycle() == projectv1.ProjectLifecycle_PROJECT_LIFECYCLE_ACTIVE,
				})
				if target, ok := targets[state.TaskID]; ok && target.Status != "" {
					projectedDetail.StatusID = target.Status
				}
				projectedDetail.FieldStates, projectedDetail.FieldErrors = projectFieldStateFor(cfg.Tenant, cfg.Subject, state.ProjectID, state.TaskID)
				if pendingMoves[state.TaskID] {
					projectedDetail.FieldStates["status"] = "saving"
				} else if moveConflicts[state.TaskID] != "" {
					projectedDetail.FieldStates["status"] = "error"
				}
				// Labels other tasks on this board already use are offered
				// as suggestions.
				used := map[string]bool{}
				for _, label := range projectedDetail.Labels {
					used[strings.ToLower(label)] = true
				}
				for _, task := range boardResponse.GetTasks() {
					for _, label := range task.GetLabels() {
						if key := strings.ToLower(label); !used[key] {
							used[key] = true
							projectedDetail.LabelSuggestions = append(projectedDetail.LabelSuggestions, label)
						}
					}
				}
				sort.Strings(projectedDetail.LabelSuggestions)
				projectedDetail.Workflows, projectedDetail.WorkflowOptions, projectedDetail.WorkflowOptionsLoading = projectWorkflowDetail(ctx, cfg, state.ProjectID, state.TaskID, linkResponse)
				detail = &projectedDetail
				detailState = productui.ProjectProjectionReady
			}
		}
	}
	board.Share = &projectui.Share{Targets: projectShareTargetsFor(ctx, cfg)}
	return board, detail, nil, productui.ProjectProjectionUnavailable, boardState, detailState, selectedViewID, nil
}

func projectProjectionErrorState(err error) productui.ProjectProjectionState {
	switch status.Code(err) {
	case codes.PermissionDenied, codes.NotFound:
		return productui.ProjectProjectionRestricted
	case codes.Unavailable, codes.DeadlineExceeded:
		return productui.ProjectProjectionUnavailable
	default:
		return productui.ProjectProjectionFailed
	}
}

func projectLifecycleLabel(value projectv1.ProjectLifecycle) string {
	switch value {
	case projectv1.ProjectLifecycle_PROJECT_LIFECYCLE_ACTIVE:
		return "Active"
	case projectv1.ProjectLifecycle_PROJECT_LIFECYCLE_SUSPENDED:
		return "Suspended"
	case projectv1.ProjectLifecycle_PROJECT_LIFECYCLE_ARCHIVED:
		return "Archived"
	default:
		return ""
	}
}

func projectTaskReferenceHref(reference *projectv1.TaskLinkReference) string {
	if reference == nil {
		return ""
	}
	if conversation := reference.GetChatConversation(); conversation != nil && conversation.GetConversationId() != "" {
		return "/workspace/app/chat#channel=" + url.QueryEscape(conversation.GetConversationId())
	}
	if post := reference.GetChatPost(); post != nil && post.GetConversationId() != "" {
		return "/workspace/app/chat#channel=" + url.QueryEscape(post.GetConversationId())
	}
	if document := reference.GetDeployedDocument(); document != nil && document.GetDocumentId() != "" {
		query := url.Values{"document": []string{document.GetDocumentId()}}
		return "/workspace/app/docs?" + query.Encode()
	}
	return ""
}

// projectDirectoryCache holds the governed worker directory for this
// session: names and portraits by subject ID. It is read once and reused,
// because every board, modal and task page render resolves people.
var projectDirectoryCache struct {
	sync.Mutex
	names, photos map[string]string
	loaded        time.Time
}

// projectDirectory resolves people the way chat does (ListChatDirectory),
// refreshed at most every five minutes. A failed read yields empty maps and
// names fall back to ones derived from the subject, never raw IDs.
func projectDirectory(ctx context.Context, cfg journeyclient.Config) (map[string]string, map[string]string) {
	projectDirectoryCache.Lock()
	defer projectDirectoryCache.Unlock()
	if projectDirectoryCache.names != nil && time.Since(projectDirectoryCache.loaded) < 5*time.Minute {
		return projectDirectoryCache.names, projectDirectoryCache.photos
	}
	names, photos := map[string]string{}, map[string]string{}
	if chatWorkers != nil {
		if response, err := chatWorkers.ListChatDirectory(chatRPCContext(ctx, cfg), &journeyv1.ListChatDirectoryRequest{}); err == nil {
			names = chatDirectoryFromWorkers(response.GetWorkers())
			for _, worker := range response.GetWorkers() {
				if worker == nil {
					continue
				}
				if subject, photo := chatWorkerSubject(worker), strings.TrimSpace(worker.GetProfilePhotoUrl()); subject != "" && photo != "" {
					photos[subject] = photo
				}
			}
			projectDirectoryCache.names, projectDirectoryCache.photos, projectDirectoryCache.loaded = names, photos, time.Now()
		}
	}
	return names, photos
}

// projectAssigneeNames names every assignee on the page through the
// directory, so cards and assignee swimlanes show people, not IDs.
func projectAssigneeNames(tasks []*projectv1.ProjectTask, names map[string]string) map[string]string {
	out := map[string]string{}
	for _, task := range tasks {
		if task != nil && task.GetAssigneeId() != "" {
			out[task.GetAssigneeId()] = projectclient.PersonName(task.GetAssigneeId(), names)
		}
	}
	return out
}

func projectLaneKind(grouping projectv1.BoardSwimlaneGrouping) string {
	switch grouping {
	case projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_ASSIGNEE:
		return projectui.LaneKindAssignee
	case projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_PRIORITY:
		return projectui.LaneKindPriority
	case projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_ENUM_FIELD:
		return projectui.LaneKindField
	default:
		return ""
	}
}

// projectMoveTargetsFor returns the in-flight move targets for one project.
func projectMoveTargetsFor(tenantID, subjectID, projectID string) map[string]projectMoveTarget {
	projectActionStateMu.Lock()
	defer projectActionStateMu.Unlock()
	prefix := tenantID + "\x00" + subjectID + "\x00" + projectID + "\x00"
	out := map[string]projectMoveTarget{}
	for key, target := range projectPendingTargets {
		if strings.HasPrefix(key, prefix) {
			out[strings.TrimPrefix(key, prefix)] = target
		}
	}
	return out
}
