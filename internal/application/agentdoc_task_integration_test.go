package application

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	agentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/agent/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportagents "github.com/monstercameron/human-capital-management-suite/internal/transport/agents"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type agentTaskDocumentResolver func(context.Context, agentdocref.Invoker, []agentdocref.Reference) ([]agentdocref.ResolvedDocument, []agentdocref.Omission, error)

func (f agentTaskDocumentResolver) Resolve(ctx context.Context, invoker agentdocref.Invoker, refs []agentdocref.Reference) ([]agentdocref.ResolvedDocument, []agentdocref.Omission, error) {
	return f(ctx, invoker, refs)
}

func newServedAgentClient(t *testing.T, f *agentFixture) (context.Context, agentv1.AgentServiceClient) {
	t.Helper()
	now := time.Now().UTC()
	verifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(grpcserver.UnaryInterceptor(transporttest.Config(verifier, func() time.Time { return now }, "agent-task-integration", nil))))
	transportagents.Register(server, transportagents.Dependencies{Settings: f.cell.AgentSettings, Starter: f.runtime.Starter, Tasks: f.runtime.Starter})
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpc.NewClient("passthrough:///bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	claims := transporttest.DefaultClaims(now)
	claims.Tenant, claims.Subject, claims.OrganizationScopeID = f.tenant, agentTestWorker, "org:ironridge-demo"
	claims.Roles = []string{"worker_self"}
	token, err := transporttest.BearerToken(verifier, claims)
	if err != nil {
		t.Fatal(err)
	}
	ctx := metadata.AppendToOutgoingContext(context.Background(), transport.AuthorizationMetadataKey, token)
	return ctx, agentv1.NewAgentServiceClient(conn)
}

