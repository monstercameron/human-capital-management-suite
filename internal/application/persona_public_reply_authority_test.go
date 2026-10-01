package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

func TestTodo_AGENTP_012_PublicReplyRequiresCurrentOutputAndVerifiedWorker(t *testing.T) {
	validator, record, run, _, _ := personaRunOutputFixture(t)
	output, err := validator.ValidateAndPersistPersonaOutput(context.Background(), record, run, agentmodel.ModelResult{Text: "A governed reply.", Finish: agentmodel.FinishComplete})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "alice", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "public-reply", CredentialDigest: "public-reply", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), principal)
	current := &runtimeReplyCurrentAuthorityFake{}
	authority := &personaPublicReplyAuthority{current: current, worker: privateChatGatewayVerifiedWorker(t, now), now: func() time.Time { return now }}
	worker, err := authority.AuthorizeSealedPublicPersonaReply(ctx, output)
	if err != nil || worker.Role() != workload.RoleWorker || worker.Fingerprint() == "" || current.calls != 1 || current.identity != output.Identity() {
		t.Fatalf("public reply authority=%s current=%d error=%v", worker, current.calls, err)
	}
	if trustPrincipal, _ := trust.FromContext(ctx); trustPrincipal != principal {
		t.Fatal("workload authorization replaced the human invoker")
	}
	if _, err := authority.AuthorizeSealedPublicPersonaReply(context.Background(), output); !errors.Is(err, chat.ErrPermissionDenied) || current.calls != 1 {
		t.Fatalf("missing invoker reached current output authority: %v calls=%d", err, current.calls)
	}
	denied := errors.New("current grant revoked")
	current.err = denied
	if _, err := authority.AuthorizeSealedPublicPersonaReply(ctx, output); !errors.Is(err, denied) {
		t.Fatalf("current output refusal was ignored: %v", err)
	}
	current.err = nil
	authority.worker = privateChatGatewayVerifiedWorkerWithRole(t, now, workload.RoleProjector)
	if _, err := authority.AuthorizeSealedPublicPersonaReply(ctx, output); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("non-worker identity authorized a persona author: %v", err)
	}
	if _, err := newPersonaPublicReplyCommitter(nil, current, authority.worker, authority.now); !errors.Is(err, ErrPersonaReplyDeliveryUnavailable) {
		t.Fatalf("missing native chat owner accepted: %v", err)
	}
}
