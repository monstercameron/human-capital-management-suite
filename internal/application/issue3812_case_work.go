package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ErrChatWorkCaseRestricted is returned when the message is in a conversation
// bound to a restricted case. It is a kind of denial, with its own code so the
// page can say why.
var ErrChatWorkCaseRestricted = fmt.Errorf("%w: restricted case conversation", ErrChatWorkDenied)

// chatCaseRestrictor is what the chat service offers to the callers that move a
// message out of its conversation.
type chatCaseRestrictor interface {
	CaseRestriction(ctx context.Context, tenant, conversation string, action chat.CaseAction) error
}

var _ chatCaseRestrictor = (*chat.Service)(nil)

// caseRestriction refuses to turn a message of a restricted case conversation
// into work elsewhere. A chat service that cannot answer the question is not
// trusted to have none: it is the one that holds the bindings.
func (w *ChatMessageWork) caseRestriction(ctx context.Context, tenant, conversation string) error {
	restrictor := w.cases
	if restrictor == nil {
		var ok bool
		if restrictor, ok = w.chat.(chatCaseRestrictor); !ok {
			return nil
		}
	}
	err := restrictor.CaseRestriction(ctx, tenant, conversation, chat.CaseActionSave)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, chat.ErrPermissionDenied):
		return ErrChatWorkCaseRestricted
	default:
		return ErrChatWorkUnavailable
	}
}

// chatCaseGateMembership puts the case binding in front of the join-request
// approval path, which writes the membership itself inside the gate's own
// transaction and so never reaches the chat service's AddMembership. A person
// is not admitted to a conversation bound to a case by approving a request: the
// case owner adds people, and only the case owner.
func chatCaseGateMembership(inner chatstore.GateMembershipPort, isBound func(ctx context.Context, tx dbport.Tx, tenant, conversation string) (bool, error)) chatstore.GateMembershipPort {
	return func(ctx context.Context, tx dbport.Tx, scope chatgate.Scope, actor chatgate.Actor, key string, admit bool) error {
		if admit {
			bound, err := isBound(ctx, tx, scope.Tenant, scope.Conversation)
			if err != nil {
				return chat.ErrUnavailable
			}
			if bound {
				return fmt.Errorf("%w: %w", chatgate.ErrDenied, &chat.CaseRestrictionError{Action: chat.CaseActionMembership})
			}
		}
		return inner(ctx, tx, scope, actor, key, admit)
	}
}

// caseAllowsAppInstall refuses an app install by anyone but the case owner in a
// conversation bound to a case. Without a case surface nothing is bound.
func (s *ChatExtensions) caseAllowsAppInstall(ctx context.Context, p chat.Principal, conversation string) error {
	if s.Cases == nil {
		return nil
	}
	view, err := s.Cases.Case(ctx, p, conversation)
	if err != nil {
		return chat.ErrUnavailable
	}
	if view.Bound && !view.OwnerIsMe {
		return &chat.CaseRestrictionError{Action: chat.CaseActionMembership}
	}
	return nil
}
