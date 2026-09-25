//go:build js && wasm

package main

import (
	"context"
	"net/url"
	"sort"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/google/uuid"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/projectui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	projectclient "github.com/monstercameron/human-capital-management-suite/tools/uxqual/projectclient"
)

// Tickets linked to workflow items. The task link (and whether the viewer
// may see its target) comes from the Project service. What a row says about
// the workflow comes from the journeys the viewer can already read in My
// Work (the same Journey service reads, the same visibility): ListJourneys,
// then InspectJourney for each executed run's human steps. That index is
// read in the background, cached briefly, and the page revalidates when it
// lands; a link the index does not know shows as a neutral row.

const (
	projectWorkflowTTL       = 90 * time.Second
	projectTaskLinkTTL       = 2 * time.Minute
	projectTaskLinkFanout    = 6
	projectWorkflowPromotion = "promotion"
)

type projectWorkflowEntry struct {
	journeyID, person, from, to, stage, effective string
	open                                          bool
}

var (
	projectWorkflowMu      sync.Mutex
	projectWorkflowIndex   map[string]projectWorkflowEntry
	projectWorkflowAt      time.Time
	projectWorkflowKey     string
	projectWorkflowLoading bool

	projectTaskLinkMu      sync.Mutex
	projectTaskLinks       = map[string]projectTaskLinkEntry{}
	projectTaskLinkQueue   = map[string]bool{}
	projectTaskLinkPending int
	projectTaskLinkSlots   = make(chan struct{}, projectTaskLinkFanout)

	// Optimistic link changes, by task key: the journey or work item being
	// linked and link IDs being removed.
	projectWorkflowAdding   = map[string]string{}
	projectWorkflowRemoving = map[string]map[string]bool{}

	projectWorkflowInstalled bool
)

// projectTaskLinkEntry is what a task's links say about workflows: the
// journeys it links and whether it links any workflow item at all.
type projectTaskLinkEntry struct {
	workflows bool
	journeys  []string
	at        time.Time
}

// projectWorkStatus folds a work item status token into open, active, done
// or closed.
func projectWorkStatus(token string) (string, bool) {
	switch strings.ToUpper(token) {
	case "CREATED", "ROUTED", "OFFERED", "READY", "PENDING":
		return "open", true
	case "CLAIMED", "IN_PROGRESS", "STARTED":
		return "active", true
	case "COMPLETED", "APPROVED", "DONE":
		return "done", false
	}
	return "closed", false
}

func projectWorkflowTone(status string) string {
	switch status {
	case "open":
		return "todo"
	case "active":
		return "active"
	case "done":
		return "done"
	}
	return "cancelled"
}

// projectStageTone and projectStageGroup read a journey stage enum name.
func projectStageGroup(stage string) string {
	switch stage {
	case "JOURNEY_STAGE_COMPLETED", "JOURNEY_STAGE_REJECTED", "JOURNEY_STAGE_FAILED":
		return "done"
	case "JOURNEY_STAGE_BLOCKED":
		return "blocked"
	}
	return "active"
}

func projectStageTone(stage string) string {
	switch projectStageGroup(stage) {
	case "done":
		return "done"
	case "blocked":
		return "blocked"
	}
	return "active"
}

func projectPlacement(p *journeyv1.Placement) string {
	if p == nil {
		return ""
	}
	parts := []string{}
	for _, v := range []string{p.GetJobCode(), p.GetGrade()} {
		if v = strings.TrimSpace(v); v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, " · ")
}

// projectWorkflowIndexFor returns the cached journeys the viewer can read,
// starting a read when they are missing or stale; the bool says whether the
// index is ready.
func projectWorkflowIndexFor(cfg journeyclient.Config) (map[string]projectWorkflowEntry, bool) {
	key := cfg.Tenant + "\x00" + cfg.Subject
	projectWorkflowMu.Lock()
	defer projectWorkflowMu.Unlock()
	fresh := key == projectWorkflowKey && projectWorkflowIndex != nil && time.Since(projectWorkflowAt) < projectWorkflowTTL
	if !fresh && !projectWorkflowLoading && chatWorkers != nil {
		projectWorkflowLoading = true
		go readProjectWorkflowIndex(cfg, key)
	}
	if key != projectWorkflowKey {
		return nil, false
	}
	return projectWorkflowIndex, projectWorkflowIndex != nil
}

