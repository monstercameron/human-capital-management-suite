package agentrun

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxTaskAgentIdentityRunes = 200

var (
	ErrTaskAgentInvalid     = errors.New("invalid task answering agent")
	ErrTaskAgentDenied      = errors.New("the selected agent is not available for this request")
	ErrTaskAgentUnavailable = errors.New("agent selection is not available")
)

// TaskAgentIdentity is the immutable, display-safe identity of the agent that
// answers a task. It contains no instructions, handles, installation IDs, or
// authority facts.
type TaskAgentIdentity struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Version     string `json:"version"`
}

// Validate bounds the identity before it becomes part of a durable plan.
func (a TaskAgentIdentity) Validate() error {
	for _, value := range []string{a.ID, a.DisplayName, a.Version} {
		if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxTaskAgentIdentityRunes {
			return ErrTaskAgentInvalid
		}
		for _, r := range value {
			if unicode.IsControl(r) {
				return ErrTaskAgentInvalid
			}
		}
	}
	return nil
}

type taskAgentIdentityContextKey struct{}

// WithTaskAgentIdentity binds an admission-resolved identity to task creation.
func WithTaskAgentIdentity(ctx context.Context, agent TaskAgentIdentity) (context.Context, error) {
	if ctx == nil || agent.Validate() != nil {
		return nil, ErrTaskAgentInvalid
	}
	return context.WithValue(ctx, taskAgentIdentityContextKey{}, agent), nil
}

func taskAgentIdentityFromContext(ctx context.Context) *TaskAgentIdentity {
	if ctx == nil {
		return nil
	}
	agent, ok := ctx.Value(taskAgentIdentityContextKey{}).(TaskAgentIdentity)
	if !ok || agent.Validate() != nil {
		return nil
	}
	return cloneTaskAgentIdentity(&agent)
}

func cloneTaskAgentIdentity(in *TaskAgentIdentity) *TaskAgentIdentity {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func sameTaskAgentIdentity(left, right *TaskAgentIdentity) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
