package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/encoding/protojson"

	agentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/agent/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type documentStarter struct {
	fakeStarter
	refs []agentdocref.Reference
	mode agentclient.StartMode
}

func (s *documentStarter) StartTaskWithDocuments(_ context.Context, p *trust.Principal, prompt string, mode agentclient.StartMode, refs []agentdocref.Reference) (agentclient.StartedTask, error) {
	s.calls++
	s.principal, s.prompt, s.mode = p, prompt, mode
	s.refs = append([]agentdocref.Reference(nil), refs...)
	if s.err != nil {
		return agentclient.StartedTask{}, s.err
	}
	return agentclient.StartedTask{ID: "task-doc", State: "FAILED", Version: 4}, nil
}

type documentTaskReader struct {
	task  agentrun.AgentTask
	tasks []agentrun.AgentTask
	err   error
}

func (r documentTaskReader) GetAgentTask(context.Context, *trust.Principal, string) (agentrun.AgentTask, error) {
	if r.err != nil {
		return agentrun.AgentTask{}, r.err
	}
	return r.task, nil
}

func (r documentTaskReader) ListAgentTasks(context.Context, *trust.Principal) ([]agentrun.AgentTask, error) {
	if r.err != nil {
		return nil, r.err
	}
	return append([]agentrun.AgentTask(nil), r.tasks...), nil
}

func protoReference(id, label string) *agentv1.AgentDocumentReference {
	return &agentv1.AgentDocumentReference{DocumentId: id, VersionMode: agentv1.AgentDocumentVersionMode_AGENT_DOCUMENT_VERSION_MODE_LATEST_PUBLISHED, Label: label}
}