// readProjectWorkflowIndex reads the journeys (Promotion runs) the viewer
// can see: the same ListJourneys My Work and Journeys read, so the same
// visibility.
func readProjectWorkflowIndex(cfg journeyclient.Config, key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	index := map[string]projectWorkflowEntry{}
	if response, err := chatWorkers.ListJourneys(chatRPCContext(ctx, cfg), &journeyv1.ListJourneysRequest{}); err == nil {
		for _, journey := range response.GetJourneys() {
			if journey == nil || journey.GetIntentId() == "" || strings.TrimSpace(journey.GetWorkerName()) == "" {
				continue
			}
			stage := journey.GetStage().String()
			index[journey.GetIntentId()] = projectWorkflowEntry{
				journeyID: journey.GetIntentId(), person: strings.TrimSpace(journey.GetWorkerName()),
				from: projectPlacement(journey.GetCurrent()), to: projectPlacement(journey.GetTarget()),
				stage: stage, effective: journey.GetEffectiveDate(), open: projectStageGroup(stage) != "done",
			}
		}
	}
	projectWorkflowMu.Lock()
	projectWorkflowIndex, projectWorkflowAt, projectWorkflowKey, projectWorkflowLoading = index, time.Now(), key, false
	projectWorkflowMu.Unlock()
	projectRevalidateProjects()
}

// projectRevalidateProjects reruns the route loader while a projects page is
// on screen.
func projectRevalidateProjects() {
	if path := currentPath(); projectRevalidate != nil && (path == projectclient.ProjectsPath || path == projectclient.ProjectPath) {
		projectRevalidate()
	}
}

// projectJourneyHref is a journey's address.
func projectJourneyHref(journeyID string) string {
	return "/workspace/app/journeys?journey=" + url.QueryEscape(journeyID)
}

// projectJourneyRow is one linked journey as a Workflows row; a journey the
// index does not hold is read directly (InspectJourney as the viewer, the
// chat embed's read), and one the viewer cannot read is restricted.
func projectJourneyRow(ctx context.Context, cfg journeyclient.Config, index map[string]projectWorkflowEntry, ready bool, linkID, journeyID string) projectui.WorkflowLink {
	row := projectui.WorkflowLink{LinkID: linkID, JourneyID: journeyID, Workflow: projectWorkflowPromotion}
	entry, known := index[journeyID]
	if !known && ready && chatWorkers != nil {
		preview := readChatJourney(ctx, cfg, chatWorkers, journeyID)
		if !preview.Readable {
			row.State = projectui.ReferenceRestricted
			return row
		}
		entry = projectWorkflowEntry{journeyID: journeyID, person: preview.Worker, from: preview.From, to: preview.To, stage: preview.Stage, effective: preview.Effective, open: projectStageGroup(preview.Stage) != "done"}
		known = true
	}
	if !known {
		row.State = projectui.ReferenceLoading
		return row
	}
	row.State, row.Person, row.From, row.To = projectui.ReferenceReady, entry.person, entry.from, entry.to
	row.Status, row.StatusTone, row.Due, row.Href = entry.stage, projectStageTone(entry.stage), entry.effective, projectJourneyHref(journeyID)
	return row
}

