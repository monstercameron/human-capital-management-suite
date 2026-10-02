package application

import (
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// personaPrivateReasonError is a refusal to post an answer to the conversation
// that also says why, so the card the asker is left with can say it in a line.
// It is a permission refusal in every other respect.
type personaPrivateReasonError struct{ reason string }

func (e personaPrivateReasonError) Error() string {
	return "persona reply stays private: " + e.reason
}

func (e personaPrivateReasonError) Is(target error) bool { return target == chat.ErrPermissionDenied }

// personaPrivateReasonOf names why a public delivery was refused. Anything that
// does not say it was the agent's setting is the audience check: it could not be
// established that every member may read every source.
func personaPrivateReasonOf(err error) string {
	var reasoned personaPrivateReasonError
	if errors.As(err, &reasoned) && chat.ValidPrivateReason(reasoned.reason) {
		return reasoned.reason
	}
	return chat.PrivateReasonAudience
}

// ErrPersonaShareRefused is returned when an answer cannot be shared to its
// channel; Reason is one of chat.PrivateReason*.
type ErrPersonaShareRefused struct{ Reason string }

func (e ErrPersonaShareRefused) Error() string {
	return "persona answer cannot be shared to the channel: " + e.Reason
}

func (e ErrPersonaShareRefused) Is(target error) bool { return target == chat.ErrPermissionDenied }
