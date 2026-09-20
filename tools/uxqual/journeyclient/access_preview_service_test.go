package journeyclient

import (
	"context"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TestTodo_REV_093_01_PreviewRoleAccessClient pins the production client's
// preview call to its canonical method and proves it carries the bearer, so
// the server, not the browser, authorizes and resolves every preview.
func TestTodo_REV_093_01_PreviewRoleAccessClient(t *testing.T) {
	conn := &recordingConn{}
	svc, ok := NewGRPCService(conn, "tok_preview").(AccessPreviewService)
	if !ok {
		t.Fatal("production client does not implement AccessPreviewService")
	}
	if _, err := svc.PreviewRoleAccess(context.Background(), &journeyv1.PreviewRoleAccessRequest{Proposed: &journeyv1.RoleOrganizationVisibilityPolicy{RoleId: "manager", Mode: "ALL"}}); err != nil {
		t.Fatalf("PreviewRoleAccess: %v", err)
	}
	if len(conn.invoked) != 1 || conn.invoked[0] != journeyv1.JourneyService_PreviewRoleAccess_FullMethodName {
		t.Fatalf("invoked = %v, want %s", conn.invoked, journeyv1.JourneyService_PreviewRoleAccess_FullMethodName)
	}
	md, _ := metadata.FromOutgoingContext(conn.ctx)
	if got := md.Get(AuthorizationHeader); len(got) != 1 || got[0] != "Bearer tok_preview" {
		t.Fatalf("authorization = %v, want the configured bearer", got)
	}

	anonymous := NewGRPCService(&recordingConn{}, "").(AccessPreviewService)
	if _, err := anonymous.PreviewRoleAccess(context.Background(), &journeyv1.PreviewRoleAccessRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("anonymous preview code = %v, want UNAUTHENTICATED", status.Code(err))
	}
}