// projectWorkflowDetail turns a task's resolved links into the Workflows
// rows and the picker options.
func projectWorkflowDetail(ctx context.Context, cfg journeyclient.Config, projectID, taskID string, links *projectv1.ListTaskLinksResponse) ([]projectui.WorkflowLink, []projectui.WorkflowOption, bool) {
	index, ready := projectWorkflowIndexFor(cfg)
	key := projectActionKey(cfg.Tenant, cfg.Subject, projectID, taskID)
	projectWorkflowMu.Lock()
	adding := projectWorkflowAdding[key]
	removing := projectWorkflowRemoving[key]
	projectWorkflowMu.Unlock()
	rows := []projectui.WorkflowLink{}
	linked := map[string]bool{}
	for _, link := range links.GetLinks() {
		if removing[link.GetLinkId()] {
			continue
		}
		available := link.GetState() == projectv1.TaskLinkResolutionState_TASK_LINK_RESOLUTION_STATE_AVAILABLE
		lockedState := projectui.ReferenceRestricted
		if link.GetState() == projectv1.TaskLinkResolutionState_TASK_LINK_RESOLUTION_STATE_UNSPECIFIED {
			lockedState = projectui.ReferenceUnavailable
		}
		switch {
		case link.GetReference().GetJourney() != nil:
			journeyID := link.GetReference().GetJourney().GetIntentId()
			linked[journeyID] = true
			if !available {
				rows = append(rows, projectui.WorkflowLink{LinkID: link.GetLinkId(), JourneyID: journeyID, State: lockedState})
				continue
			}
			rows = append(rows, projectJourneyRow(ctx, cfg, index, ready, link.GetLinkId(), journeyID))
		case link.GetReference().GetWorkItem() != nil:
			item := link.GetReference().GetWorkItem()
			row := projectui.WorkflowLink{LinkID: link.GetLinkId(), WorkItemID: item.GetWorkItemId(), Workflow: projectWorkflowPromotion, State: lockedState}
			if available {
				status, _ := projectWorkStatus(link.GetPreview().GetSafeWorkItemStatus())
				row.State, row.Step, row.Status, row.StatusTone = projectui.ReferenceReady, "approval", "work:"+status, projectWorkflowTone(status)
			}
			rows = append(rows, row)
		}
	}
	if adding != "" && !linked[adding] {
		rows = append(rows, projectui.WorkflowLink{JourneyID: adding, State: projectui.ReferenceLoading})
		linked[adding] = true
	}
	options := []projectui.WorkflowOption{}
	for _, entry := range index {
		group := projectStageGroup(entry.stage)
		options = append(options, projectui.WorkflowOption{
			JourneyID: entry.journeyID, Workflow: projectWorkflowPromotion, Person: entry.person, From: entry.from, To: entry.to,
			Status: entry.stage, StatusTone: projectStageTone(entry.stage), Due: entry.effective, Open: entry.open, Linked: linked[entry.journeyID], Group: "group:" + group,
		})
	}
	rank := map[string]int{"group:active": 0, "group:blocked": 1, "group:done": 2}
	sort.SliceStable(options, func(i, j int) bool {
		if rank[options[i].Group] != rank[options[j].Group] {
			return rank[options[i].Group] < rank[options[j].Group]
		}
		return options[i].Person < options[j].Person
	})
	return rows, options, !ready
}

// projectTaskWorkflows reports a task's linked journeys and whether it
// links any workflow item, reading ListTaskLinks in the background for tasks
// not yet cached.
func projectTaskWorkflows(cfg journeyclient.Config, service projectv1.ProjectServiceClient, projectID, taskID string) projectTaskLinkEntry {
	key := projectActionKey(cfg.Tenant, cfg.Subject, projectID, taskID)
	projectTaskLinkMu.Lock()
	defer projectTaskLinkMu.Unlock()
	entry, ok := projectTaskLinks[key]
	if (!ok || time.Since(entry.at) > projectTaskLinkTTL) && !projectTaskLinkQueue[key] {
		projectTaskLinkQueue[key] = true
		projectTaskLinkPending++
		go readProjectTaskLinks(cfg, service, projectID, taskID, key)
	}
	return entry
}

// projectTaskHasWorkflow is projectTaskWorkflows as a yes or no.
func projectTaskHasWorkflow(cfg journeyclient.Config, service projectv1.ProjectServiceClient, projectID, taskID string) bool {
	return projectTaskWorkflows(cfg, service, projectID, taskID).workflows
}

func readProjectTaskLinks(cfg journeyclient.Config, service projectv1.ProjectServiceClient, projectID, taskID, key string) {
	projectTaskLinkSlots <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	response, err := service.ListTaskLinks(chatRPCContext(ctx, cfg), &projectv1.ListTaskLinksRequest{ProjectId: projectID, TaskId: taskID})
	cancel()
	<-projectTaskLinkSlots
	entry := projectTaskLinkEntry{at: time.Now()}
	if err == nil {
		for _, link := range response.GetLinks() {
			switch {
			case link.GetReference().GetJourney() != nil:
				entry.workflows = true
				entry.journeys = append(entry.journeys, link.GetReference().GetJourney().GetIntentId())
			case link.GetReference().GetWorkItem() != nil:
				entry.workflows = true
			}
		}
	}
	projectTaskLinkMu.Lock()
	previous := projectTaskLinks[key]
	projectTaskLinks[key] = entry
	delete(projectTaskLinkQueue, key)
	projectTaskLinkPending--
	settled := projectTaskLinkPending == 0
	changed := previous.at.IsZero() || previous.workflows != entry.workflows || len(previous.journeys) != len(entry.journeys)
	projectTaskLinkMu.Unlock()
	if settled && changed {
		projectRevalidateProjects()
	}
}

