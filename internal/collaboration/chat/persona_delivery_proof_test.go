package chat

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
)

type personaProofFloorFake struct{ calls int }

func (f *personaProofFloorFake) AuthorizePersonaOutput(context.Context, agentsecurity.FinalOutputPersistence) (PersonaAudienceDecision, error) {
	f.calls++
	return PersonaAudienceDecision{Revision: 17, Body: "safe", ParentID: "thread"}, nil
}

func TestIssuePersonaDeliveryProof_RejectsUnsealedOutput(t *testing.T) {
	floor := &personaProofFloorFake{}
	proof, err := IssuePersonaDeliveryProof(context.Background(), agentsecurity.FinalOutputPersistence{}, floor)
	if proof != nil || err != ErrPersonaProofUnavailable || floor.calls != 0 {
		t.Fatalf("zero output proof=%v err=%v floor calls=%d", proof, err, floor.calls)
	}
}

func TestIssuePersonaDeliveryProof_RejectsMissingFloor(t *testing.T) {
	proof, err := IssuePersonaDeliveryProof(context.Background(), agentsecurity.FinalOutputPersistence{}, nil)
	if proof != nil || err != ErrPersonaProofUnavailable {
		t.Fatalf("missing floor proof=%v err=%v", proof, err)
	}
}
