package journeyclient

import (
	"context"
	"slices"
	"testing"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"google.golang.org/grpc/metadata"
)

func TestTodo_WF_UI_002_WorkflowReadsUseBoundedAuthenticatedAdapter(t *testing.T) {
	conn := &recordingConn{}
	service, ok := NewGRPCService(conn, "tok_workflow_author").(WorkflowViewerService)
	if !ok {
		t.Fatal("production gRPC service does not implement WorkflowViewerService")
	}
	if _, err := service.ListWorkflowPublications(context.Background(), &workflowv1.ListWorkflowPublicationsRequest{}); err != nil {
		t.Fatalf("ListWorkflowPublications: %v", err)
	}
	if _, err := service.GetWorkflowDefinitionView(context.Background(), &workflowv1.GetWorkflowDefinitionViewRequest{WorkflowId: "workflow.promotion"}); err != nil {
		t.Fatalf("GetWorkflowDefinitionView: %v", err)
	}
	want := []string{
		workflowv1.WorkflowService_ListWorkflowPublications_FullMethodName,
		workflowv1.WorkflowService_GetWorkflowDefinitionView_FullMethodName,
	}
	if !slices.Equal(conn.invoked, want) {
		t.Fatalf("invoked methods = %v, want %v", conn.invoked, want)
	}
	md, ok := metadata.FromOutgoingContext(conn.ctx)
	if !ok || !slices.Equal(md.Get(AuthorizationHeader), []string{"Bearer tok_workflow_author"}) {
		t.Fatalf("authorization metadata = %v", md.Get(AuthorizationHeader))
	}
	methods, err := canonicalWorkflowRPCMethods()
	if err != nil {
		t.Fatalf("canonicalWorkflowRPCMethods: %v", err)
	}
	if len(methods) != len(workflowv1.WorkflowService_ServiceDesc.Methods) {
		t.Fatalf("workflow method set = %d, grpc descriptor = %d", len(methods), len(workflowv1.WorkflowService_ServiceDesc.Methods))
	}
}
