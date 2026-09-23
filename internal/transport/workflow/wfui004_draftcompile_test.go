package workflow

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	workflowcore "github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/draftcompile"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
)

func TestTodo_WF_UI_004_TransportCompilesStoredRevision(t *testing.T) {
	definition := prototype.ApprovalDefinition()
	definition.Edges[0].To = "missing-node"
	document, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	reader := &wfui004DraftReader{draft: Draft{
		DraftID: "draft-42", AuthorRef: transporttest.Subject, Revision: 7, Document: document,
	}}
	srv := &server{deps: Dependencies{
		Drafts:        reader,
		DraftCompiler: draftcompile.Compiler{Options: workflowcore.Options{Phase: workflowcore.PhaseP1B}},
		Authorize:     allowWorkflowCalls,
	}}
	response, err := srv.CompileWorkflowDraft(
		workflowTestContext(t, CompileWorkflowDraftProcedure),
		&workflowv1.CompileWorkflowDraftRequest{DraftId: "draft-42"},
	)
	if err != nil {
		t.Fatalf("CompileWorkflowDraft: %v", err)
	}
	if response.GetDraftId() != "draft-42" || response.GetRevision() != 7 || response.GetValid() {
		t.Fatalf("response identity = %+v", response)
	}
	foundEdge := false
	for _, diagnostic := range response.GetDiagnostics() {
		if diagnostic.GetEdgeId() != "" && diagnostic.GetEdgeFrom() != "" {
			foundEdge = true
		}
	}
	if !foundEdge {
		t.Fatalf("stored revision diagnostics are not graph-addressable: %+v", response.GetDiagnostics())
	}
	if reader.calls.Load() != 1 || reader.tenant != values.TenantId(transporttest.Tenant) {
		t.Fatalf("draft reads=%d tenant=%q", reader.calls.Load(), reader.tenant)
	}
}

func TestTodo_WF_UI_004_TransportSecurity(t *testing.T) {
	document, err := json.Marshal(prototype.ApprovalDefinition())
	if err != nil {
		t.Fatal(err)
	}
	t.Run("authorization precedes storage", func(t *testing.T) {
		reader := &wfui004DraftReader{}
		srv := &server{deps: Dependencies{
			Drafts: reader, DraftCompiler: draftcompile.Compiler{},
			Authorize: func(context.Context, *trust.Principal, string) bool { return false },
		}}
		_, err := srv.CompileWorkflowDraft(workflowTestContext(t, CompileWorkflowDraftProcedure), &workflowv1.CompileWorkflowDraftRequest{DraftId: "draft-42"})
		owned, ok := envelope.As(err)
		if !ok || owned.Code() != envelope.CodePermissionDenied {
			t.Fatalf("authorization error = %v", err)
		}
		if reader.calls.Load() != 0 {
			t.Fatalf("denied request read %d drafts", reader.calls.Load())
		}
	})
	t.Run("author mismatch is undisclosed and not compiled", func(t *testing.T) {
		reader := &wfui004DraftReader{draft: Draft{DraftID: "draft-42", AuthorRef: "another-author", Revision: 1, Document: document}}
		compiler := &wfui004CompilerSpy{}
		srv := &server{deps: Dependencies{Drafts: reader, DraftCompiler: compiler, Authorize: allowWorkflowCalls}}
		_, err := srv.CompileWorkflowDraft(workflowTestContext(t, CompileWorkflowDraftProcedure), &workflowv1.CompileWorkflowDraftRequest{DraftId: "draft-42"})
		owned, ok := envelope.As(err)
		if !ok || owned.Code() != envelope.CodeNotFound {
			t.Fatalf("ownership error = %v", err)
		}
		if compiler.calls.Load() != 0 {
			t.Fatal("another author's draft reached the compiler")
		}
	})
}

func TestTodo_WF_UI_004_TransportGolden(t *testing.T) {
	document, err := json.Marshal(prototype.ApprovalDefinition())
	if err != nil {
		t.Fatal(err)
	}
	srv := &server{deps: Dependencies{
		Drafts:        &wfui004DraftReader{draft: Draft{DraftID: "draft-42", AuthorRef: transporttest.Subject, Revision: 3, Document: document}},
		DraftCompiler: draftcompile.Compiler{Options: workflowcore.Options{Phase: workflowcore.PhaseP1B}},
		Authorize:     allowWorkflowCalls,
	}}
	response, err := srv.CompileWorkflowDraft(workflowTestContext(t, CompileWorkflowDraftProcedure), &workflowv1.CompileWorkflowDraftRequest{DraftId: "draft-42"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	compact := strings.ReplaceAll(string(got), " ", "")
	want := `{"draft_id":"draft-42","revision":"3","valid":true,"compiled_plan_digest":"`
	if !strings.HasPrefix(compact, want) ||
		!strings.Contains(compact, `"effects":{"zero_effect":true`) ||
		!strings.Contains(compact, `"allowed_modes":["SIMULATE","EXECUTE","REPLAY","REPAIR","SHADOW"]`) ||
		!strings.HasSuffix(compact, `"unwind":{"complete":true}}`) {
		t.Fatalf("draft compile response golden drifted: %s", got)
	}
}

type wfui004DraftReader struct {
	draft  Draft
	err    error
	calls  atomic.Int64
	tenant values.TenantId
}

func (r *wfui004DraftReader) ReadWorkflowDraft(_ context.Context, tenant values.TenantId, draftID string) (Draft, error) {
	r.calls.Add(1)
	r.tenant = tenant
	if r.err != nil {
		return Draft{}, r.err
	}
	if draftID != r.draft.DraftID {
		return Draft{}, ErrNotFound
	}
	return r.draft, nil
}

type wfui004CompilerSpy struct{ calls atomic.Int64 }

func (s *wfui004CompilerSpy) CompileDocument(context.Context, values.TenantId, json.RawMessage) draftcompile.Result {
	s.calls.Add(1)
	return draftcompile.Result{Valid: true}
}