func TestTodo_AGENTDOC_004_Security(t *testing.T) {
	principal := principalOf(t, "tenant-a", trust.SubjectKindHuman)
	ctx := trust.WithPrincipal(context.Background(), principal)
	starter := &documentStarter{}
	server := NewServer(Dependencies{Starter: starter})

	tooMany := make([]*agentv1.AgentDocumentReference, agentdocref.MaxRequestReferences+1)
	for i := range tooMany {
		tooMany[i] = protoReference(string(rune('a'+i)), string(rune('A'+i)))
	}
	if _, err := server.StartAgentTask(ctx, &agentv1.StartAgentTaskRequest{Prompt: "summarize", DocumentReferences: tooMany}); code(err) != codes.InvalidArgument || starter.calls != 0 {
		t.Fatalf("six references = %v calls=%d", err, starter.calls)
	}

	for _, tc := range []struct {
		name string
		err  error
	}{
		{"unreadable", agentrun.ErrDocumentReferenceUnreadable},
		{"another tenant", agentrun.ErrDocumentReferenceUnreadable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			starter.err = tc.err
			_, err := server.StartAgentTask(ctx, &agentv1.StartAgentTaskRequest{Prompt: "summarize", DocumentReferences: []*agentv1.AgentDocumentReference{protoReference("doc-aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "Secret policy")}})
			if code(err) != codes.PermissionDenied || strings.Contains(err.Error(), "doc-aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa") || strings.Contains(err.Error(), "Secret policy") {
				t.Fatalf("refusal = %v", err)
			}
		})
	}
	starter.err = nil
	if _, err := server.StartAgentTask(ctx, &agentv1.StartAgentTaskRequest{Prompt: "summarize", DocumentReferences: []*agentv1.AgentDocumentReference{protoReference("doc-12345678-1234-4123-8123-123456789abc", "Policy")}}); err != nil {
		t.Fatal(err)
	}
	if len(starter.refs) != 1 || starter.refs[0] != (agentdocref.Reference{DocumentID: "doc-12345678-1234-4123-8123-123456789abc", VersionMode: agentdocref.ModeLatestPublished, Label: "Policy"}) || starter.principal != principal {
		t.Fatalf("forwarded = %+v principal=%p", starter.refs, starter.principal)
	}
}

func TestTodo_AGENTDOC_004_Golden(t *testing.T) {
	ref := agentdocref.Reference{DocumentID: "doc-12345678-1234-4123-8123-123456789abc", VersionMode: agentdocref.ModePinned, PinnedVersion: 3, SectionAnchor: "leave", Label: "Leave policy"}
	at := time.Date(2026, 9, 30, 14, 15, 16, 0, time.UTC)
	task := agentrun.AgentTask{ID: "task-doc", Goal: "Summarize leave", State: agentrun.StateFailed, Version: 4, CreatedAt: at, UpdatedAt: at.Add(time.Minute), FailureCode: "STEP_FAILED", Ledger: agentrun.TaskLedger{AnswerText: "One line result"}, Plan: agentrun.AgentPlan{DocumentReferences: []agentdocref.Reference{ref}, DocumentOmissions: []agentdocref.Omission{{Label: ref.Label, Reason: agentdocref.NotPublished}}, Steps: []agentrun.PlanStep{{Type: agentrun.StepAnalyze, State: agentrun.StepFailed, StartedAt: at.Add(10 * time.Second), FinishedAt: at.Add(20 * time.Second)}}}}
	starter := &documentStarter{}
	reader := documentTaskReader{task: task, tasks: []agentrun.AgentTask{task}}
	server := NewServer(Dependencies{Starter: starter, Tasks: reader})
	ctx := trust.WithPrincipal(context.Background(), principalOf(t, "tenant-a", trust.SubjectKindHuman))
	in := &agentv1.StartAgentTaskRequest{Prompt: "Summarize leave", Mode: agentv1.AgentStartMode_AGENT_START_MODE_LONG_TASK, DocumentReferences: []*agentv1.AgentDocumentReference{{DocumentId: ref.DocumentID, VersionMode: agentv1.AgentDocumentVersionMode_AGENT_DOCUMENT_VERSION_MODE_PINNED, PinnedVersion: ref.PinnedVersion, SectionAnchor: ref.SectionAnchor, Label: ref.Label}}}
	response, err := server.StartAgentTask(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := server.ListAgentTasks(ctx, &agentv1.ListAgentTasksRequest{})
	if err != nil || len(listed.GetTasks()) != 1 {
		t.Fatalf("list = %+v, %v", listed, err)
	}
	options := protojson.MarshalOptions{UseProtoNames: true}
	requestJSON, _ := options.Marshal(in)
	projectionJSON, _ := options.Marshal(response.GetTask())
	requestJSON = compactAgentTaskJSON(t, requestJSON)
	projectionJSON = compactAgentTaskJSON(t, projectionJSON)
	const wantRequest = `{"prompt":"Summarize leave","mode":"AGENT_START_MODE_LONG_TASK","document_references":[{"document_id":"doc-12345678-1234-4123-8123-123456789abc","version_mode":"AGENT_DOCUMENT_VERSION_MODE_PINNED","pinned_version":"3","section_anchor":"leave","label":"Leave policy"}]}`
	const wantProjection = `{"task_id":"task-doc","state":"FAILED","version":"4","prompt":"Summarize leave","document_references":[{"document_id":"doc-12345678-1234-4123-8123-123456789abc","version_mode":"AGENT_DOCUMENT_VERSION_MODE_PINNED","pinned_version":"3","section_anchor":"leave","label":"Leave policy"}],"document_omissions":[{"label":"Leave policy","reason":"NOT_PUBLISHED"}],"created_at":"2026-09-30T14:15:16Z","updated_at":"2026-09-30T14:16:16Z","result_preview":"One line result","failure_summary":"A step could not be completed.","retryable":true,"steps":[{"kind":"Prepare answer","status":"Failed","started_at":"2026-09-30T14:15:26Z","finished_at":"2026-09-30T14:15:36Z","failure_summary":"This step could not be completed."}],"answering_agent_id":"general-agent","answering_agent_display_name":"General agent","answering_agent_version":"hcm-agent-self-service/v1"}`
	if string(requestJSON) != wantRequest || string(projectionJSON) != wantProjection {
		t.Fatalf("request=%s\nprojection=%s", requestJSON, projectionJSON)
	}
	got, err := server.GetAgentTask(ctx, &agentv1.GetAgentTaskRequest{TaskId: task.ID})
	if err != nil || string(mustProtoJSON(t, got.GetTask())) != wantProjection {
		t.Fatalf("get = %+v, %v", got, err)
	}
}

func TestAgentUXR5Srv_DocumentUsage(t *testing.T) {
	refs := []agentdocref.Reference{
		{DocumentID: "doc-leave", VersionMode: agentdocref.ModeLatestPublished, Label: "Leave policy"},
		{DocumentID: "doc-pay", VersionMode: agentdocref.ModePinned, PinnedVersion: 4, Label: "Pay policy"},
	}
	legacy := agentrun.AgentTask{State: agentrun.StateCompleted, Plan: agentrun.AgentPlan{DocumentReferences: refs}}
	if projection := projectAgentTask(legacy); projection.GetDocumentUsageState() != agentv1.AgentDocumentUsageState_AGENT_DOCUMENT_USAGE_STATE_UNKNOWN || len(projection.GetUsedDocumentReferences()) != 0 {
		t.Fatalf("legacy usage = %s %+v", projection.GetDocumentUsageState(), projection.GetUsedDocumentReferences())
	}

	noCitation := legacy
	noCitation.Ledger.Entries = []agentrun.LedgerEntry{{Kind: "STEP_RESULT", SourceIDs: []string{agentDocumentUsageLedgerMarker}}}
	if projection := projectAgentTask(noCitation); projection.GetDocumentUsageState() != agentv1.AgentDocumentUsageState_AGENT_DOCUMENT_USAGE_STATE_NONE || len(projection.GetUsedDocumentReferences()) != 0 {
		t.Fatalf("uncited usage = %s %+v", projection.GetDocumentUsageState(), projection.GetUsedDocumentReferences())
	}

	cited := legacy
	cited.Ledger.Entries = []agentrun.LedgerEntry{{Kind: "STEP_RESULT", SourceIDs: []string{agentDocumentUsageLedgerMarker, "document:doc-pay/version:4", "document:another-tenant#summary"}}}
	projection := projectAgentTask(cited)
	if projection.GetDocumentUsageState() != agentv1.AgentDocumentUsageState_AGENT_DOCUMENT_USAGE_STATE_USED || len(projection.GetUsedDocumentReferences()) != 1 || projection.GetUsedDocumentReferences()[0].GetDocumentId() != "doc-pay" || projection.GetUsedDocumentReferences()[0].GetLabel() != "Pay policy" {
		t.Fatalf("cited usage = %s %+v", projection.GetDocumentUsageState(), projection.GetUsedDocumentReferences())
	}
}

func mustProtoJSON(t *testing.T, message *agentv1.AgentTaskProjection) []byte {
	t.Helper()
	encoded, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	return compactAgentTaskJSON(t, encoded)
}

func compactAgentTaskJSON(t *testing.T, encoded []byte) []byte {
	t.Helper()
	var compact bytes.Buffer
	if err := json.Compact(&compact, encoded); err != nil {
		t.Fatal(err)
	}
	return compact.Bytes()
}

func TestAgentTaskProjectionReadErrors(t *testing.T) {
	ctx := trust.WithPrincipal(context.Background(), principalOf(t, "tenant-a", trust.SubjectKindHuman))
	server := NewServer(Dependencies{Tasks: documentTaskReader{err: agentrun.ErrNotFound}})
	if _, err := server.GetAgentTask(ctx, &agentv1.GetAgentTaskRequest{TaskId: "missing"}); code(err) != codes.NotFound {
		t.Fatalf("missing = %v", err)
	}
	server = NewServer(Dependencies{Tasks: documentTaskReader{err: errors.New("private store detail")}})
	if _, err := server.ListAgentTasks(ctx, &agentv1.ListAgentTasksRequest{}); code(err) != codes.Unavailable || strings.Contains(err.Error(), "private") {
		t.Fatalf("list failure = %v", err)
	}
}