func projectTaskLinksForget(cfg journeyclient.Config, projectID, taskID string) {
	projectTaskLinkMu.Lock()
	delete(projectTaskLinks, projectActionKey(cfg.Tenant, cfg.Subject, projectID, taskID))
	projectTaskLinkMu.Unlock()
}

// projectBoardWorkflows lists every journey linked from the board's tasks,
// for the board's Workflows panel.
func projectBoardWorkflows(cfg journeyclient.Config, service projectv1.ProjectServiceClient, projectID string, taskIDs []string) []projectui.WorkflowLink {
	index, ready := projectWorkflowIndexFor(cfg)
	seen := map[string]int{}
	rows := []projectui.WorkflowLink{}
	for _, taskID := range taskIDs {
		for _, journeyID := range projectTaskWorkflows(cfg, service, projectID, taskID).journeys {
			if at, ok := seen[journeyID]; ok {
				rows[at].LinkID = ""
				continue
			}
			entry, known := index[journeyID]
			row := projectui.WorkflowLink{JourneyID: journeyID, Workflow: projectWorkflowPromotion, State: projectui.ReferenceLoading}
			if known {
				row.State, row.Person, row.From, row.To = projectui.ReferenceReady, entry.person, entry.from, entry.to
				row.Status, row.StatusTone, row.Due, row.Href = entry.stage, projectStageTone(entry.stage), entry.effective, projectJourneyHref(journeyID)
			} else if ready {
				row.State = projectui.ReferenceRestricted
			}
			seen[journeyID] = len(rows)
			rows = append(rows, row)
		}
	}
	return rows
}

// bindProjectWorkflows installs the picker and link/unlink listeners.
func bindProjectWorkflows(cfg journeyclient.Config, service projectv1.ProjectServiceClient, revalidate func()) {
	if projectWorkflowInstalled {
		return
	}
	document := js.Global().Get("document")
	if !document.Truthy() {
		return
	}
	projectWorkflowInstalled = true
	document.Call("addEventListener", "click", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		target := args[0].Get("target")
		if linker := projectClosest(target, `[data-projectui-action="open-ticket-linker"]`); linker.Truthy() {
			openProjectTicketLinker(cfg, service, linker)
			return nil
		}
		if opener := projectClosest(target, `[data-projectui-action="open-workflow-picker"]`); opener.Truthy() {
			picker := document.Call("getElementById", "projectui-workflow-picker")
			if picker.Truthy() {
				picker.Call("setAttribute", "data-status", "open")
				filterProjectWorkflowPicker(picker)
				func() {
					defer func() { _ = recover() }()
					picker.Call("showModal")
				}()
				if search := picker.Call("querySelector", "#projectui-workflow-search"); search.Truthy() {
					search.Call("focus")
				}
			}
			return nil
		}
		if closer := projectClosest(target, `[data-projectui-action="close-workflow-picker"]`); closer.Truthy() {
			if picker := projectClosest(closer, "dialog"); picker.Truthy() {
				picker.Call("close")
			}
			return nil
		}
		if toggle := projectClosest(target, `[data-projectui-action="workflow-status"]`); toggle.Truthy() {
			picker := projectClosest(toggle, "dialog")
			picker.Call("setAttribute", "data-status", projectAttr(toggle, "data-status"))
			buttons := picker.Call("querySelectorAll", `[data-projectui-action="workflow-status"]`)
			for index := 0; index < buttons.Get("length").Int(); index++ {
				button := buttons.Index(index)
				button.Call("setAttribute", "aria-checked", boolText(button.Equal(toggle)))
			}
			filterProjectWorkflowPicker(picker)
			return nil
		}
		if row := projectClosest(target, `button[data-projectui-action="link-workflow"]`); row.Truthy() && !row.Get("disabled").Bool() {
			control, ok := readProjectTaskControl(row)
			if !ok {
				return nil
			}
			if picker := projectClosest(row, "dialog"); picker.Truthy() {
				picker.Call("close")
			}
			linkProjectWorkflow(cfg, service, revalidate, control, projectAttr(row, "data-journey-id"))
			return nil
		}
		if unlink := projectClosest(target, `[data-projectui-action="unlink-workflow"]`); unlink.Truthy() {
			control, ok := readProjectTaskControl(unlink)
			if ok {
				unlinkProjectWorkflow(cfg, service, revalidate, control, projectAttr(unlink, "data-link-id"))
			}
		}
		return nil
	}))
	document.Call("addEventListener", "input", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			if input := args[0].Get("target"); projectAttr(input, "id") == "projectui-workflow-search" {
				filterProjectWorkflowPicker(projectClosest(input, "dialog"))
			}
		}
		return nil
	}))
}

