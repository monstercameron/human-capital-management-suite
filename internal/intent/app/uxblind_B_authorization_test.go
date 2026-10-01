package app

import (
	"errors"
	"testing"
	"time"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_UXBLIND_005_Security(t *testing.T) {
	proposal := &intentsv1.IntentInstance{Initiator: &intentsv1.PrincipalReference{PrincipalId: "proposer"}}
	if err := authorizeJourneyFeatureAction("proposer", proposal); err != nil {
		t.Fatalf("proposer intervention was refused: %v", err)
	}
	err := authorizeJourneyFeatureAction("finance-approver", proposal)
	if err == nil {
		t.Fatal("finance approver could use proposer-only journey intervention")
	}
	var denied *envelope.Error
	if !errors.As(err, &denied) || denied.Code() != envelope.CodePermissionDenied || denied.ReasonRef() != reasonJourneyFeatureActionDenied {
		t.Fatalf("wrong refusal: %v", err)
	}
}

func TestTodo_UXBLIND_006_Security(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "acme-corp", Subject: "worker-self", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "uxblind-session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		CredentialDigest: "uxblind-credential",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !journeySubjectIsViewer(principal, WorkerLocation{Key: "worker-self"}) {
		t.Fatal("worker key did not identify the viewer's own subject")
	}
	if journeySubjectIsViewer(principal, WorkerLocation{Key: "worker-other"}) {
		t.Fatal("different worker was treated as the viewer's own subject")
	}
	if errors.Is(journeyError(envelope.New(envelope.CodePermissionDenied, reasonJourneySelfPromotion, "self")), workspace.ErrDenied) == false {
		t.Fatal("self-promotion refusal did not project to journey denied")
	}
}
