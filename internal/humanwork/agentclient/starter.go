package agentclient

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	// ErrDisabled means the principal's tenant has not turned agents on.
	ErrDisabled = errors.New("agentclient: agents are not enabled for this tenant")
	// ErrInvalidPrompt means the prompt was empty or over the size bound.
	ErrInvalidPrompt = errors.New("agentclient: the task prompt is empty or too long")
	// ErrNotAuthorized means the principal may not start agent tasks.
	ErrNotAuthorized = errors.New("agentclient: the principal may not start agent tasks")
)

// StartedTask is the immediate answer to starting a task: the durable id and
// the state the task had when the start call returned. It carries no ledger
// entry, no tainted content and no failure detail.
type StartedTask struct {
	ID      string
	State   string
	Version uint64
}

// Starter is the write side of the Agents page: it turns the signed-in user's
// own prompt into a confirmed, read-only task and drives it. The principal is
// the verified transport principal; the tenant, the user and the user's
// authority are derived from it and never from the prompt. Implementations
// refuse with ErrDisabled when the tenant setting is off.
type Starter interface {
	StartTask(ctx context.Context, principal *trust.Principal, prompt string) (StartedTask, error)
}
