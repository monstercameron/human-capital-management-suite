package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// AGENTUX-075: an agent's owner can turn off the reaction the agent puts on the
// questions it is asked. The choice is one stored setting per agent; this file is
// the command that sets it and the port that stores it.

// personaReactionSettingWriter stores the owner's choice.
type personaReactionSettingWriter interface {
	SetPersonaReactions(ctx context.Context, tenant, personaID, actor string, react bool, at time.Time) error
}

// personaReactionSettingStore is the setting kept in the agent database, read by
// the runtime and written by the command.
type personaReactionSettingStore struct{ store *agentinvocationstore.Store }

func (s personaReactionSettingStore) PersonaReactionsEnabled(ctx context.Context, tenant, personaID string) (bool, error) {
	return s.store.PersonaReactionsEnabled(ctx, tenant, personaID)
}

func (s personaReactionSettingStore) SetPersonaReactions(ctx context.Context, tenant, personaID, actor string, react bool, at time.Time) error {
	_, err := s.store.SetPersonaReactions(ctx, tenant, personaID, actor, react, at)
	return err
}

// personaAdminWithoutReaction is the request with the reaction choice cleared, so
// that the shared field checks of the lifecycle commands can be applied to the
// rest of a SET_REACTIONS request.
func personaAdminWithoutReaction(request productui.PersonaAdminCommandRequest) productui.PersonaAdminCommandRequest {
	request.ReactToQuestions = nil
	return request
}

var errPersonaAdminReactionsUnavailable = errors.New("application: agent reaction settings unavailable")

// setReactions stores the owner's choice for an agent that exists in the actor's
// tenant. The actor's authority to change the agent's setup was proved by the
// command authorizer before this runs.
func (e *PersonaAdminLifecycleExecutor) setReactions(ctx context.Context, actor PersonaAdminCommandActor, command PersonaAdminCommand) error {
	if e.Reactions == nil || command.ReactToQuestions == nil || e.Now == nil || e.Store == nil {
		return errPersonaAdminReactionsUnavailable
	}
	tenant, err := e.Store.ForTenant(ctx, actor.Tenant)
	if err != nil || tenant == nil {
		return ErrPersonaAdminLifecycleUnavailable
	}
	versions, err := tenant.ListVersions(ctx, command.PersonaID)
	if err != nil {
		return ErrPersonaAdminLifecycleUnavailable
	}
	if len(versions) == 0 {
		return personaAdminCommandError(personaAdminCodeInvalid, ErrPersonaAdminCommandUnavailable)
	}
	if err := e.Reactions.SetPersonaReactions(ctx, actor.Tenant.String(), strings.TrimSpace(command.PersonaID), actor.Subject, *command.ReactToQuestions, e.Now().UTC()); err != nil {
		return ErrPersonaAdminLifecycleUnavailable
	}
	return nil
}
