package application

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

type personaPublicReplyAuthority struct {
	current agentsecurity.FinalOutputRecoveryCurrentAuthority
	worker  PersonaPrivateChatWorkloadIdentitySource
	now     func() time.Time
}

func newPersonaPublicReplyCommitter(store *chatstore.Store, current agentsecurity.FinalOutputRecoveryCurrentAuthority, worker PersonaPrivateChatWorkloadIdentitySource, now func() time.Time) (chat.PersonaReplyCommitter, error) {
	if store == nil || isNilPersonaOutputPort(current) || isNilPersonaOutputPort(worker) || now == nil {
		return nil, ErrPersonaReplyDeliveryUnavailable
	}
	return chatstore.NewSealedPublicPersonaDelivery(store, &personaPublicReplyAuthority{current: current, worker: worker, now: now}, now)
}

func (a *personaPublicReplyAuthority) AuthorizeSealedPublicPersonaReply(ctx context.Context, output agentsecurity.FinalOutputPersistence) (workload.Identity, error) {
	if a == nil || ctx == nil || isNilPersonaOutputPort(a.current) || isNilPersonaOutputPort(a.worker) || a.now == nil || output.Digest() == "" {
		return workload.Identity{}, ErrPersonaReplyDeliveryUnavailable
	}
	i := output.Identity()
	if _, ok := personaRunChatPrincipal(ctx, i.TenantID, i.InvokerID); !ok {
		return workload.Identity{}, chat.ErrPermissionDenied
	}
	if err := a.current.AuthorizeRecoveredFinalOutput(ctx, output); err != nil {
		return workload.Identity{}, err
	}
	worker, err := a.worker.ResolvePersonaChatWorker(ctx)
	if err != nil || !privatePersonaReplyWorkerMatches(worker, a.now().UTC()) {
		return workload.Identity{}, chat.ErrPermissionDenied
	}
	return worker, nil
}