func TestTodo_AGENTDOC_004_Integration(t *testing.T) {
	f := newAgentFixture(t)
	f.setEnabled(t, true)
	documents := documentServiceFixture(t).store
	readableID, err := documents.CreateDocument(context.Background(), f.tenant, agentTestAdmin, "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	version, err := documents.SubmitCandidate(context.Background(), f.tenant, documenthubstore.Version{DocumentID: readableID, CreatorID: agentTestAdmin, Title: "Leave policy", Markdown: "# Leave\n\nCurrent policy.\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	deployDocumentVersion(t, documents, f.tenant, agentTestAdmin, readableID, version.ID, "")
	if _, err := documents.ShareDocument(context.Background(), f.tenant, readableID, agentTestAdmin, documenthubstore.GrantInput{SubjectKind: "person", SubjectID: agentTestWorker, Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectAllow}); err != nil {
		t.Fatal(err)
	}
	unreadableID, err := documents.CreateDocument(context.Background(), f.tenant, agentTestAdmin, "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	unreadableVersion, err := documents.SubmitCandidate(context.Background(), f.tenant, documenthubstore.Version{DocumentID: unreadableID, CreatorID: agentTestAdmin, Title: "Private policy", Markdown: "# Private\n\nNot readable.\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	deployDocumentVersion(t, documents, f.tenant, agentTestAdmin, unreadableID, unreadableVersion.ID, "")
	resolver, err := NewAgentDocumentResolver(documents)
	if err != nil {
		t.Fatal(err)
	}
	f.runtime.Starter.documents = resolver
	ref := agentdocref.Reference{DocumentID: readableID, VersionMode: agentdocref.ModeLatestPublished, Label: "Leave policy"}

	ctx, client := newServedAgentClient(t, f)
	started, err := client.StartAgentTask(ctx, &agentv1.StartAgentTaskRequest{Prompt: "Summarize leave", Mode: agentv1.AgentStartMode_AGENT_START_MODE_LONG_TASK, DocumentReferences: []*agentv1.AgentDocumentReference{{DocumentId: ref.DocumentID, VersionMode: agentv1.AgentDocumentVersionMode_AGENT_DOCUMENT_VERSION_MODE_LATEST_PUBLISHED, SectionAnchor: ref.SectionAnchor, Label: ref.Label}}})
	if err != nil {
		t.Fatal(err)
	}
	if started.GetState() != string(agentrun.StateAwaitingPlanConfirmation) || started.GetTask().GetDocumentReferences()[0].GetDocumentId() != ref.DocumentID {
		t.Fatalf("started = %+v", started)
	}
	got, err := client.GetAgentTask(ctx, &agentv1.GetAgentTaskRequest{TaskId: started.GetTaskId()})
	if err != nil || len(got.GetTask().GetDocumentReferences()) != 1 || got.GetTask().GetPrompt() != "Summarize leave" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	listed, err := client.ListAgentTasks(ctx, &agentv1.ListAgentTasksRequest{})
	if err != nil || len(listed.GetTasks()) != 1 || listed.GetTasks()[0].GetTaskId() != started.GetTaskId() {
		t.Fatalf("list = %+v, %v", listed, err)
	}
	_, err = client.StartAgentTask(ctx, &agentv1.StartAgentTaskRequest{Prompt: "Summarize private policy", Mode: agentv1.AgentStartMode_AGENT_START_MODE_LONG_TASK, DocumentReferences: []*agentv1.AgentDocumentReference{{DocumentId: unreadableID, VersionMode: agentv1.AgentDocumentVersionMode_AGENT_DOCUMENT_VERSION_MODE_LATEST_PUBLISHED, Label: "Private policy"}}})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("unreadable start = %v", err)
	}
	f.runtime.Starter.documents = nil
	_, err = client.StartAgentTask(ctx, &agentv1.StartAgentTaskRequest{Prompt: "Summarize leave", Mode: agentv1.AgentStartMode_AGENT_START_MODE_LONG_TASK, DocumentReferences: []*agentv1.AgentDocumentReference{{DocumentId: readableID, VersionMode: agentv1.AgentDocumentVersionMode_AGENT_DOCUMENT_VERSION_MODE_LATEST_PUBLISHED, Label: "Leave policy"}}})
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(status.Convert(err).Message(), "Documents are not available in this workspace") {
		t.Fatalf("unconfigured documents = %v", err)
	}
	withoutReferences, err := client.StartAgentTask(ctx, &agentv1.StartAgentTaskRequest{Prompt: "Give a general welcome tip", Mode: agentv1.AgentStartMode_AGENT_START_MODE_LONG_TASK})
	if err != nil || withoutReferences.GetTaskId() == "" {
		t.Fatalf("reference-free start = %+v, %v", withoutReferences, err)
	}
}

func TestTodo_AGENTDOC_004_SecurityAdmission(t *testing.T) {
	f := newAgentFixture(t)
	f.setEnabled(t, true)
	var invokers []agentdocref.Invoker
	f.runtime.Starter.documents = agentTaskDocumentResolver(func(_ context.Context, invoker agentdocref.Invoker, _ []agentdocref.Reference) ([]agentdocref.ResolvedDocument, []agentdocref.Omission, error) {
		invokers = append(invokers, invoker)
		return nil, []agentdocref.Omission{{Reason: agentdocref.NotFound}}, nil
	})
	principal := agentTestPrincipal(t, f.tenant, agentTestWorker)
	ctx := trust.WithPrincipal(context.Background(), principal)
	for _, ref := range []agentdocref.Reference{
		{DocumentID: "doc-unreadable", VersionMode: agentdocref.ModeLatestPublished, Label: "Unreadable policy"},
		{DocumentID: "doc-another-tenant", VersionMode: agentdocref.ModeLatestPublished, Label: "Foreign policy"},
	} {
		if _, err := f.runtime.Starter.StartTaskWithDocuments(ctx, principal, "summarize", agentclient.StartLongTask, []agentdocref.Reference{ref}); err != agentrun.ErrDocumentReferenceUnreadable {
			t.Fatalf("unreadable start = %v", err)
		}
	}
	if len(invokers) != 2 {
		t.Fatalf("resolver calls = %d", len(invokers))
	}
	for _, invoker := range invokers {
		if invoker.TenantID != f.tenant || invoker.SubjectID != agentTestWorker {
			t.Fatalf("resolver authority = %+v", invoker)
		}
	}
	runner, err := f.runtime.Platform.ForTenant(context.Background(), principal.Tenant())
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := runner.UserTasks(context.Background(), principal.Subject())
	if err != nil || len(tasks) != 0 {
		t.Fatalf("refused admission stored tasks = %+v, %v", tasks, err)
	}
}