// filterProjectWorkflowPicker hides rows that miss the search or the status
// choice, and groups left empty.
func filterProjectWorkflowPicker(picker js.Value) {
	if !picker.Truthy() {
		return
	}
	query := ""
	if search := picker.Call("querySelector", "#projectui-workflow-search"); search.Truthy() {
		query = strings.ToLower(strings.TrimSpace(search.Get("value").String()))
	}
	openOnly := projectAttr(picker, "data-status") != "all"
	shown := 0
	groups := picker.Call("querySelectorAll", ".projectui-picker-group")
	for g := 0; g < groups.Get("length").Int(); g++ {
		group := groups.Index(g)
		items := group.Call("querySelectorAll", ".projectui-picker-item")
		visible := 0
		for i := 0; i < items.Get("length").Int(); i++ {
			item := items.Index(i)
			match := (!openOnly || projectAttr(item, "data-open") == "true") && (query == "" || strings.Contains(projectAttr(item, "data-search"), query))
			item.Set("hidden", !match)
			if match {
				visible++
			}
		}
		group.Set("hidden", visible == 0)
		shown += visible
	}
	if empty := picker.Call("querySelector", ".projectui-picker-nomatch"); empty.Truthy() {
		empty.Set("hidden", shown > 0)
	}
}

func linkProjectWorkflow(cfg journeyclient.Config, service projectv1.ProjectServiceClient, revalidate func(), control projectTaskControl, journeyID string) {
	if journeyID == "" {
		return
	}
	key := projectActionKey(cfg.Tenant, cfg.Subject, control.projectID, control.taskID)
	projectWorkflowMu.Lock()
	projectWorkflowAdding[key] = journeyID
	projectWorkflowMu.Unlock()
	setProjectFieldState(key, "workflows", "saving", "")
	if revalidate != nil {
		revalidate()
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := service.AddTaskLink(chatRPCContext(ctx, cfg), &projectv1.AddTaskLinkRequest{
			IdempotencyKey: uuid.NewString(), ProjectId: control.projectID, TaskId: control.taskID,
			Reference:            &projectv1.TaskLinkReference{Target: &projectv1.TaskLinkReference_Journey{Journey: &projectv1.JourneyLink{IntentId: journeyID}}},
			ExpectedTaskRevision: control.taskRevision,
		})
		projectWorkflowMu.Lock()
		delete(projectWorkflowAdding, key)
		projectWorkflowMu.Unlock()
		if err != nil {
			setProjectFieldState(key, "workflows", "error", projectWriteError(err))
		} else {
			setProjectFieldState(key, "workflows", "saved", "")
		}
		projectTaskLinksForget(cfg, control.projectID, control.taskID)
		if revalidate != nil {
			revalidate()
		}
	}()
}

func unlinkProjectWorkflow(cfg journeyclient.Config, service projectv1.ProjectServiceClient, revalidate func(), control projectTaskControl, linkID string) {
	if linkID == "" {
		return
	}
	key := projectActionKey(cfg.Tenant, cfg.Subject, control.projectID, control.taskID)
	projectWorkflowMu.Lock()
	if projectWorkflowRemoving[key] == nil {
		projectWorkflowRemoving[key] = map[string]bool{}
	}
	projectWorkflowRemoving[key][linkID] = true
	projectWorkflowMu.Unlock()
	setProjectFieldState(key, "workflows", "saving", "")
	if revalidate != nil {
		revalidate()
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := service.RemoveTaskLink(chatRPCContext(ctx, cfg), &projectv1.RemoveTaskLinkRequest{
			ProjectId: control.projectID, TaskId: control.taskID, LinkId: linkID,
			ExpectedTaskRevision: control.taskRevision, IdempotencyKey: uuid.NewString(),
		})
		projectWorkflowMu.Lock()
		delete(projectWorkflowRemoving[key], linkID)
		projectWorkflowMu.Unlock()
		if err != nil {
			setProjectFieldState(key, "workflows", "error", projectWriteError(err))
		} else {
			setProjectFieldState(key, "workflows", "saved", "")
		}
		projectTaskLinksForget(cfg, control.projectID, control.taskID)
		if revalidate != nil {
			revalidate()
		}
	}()
}
