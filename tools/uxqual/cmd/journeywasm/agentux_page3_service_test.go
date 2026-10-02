package main

import (
	"context"
	"testing"
	"time"

	agentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/agent/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func agentUXPage3Projection(id string) *agentv1.AgentTaskProjection {
	return &agentv1.AgentTaskProjection{
		TaskId: id, State: "FAILED", Version: 7, Prompt: "Read the policy", ResultPreview: "No answer was produced.", FailureSummary: "The document service was unavailable.", Retryable: true,
		CreatedAt: timestamppb.New(time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)), UpdatedAt: timestamppb.New(time.Date(2026, 9, 30, 12, 5, 0, 0, time.UTC)),
		DocumentReferences: []*agentv1.AgentDocumentReference{{DocumentId: "policy", Label: "Benefits policy", VersionMode: agentv1.AgentDocumentVersionMode_AGENT_DOCUMENT_VERSION_MODE_LATEST_PUBLISHED, SectionAnchor: "leave"}},
		DocumentOmissions:  []*agentv1.AgentDocumentOmission{{Label: "Private notes", Reason: "unreadable"}},
		Steps:              []*agentv1.AgentTaskStepProjection{{Kind: "Prepare answer", Status: "In progress", StartedAt: timestamppb.New(time.Date(2026, 9, 30, 12, 4, 0, 0, time.UTC))}},
		AnsweringAgentId:   "policy-helper", AnsweringAgentDisplayName: "Policy Helper", AnsweringAgentVersion: "7",
	}
}

func TestTodo_AGENTUX_008_ClientRPCProjection(t *testing.T) {
	client := &fakeAgentClient{list: &agentv1.ListAgentTasksResponse{Tasks: []*agentv1.AgentTaskProjection{agentUXPage3Projection("task-1")}}, get: &agentv1.GetAgentTaskResponse{Task: agentUXPage3Projection("task-1")}}
	binding := &agentServiceBinding{cfg: journeyclient.Config{Bearer: "owner-token"}, client: client}
	tasks, err := binding.ListTasks(context.Background())
	if err != nil || len(tasks) != 1 || tasks[0].State != productui.AgentTaskFailed || tasks[0].FailureReason == "" || !tasks[0].Retryable || tasks[0].UpdatedAt.IsZero() || len(tasks[0].Documents) != 1 || tasks[0].Documents[0].SectionAnchor != "leave" || len(tasks[0].Steps) != 1 || tasks[0].Steps[0].State != "running" || tasks[0].AnsweringAgentDisplayName != "Policy Helper" {
		t.Fatalf("list projection = %+v, %v", tasks, err)
	}
	task, err := binding.GetTask(context.Background(), "task-1")
	if err != nil || task.ID != "task-1" || client.gotGet.GetTaskId() != "task-1" || client.bearer != journeyclient.BearerScheme+"owner-token" {
		t.Fatalf("get projection = %+v, %v request=%+v bearer=%q", task, err, client.gotGet, client.bearer)
	}
	merged := reconcileAgentTasks([]productui.AgentTask{{ID: "task-1", Steps: []productui.AgentTaskStep{{Name: "stale"}}}}, tasks)
	if len(merged) != 1 || len(merged[0].Steps) != 1 || merged[0].Steps[0].Name != "Prepare answer" || merged[0].FailureReason == "" {
		t.Fatalf("reconciled task = %+v", merged)
	}
}

func TestTodo_AGENTUX_009_ClientSelectionAndRefusal(t *testing.T) {
	client := &fakeAgentClient{start: &agentv1.StartAgentTaskResponse{Task: agentUXPage3Projection("selected")}}
	binding := &agentServiceBinding{client: client}
	task, err := binding.StartTaskModeWithSelection(context.Background(), "Keep this text", agentv1.AgentStartMode_AGENT_START_MODE_QUICK_ANSWER, nil, "policy-helper")
	if err != nil || task.ID != "selected" || client.gotStart.GetPersonaId() != "policy-helper" || client.gotStart.GetPrompt() != "Keep this text" {
		t.Fatalf("selected start = %+v, %v request=%+v", task, err, client.gotStart)
	}
	client.err = status.Error(codes.PermissionDenied, "This agent is not available to answer this request")
	if _, err := binding.StartTaskModeWithSelection(context.Background(), "Keep this text", agentv1.AgentStartMode_AGENT_START_MODE_QUICK_ANSWER, nil, "policy-helper"); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("selection refusal = %v", err)
	}
	if client.gotStart.GetPrompt() != "Keep this text" || client.gotStart.GetPersonaId() != "policy-helper" {
		t.Fatalf("refused request lost composer state: %+v", client.gotStart)
	}
}

func TestAgentUXPage4_ServedStatesKeepTheirTaskGroups(t *testing.T) {
	for state, want := range map[string]productui.AgentTaskState{
		"RUNNING": productui.AgentTaskRunning, "COMPLETED": productui.AgentTaskCompleted, "FAILED": productui.AgentTaskFailed,
		"AWAITING_PLAN_CONFIRMATION": productui.AgentTaskAwaitingPlanConfirmation,
	} {
		task, ok := projectAgentTask(&agentv1.AgentTaskProjection{TaskId: "task-" + state, State: state})
		if !ok || task.State != want {
			t.Fatalf("state %q projected as %q", state, task.State)
		}
	}
}

func TestTodo_AGENTDOC_006_ClientRequest(t *testing.T) {
	references := []agentdocref.Reference{{DocumentID: "policy", Label: "Benefits policy", SectionAnchor: "leave", VersionMode: agentdocref.ModeLatestPublished}}
	request, ok := newStartAgentTaskModeRequestWithDocuments(" Read this ", agentv1.AgentStartMode_AGENT_START_MODE_QUICK_ANSWER, references)
	if !ok || request.GetPrompt() != "Read this" || len(request.GetDocumentReferences()) != 1 || request.GetDocumentReferences()[0].GetVersionMode() != agentv1.AgentDocumentVersionMode_AGENT_DOCUMENT_VERSION_MODE_LATEST_PUBLISHED || request.GetDocumentReferences()[0].GetPinnedVersion() != 0 {
		t.Fatalf("document start request = %+v, %t", request, ok)
	}
	tooMany := make([]agentdocref.Reference, agentdocref.MaxRequestReferences+1)
	for index := range tooMany {
		tooMany[index] = agentdocref.Reference{DocumentID: "doc-" + string(rune('a'+index)), Label: "Document " + string(rune('a'+index)), VersionMode: agentdocref.ModeLatestPublished}
	}
	if _, ok := newStartAgentTaskModeRequestWithDocuments("read", agentv1.AgentStartMode_AGENT_START_MODE_QUICK_ANSWER, tooMany); ok {
		t.Fatal("request above the five-document limit was accepted")
	}
	client := &fakeAgentClient{start: &agentv1.StartAgentTaskResponse{Task: agentUXPage3Projection("task-new")}}
	binding := &agentServiceBinding{client: client}
	task, err := binding.StartTaskModeWithDocuments(context.Background(), "Read this", agentv1.AgentStartMode_AGENT_START_MODE_QUICK_ANSWER, references)
	if err != nil || task.ID != "task-new" || len(client.gotStart.GetDocumentReferences()) != 1 {
		t.Fatalf("started task = %+v, %v request=%+v", task, err, client.gotStart)
	}
}
