// Package personachat is the HTTP boundary for authorized persona chat views.
package personachat

import (
	"context"
	"errors"
)

var (
	ErrUnauthenticated = errors.New("persona chat: authentication required")
	ErrDenied          = errors.New("persona chat: access denied")
	ErrInvalid         = errors.New("persona chat: invalid request")
	ErrUnavailable     = errors.New("persona chat: unavailable")
	ErrConflict        = errors.New("persona chat: conflict")
)

const Path = "/api/chat/personas"

type Reference struct {
	Kind           string `json:"kind"`
	TenantID       string `json:"tenant_id"`
	ID             string `json:"id"`
	Display        string `json:"display"`
	ConversationID string `json:"conversation_id"`
}

type Skill struct {
	Name string `json:"name"`
	Tier string `json:"tier"`
}

type Profile struct {
	Reference      Reference `json:"reference"`
	Purpose        string    `json:"purpose"`
	Owner          string    `json:"owner"`
	Version        string    `json:"version"`
	Skills         []Skill   `json:"skills"`
	DataClasses    []string  `json:"data_classes"`
	CannotDo       []string  `json:"cannot_do"`
	ReplyPlacement string    `json:"reply_placement"`
}

type Directory struct {
	Personas   []Profile   `json:"personas"`
	PostActors []PostActor `json:"post_actors"`
}

type PostActor struct {
	PostID         string `json:"post_id"`
	PersonaID      string `json:"persona_id"`
	AgentID        string `json:"agent_id"`
	InvokerHandle  string `json:"invoker_handle"`
	Display        string `json:"display"`
	PersonaVersion string `json:"persona_version"`
}

type Invocation struct {
	InvocationID          string `json:"invocation_id"`
	PostID                string `json:"post_id"`
	ThreadID              string `json:"thread_id"`
	ConversationID        string `json:"conversation_id"`
	InvokerID             string `json:"invoker_id"`
	AgentName             string `json:"agent_name"`
	Status                string `json:"status"`
	Activity              string `json:"activity"`
	CurrentStep           int    `json:"current_step"`
	TotalSteps            int    `json:"total_steps"`
	TaskID                string `json:"task_id,omitempty"`
	TaskTitle             string `json:"task_title,omitempty"`
	TaskState             string `json:"task_state,omitempty"`
	TaskRevision          uint64 `json:"task_revision,omitempty"`
	FailureCode           string `json:"failure_code,omitempty"`
	FailureMessage        string `json:"failure_message,omitempty"`
	Retryable             bool   `json:"retryable"`
	PrivateConversationID string `json:"private_conversation_id,omitempty"`
	PrivatePostID         string `json:"private_post_id,omitempty"`
	// PublicPostID is the answer posted to the conversation (AGENTUX-070).
	PublicPostID string `json:"public_post_id,omitempty"`
}

type Progress struct {
	Invocations []Invocation `json:"invocations"`
}

type RetryResult struct {
	PostID         string `json:"post_id"`
	ConversationID string `json:"conversation_id"`
	ThreadID       string `json:"thread_id"`
}

type CancelResult struct {
	InvocationID string `json:"invocation_id"`
	State        string `json:"state"`
}

type FeedbackResult struct {
	InvocationID string `json:"invocation_id"`
	Helpful      bool   `json:"helpful,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Active       bool   `json:"active"`
}

// ActionSurface contains authenticated mutations on an invocation. It is
// separate from Surface so read-only embeddings cannot accidentally expose
// mutation routes merely by serving the directory.
type ActionSurface interface {
	Cancel(context.Context, string, string) (CancelResult, error)
	SubmitFeedback(context.Context, string, bool, string, string) (FeedbackResult, error)
	UndoFeedback(context.Context, string, string) (FeedbackResult, error)
}

// FinalStateConflict reports the terminal execution state without exposing
// run internals. The HTTP boundary serializes it with the conflict response.
type FinalStateConflict struct{ State string }

func (e *FinalStateConflict) Error() string { return ErrConflict.Error() }
func (e *FinalStateConflict) Unwrap() error { return ErrConflict }

// Surface derives tenant and actor exclusively from the verified context.
type Surface interface {
	Directory(context.Context, string) (Directory, error)
	Progress(context.Context, string) (Progress, error)
	Retry(context.Context, string, string) (RetryResult, error)
}
