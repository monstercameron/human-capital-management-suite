package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

type proactiveWriterAuthority struct{ calls int }

func (a *proactiveWriterAuthority) AuthorizeSealedPublicPersonaReply(context.Context, agentsecurity.FinalOutputPersistence) (workload.Identity, error) {
	a.calls++
	return workload.Identity{}, nil
}
func (a *proactiveWriterAuthority) AuthorizeSealedAnnouncement(context.Context, agentsecurity.FinalOutputPersistence) (workload.Identity, error) {
	a.calls++
	return workload.Identity{}, nil
}

func TestAgentUXProactive_PublicWriterRequiresAdmission_Security(t *testing.T) {
	owner := &proactiveWriterAuthority{}
	writer, err := NewSealedPublicPersonaDelivery(&Store{}, owner, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	post, err := writer.CommitSealedAnnouncement(context.Background(), agentsecurity.FinalOutputPersistence{}, chat.PersonaReplyCommitRequest{TenantID: "foreign", AuthorID: "agent", ConversationID: "general", Body: "Forged announcement", IdempotencyKey: "occurrence"})
	if !errors.Is(err, chat.ErrPermissionDenied) || post.ID != "" || owner.calls != 0 {
		t.Fatalf("unadmitted public write reached authority: %+v calls=%d %v", post, owner.calls, err)
	}
}
