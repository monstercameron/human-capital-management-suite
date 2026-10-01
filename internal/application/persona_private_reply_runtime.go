package application

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

var errPersonaPrivateReplyRuntimeUnavailable = errors.New("application: private persona reply runtime unavailable")

// PersonaPrivateReplyRuntimeConfig contains the current sealed-output authority,
// immutable persistence port, and audited chat effects for private delivery.
type PersonaPrivateReplyRuntimeConfig struct {
	OutputAuthority PersonaRunChatReplyAuthoritySource
	OutputPersister PersonaRunFinalOutputPersister
	OutputSource    PersonaOutputSource
	Chat            chat.ConversationService
	ReplyCommitter  chat.PersonaReplyCommitter
	OutputPolicy    PersonaReplyOutputPolicy
}

// PersonaPrivateReplyRuntime supplies the validator and delivery ports used by
// the run executor. Public posting remains disabled until a shared audience
// eligibility and commit fence is available.
type PersonaPrivateReplyRuntime struct {
	Output       PersonaRunOutputValidator
	Reply        PersonaRunReplyDeliverer
	OutputSource PersonaOutputSource
}

// NewPersonaPrivateReplyRuntime composes sealed output validation/persistence
// with the existing renderer and invoker-only ephemeral delivery path.
func NewPersonaPrivateReplyRuntime(cfg PersonaPrivateReplyRuntimeConfig) (*PersonaPrivateReplyRuntime, error) {
	if isNilPersonaOutputPort(cfg.OutputAuthority) || isNilPersonaOutputPort(cfg.OutputPersister) ||
		isNilPersonaOutputPort(cfg.Chat) || isNilPersonaOutputPort(cfg.ReplyCommitter) {
		return nil, errPersonaPrivateReplyRuntimeUnavailable
	}
	validator, err := NewPersonaRunOutputValidator(PersonaRunOutputValidatorConfig{
		Authority: cfg.OutputAuthority,
		Persister: cfg.OutputPersister,
	})
	if err != nil {
		return nil, errPersonaPrivateReplyRuntimeUnavailable
	}
	delivery, err := NewPersonaReplyDeliveryWithOutputPolicy(cfg.Chat, cfg.ReplyCommitter, personaPrivateOnlyAudienceFloor{}, cfg.OutputPolicy)
	if err != nil {
		return nil, errPersonaPrivateReplyRuntimeUnavailable
	}
	return &PersonaPrivateReplyRuntime{Output: validator, Reply: delivery, OutputSource: cfg.OutputSource}, nil
}

type personaPrivateOnlyAudienceFloor struct{}

func (personaPrivateOnlyAudienceFloor) AuthorizePersonaOutput(context.Context, agentsecurity.FinalOutputPersistence) (chat.PersonaAudienceDecision, error) {
	return chat.PersonaAudienceDecision{}, chat.ErrPermissionDenied
}

var _ PersonaRunOutputValidator = (*SealedPersonaRunOutputValidator)(nil)
var _ PersonaRunReplyDeliverer = (*PersonaReplyDelivery)(nil)
