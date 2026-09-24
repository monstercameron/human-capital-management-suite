package reviewparticipants

import (
	"context"
	"errors"
	"testing"
	"time"

	reviewparticipantsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/reviewparticipants/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTodo_REV_075_02_Transport(t *testing.T) {
	deniedErr := errors.New("denied")
	server := &server{deps: Dependencies{IsDenied: func(err error) bool { return errors.Is(err, deniedErr) }, Read: func(_ context.Context, _ *trust.Principal) (productui.ReviewParticipantsProjection, error) {
		return productui.ReviewParticipantsProjection{Cycles: []productui.ReviewParticipantsCycleProjection{{
			CycleID: "cycle-1", CycleRevision: 2, GraphRevision: 3, GraphDigest: "digest",
			Assignments: []productui.ReviewParticipantAssignmentProjection{{ParticipantID: "worker-a", ReviewerID: "worker-b", Relationship: "MANAGER"}},
		}}}, nil
	}}}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: "subject-a", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session-a", IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour), CredentialDigest: "digest-a"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := transport.WithInvocation(trust.WithPrincipal(context.Background(), principal), &transport.Invocation{})
	response, err := server.GetReviewParticipants(ctx, &reviewparticipantsv1.GetReviewParticipantsRequest{})
	if err != nil || len(response.GetCycles()) != 1 || response.GetCycles()[0].GetAssignments()[0].GetReviewerId() != "worker-b" {
		t.Fatalf("response = %+v, %v", response, err)
	}
	if _, err := server.GetReviewParticipants(context.Background(), &reviewparticipantsv1.GetReviewParticipantsRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated request = %v", err)
	}
	server.deps.Read = func(context.Context, *trust.Principal) (productui.ReviewParticipantsProjection, error) {
		return productui.ReviewParticipantsProjection{}, deniedErr
	}
	if _, err := server.GetReviewParticipants(ctx, &reviewparticipantsv1.GetReviewParticipantsRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("denied read = %v", err)
	}
	server.deps.Read = func(context.Context, *trust.Principal) (productui.ReviewParticipantsProjection, error) {
		return productui.ReviewParticipantsProjection{}, errors.New("store failure")
	}
	if _, err := server.GetReviewParticipants(ctx, &reviewparticipantsv1.GetReviewParticipantsRequest{}); status.Code(err) != codes.Unavailable {
		t.Fatalf("store failure = %v", err)
	}
}
