package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// allowWorkflowCalls admits every action. Fixtures for behavior unrelated
// to authorization use it so the fail-closed nil hook does not change what
// they prove; the authorization tests below use selective hooks instead.
func allowWorkflowCalls(context.Context, *trust.Principal, string) bool { return true }

// workflowTestContextAs admits the fixed test principal with its subject
// replaced, so participation tests can act as the participant, a
// supervisor, an operator or an outsider.
func workflowTestContextAs(t *testing.T, method, subject string) context.Context {
	t.Helper()
	now := time.Date(2026, 9, 5, 9, 0, 0, 0, time.UTC)
	verifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	claims := transporttest.DefaultClaims(now)
	claims.Subject = subject
	token, err := transporttest.BearerToken(verifier, claims)
	if err != nil {
		t.Fatalf("BearerToken: %v", err)
	}
	ctx, _, admitErr := transport.Admit(context.Background(), transporttest.Config(verifier, func() time.Time { return now }, "workflow-test-request", nil), transport.AdmissionRequest{
		Metadata: transport.MapMetadata{transport.AuthorizationMetadataKey: []string{token}},
		Method:   method, Kind: transport.KindGRPC,
		Message: &workflowv1.GetWorkflowRequest{InstanceId: "workflow-1"},
	})
	if admitErr != nil {
		t.Fatalf("Admit: %v", admitErr)
	}
	return ctx
}

func subjectRecord() Record {
	return Record{
		Instance: Instance{InstanceID: "workflow-1", TenantID: transporttest.Tenant, WorkflowID: "promotion",
			WorkflowVersion: 2, RuntimeStatus: "RUNNING", InstanceVersion: 7,
			BusinessSubjectRefs: []string{"worker:jane"},
			CreatedAt:           time.Date(2026, 9, 5, 9, 0, 0, 0, time.UTC)},
		Nodes: []NodeExecution{{NodeExecutionID: "node-a-1", WorkflowInstanceID: "workflow-1", NodeID: "a", Attempt: 1, Status: "RUNNING"}},
	}
}

// participantHookSelective admits the read capability to every caller but
// the operator admission to nobody: reads then stand or fall on
// participation and supervision alone.
func participantHookSelective(_ context.Context, _ *trust.Principal, action string) bool {
	return action != ActionInspectAnySubject
}

// operatorHook admits everything, modelling a caller whose durable grant
// the cell authorizer resolved to operator.
func operatorHook(context.Context, *trust.Principal, string) bool { return true }

var errSupervisionDown = errors.New("workflow test: supervision unavailable")

func codeOf(t *testing.T, err error) envelope.Code {
	t.Helper()
	owned, ok := envelope.As(err)
	if !ok {
		t.Fatalf("error is not an owned envelope error: %v", err)
	}
	return owned.Code()
}

// TestTodo_RBAC_RT_004 is the PRIMARY: an unwired service refuses every
// call before touching its reader; a wired service still refuses instance
// reads to callers who neither participate, supervise nor hold the
// operator grant, answering NOT_FOUND so an outsider cannot tell an
// unauthorized instance from an absent one.
func TestTodo_RBAC_RT_004(t *testing.T) {
	reader := &workflowTestReader{record: subjectRecord()}
	closed := &server{deps: Dependencies{Instances: reader, CursorKey: []byte("rt004-key")}}
	if _, err := closed.GetWorkflow(workflowTestContextAs(t, GetWorkflowProcedure, "jane"), &workflowv1.GetWorkflowRequest{InstanceId: "workflow-1"}); err == nil {
		t.Fatal("unwired GetWorkflow succeeded")
	} else if codeOf(t, err) != envelope.CodePermissionDenied {
		t.Fatalf("unwired GetWorkflow code = %v", codeOf(t, err))
	}
	if _, err := closed.ListNodeExecutions(workflowTestContextAs(t, ListNodeExecutionsProcedure, "jane"), &workflowv1.ListNodeExecutionsRequest{InstanceId: "workflow-1"}); err == nil {
		t.Fatal("unwired ListNodeExecutions succeeded")
	} else if codeOf(t, err) != envelope.CodePermissionDenied {
		t.Fatalf("unwired ListNodeExecutions code = %v", codeOf(t, err))
	}
	if _, err := closed.ListWorkflowPublications(workflowTestContextAs(t, ListWorkflowPublicationsProcedure, "jane"), &workflowv1.ListWorkflowPublicationsRequest{}); err == nil {
		t.Fatal("unwired ListWorkflowPublications succeeded")
	}
	if reader.calls.Load() != 0 {
		t.Fatalf("unwired calls reached the reader %d times", reader.calls.Load())
	}

	participant := &server{deps: Dependencies{Instances: reader, CursorKey: []byte("rt004-key"), Authorize: participantHookSelective}}
	if _, err := participant.GetWorkflow(workflowTestContextAs(t, GetWorkflowProcedure, "jane"), &workflowv1.GetWorkflowRequest{InstanceId: "workflow-1"}); err != nil {
		t.Fatalf("participant GetWorkflow: %v", err)
	}
	if _, err := participant.ListNodeExecutions(workflowTestContextAs(t, ListNodeExecutionsProcedure, "jane"), &workflowv1.ListNodeExecutionsRequest{InstanceId: "workflow-1"}); err != nil {
		t.Fatalf("participant ListNodeExecutions: %v", err)
	}

	outsider := &server{deps: Dependencies{Instances: reader, CursorKey: []byte("rt004-key"), Authorize: participantHookSelective}}
	callsBefore := reader.calls.Load()
	if _, err := outsider.GetWorkflow(workflowTestContextAs(t, GetWorkflowProcedure, "mallory"), &workflowv1.GetWorkflowRequest{InstanceId: "workflow-1"}); err == nil {
		t.Fatal("outsider GetWorkflow succeeded")
	} else if codeOf(t, err) != envelope.CodeNotFound {
		t.Fatalf("outsider GetWorkflow code = %v, want not-found (non-disclosing)", codeOf(t, err))
	}
	if _, err := outsider.ListNodeExecutions(workflowTestContextAs(t, ListNodeExecutionsProcedure, "mallory"), &workflowv1.ListNodeExecutionsRequest{InstanceId: "workflow-1"}); err == nil {
		t.Fatal("outsider ListNodeExecutions succeeded")
	} else if codeOf(t, err) != envelope.CodeNotFound {
		t.Fatalf("outsider ListNodeExecutions code = %v, want not-found (non-disclosing)", codeOf(t, err))
	}
	if reader.calls.Load() == callsBefore {
		t.Fatal("outsider reads never reached the reader, so the subject rule decided nothing")
	}

	operator := &server{deps: Dependencies{Instances: reader, CursorKey: []byte("rt004-key"), Authorize: operatorHook}}
	if _, err := operator.GetWorkflow(workflowTestContextAs(t, GetWorkflowProcedure, "ops"), &workflowv1.GetWorkflowRequest{InstanceId: "workflow-1"}); err != nil {
		t.Fatalf("operator GetWorkflow: %v", err)
	}

	supervised := &server{deps: Dependencies{Instances: reader, CursorKey: []byte("rt004-key"), Authorize: participantHookSelective,
		Supervision: func(_ context.Context, supervisor, subject string) (bool, error) {
			return supervisor == "boss" && subject == "worker:jane", nil
		}}}
	if _, err := supervised.GetWorkflow(workflowTestContextAs(t, GetWorkflowProcedure, "boss"), &workflowv1.GetWorkflowRequest{InstanceId: "workflow-1"}); err != nil {
		t.Fatalf("supervisor GetWorkflow: %v", err)
	}
	if _, err := supervised.GetWorkflow(workflowTestContextAs(t, GetWorkflowProcedure, "mallory"), &workflowv1.GetWorkflowRequest{InstanceId: "workflow-1"}); err == nil {
		t.Fatal("non-supervisor GetWorkflow succeeded")
	}
}

