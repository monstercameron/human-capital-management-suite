package chat

import (
	"context"
	"errors"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// SendEphemeralPost is the transport-neutral endpoint adapter used by the
// persona delivery surface until the generated chat RPC grows a dedicated
// ephemeral message shape. The target recipient is never accepted from the
// wire: the core service derives it from the authenticated principal.
func SendEphemeralPost(ctx context.Context, service chatcore.ConversationService, request chatcore.SendEphemeralPostRequest) (chatcore.EphemeralPost, error) {
	if service == nil {
		return chatcore.EphemeralPost{}, chatcore.ErrUnavailable
	}
	ephemeral, ok := service.(chatcore.EphemeralService)
	if !ok {
		return chatcore.EphemeralPost{}, chatcore.ErrUnavailable
	}
	post, err := ephemeral.SendEphemeralPost(ctx, request)
	if err != nil {
		return chatcore.EphemeralPost{}, err
	}
	if !post.OnlyVisibleToYou || post.RecipientSubjectID != request.Principal.SubjectID || post.RecipientHomeTenantID != request.Principal.TenantID {
		return chatcore.EphemeralPost{}, errors.New("chat transport: invalid ephemeral recipient projection")
	}
	return post, nil
}