// TestTodo_RBAC_RT_004_Security proves the supervision admission fails
// closed on error, the participation match names no one through an empty
// ref, and a credential naming authority the hook does not grant still
// loses: the hook, not the token, decides.
func TestTodo_RBAC_RT_004_Security(t *testing.T) {
	reader := &workflowTestReader{record: subjectRecord()}
	failing := &server{deps: Dependencies{Instances: reader, CursorKey: []byte("rt004-key"), Authorize: participantHookSelective,
		Supervision: func(context.Context, string, string) (bool, error) {
			return true, errSupervisionDown
		}}}
	if _, err := failing.GetWorkflow(workflowTestContextAs(t, GetWorkflowProcedure, "boss"), &workflowv1.GetWorkflowRequest{InstanceId: "workflow-1"}); err == nil {
		t.Fatal("erroring supervision admitted the read")
	} else if codeOf(t, err) != envelope.CodeNotFound {
		t.Fatalf("erroring supervision code = %v", codeOf(t, err))
	}

	denied := &server{deps: Dependencies{Instances: reader, CursorKey: []byte("rt004-key"),
		Authorize: func(context.Context, *trust.Principal, string) bool { return false }}}
	if _, err := denied.GetWorkflow(workflowTestContextAs(t, GetWorkflowProcedure, "jane"), &workflowv1.GetWorkflowRequest{InstanceId: "workflow-1"}); err == nil {
		t.Fatal("token-named participant passed a denying hook")
	} else if codeOf(t, err) != envelope.CodePermissionDenied {
		t.Fatalf("denying hook code = %v", codeOf(t, err))
	}

	for _, tc := range []struct {
		name    string
		subject string
		refs    []string
		want    bool
	}{
		{"bare identity matches id part", "jane", []string{"worker:jane"}, true},
		{"exact ref matches", "worker:jane", []string{"worker:jane"}, true},
		{"match is case-insensitive", "JANE", []string{"worker:jane"}, true},
		{"employment ref matches id part", "doe-1", []string{"employment:doe-1"}, true},
		{"outsider matches nothing", "mallory", []string{"worker:jane"}, false},
		{"empty ref names no one", "jane", []string{""}, false},
		{"empty subject names no one", "", []string{"worker:jane"}, false},
		{"no refs names no one", "jane", nil, false},
	} {
		if got := participantOf(tc.subject, tc.refs); got != tc.want {
			t.Errorf("participantOf(%q, %v) = %v, want %v", tc.subject, tc.refs, got, tc.want)
		}
	}

	srv := &server{deps: Dependencies{Instances: reader, CursorKey: []byte("rt004-key"), Authorize: participantHookSelective}}
	if srv.mayInspectSubject(context.Background(), nil, []string{"worker:jane"}) {
		t.Error("nil principal passed the subject rule")
	}
	if _, err := srv.ListNodeExecutions(workflowTestContextAs(t, ListNodeExecutionsProcedure, "mallory"), &workflowv1.ListNodeExecutionsRequest{
		InstanceId: "workflow-1", Page: &commonv1.PageRequest{PageSize: 2},
	}); err == nil {
		t.Error("outsider paged list succeeded")
	}
}
