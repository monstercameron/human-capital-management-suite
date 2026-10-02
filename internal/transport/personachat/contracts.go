// Package personachat is the HTTP boundary for authorized persona chat views.
package personachat

import (
	"context"
	"errors"
	"time"
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
	// Examples are the example questions of the agent's published definition,
	// offered in an empty conversation with it (CHATUX-024).
	Examples []string `json:"examples,omitempty"`
	// DocumentScope says which documents the agent reads in this conversation:
	// CHANNEL_DOCUMENTS, WORKSPACE_DOCUMENTS or NO_DOCUMENTS (AGENTUX-064).
	DocumentScope string `json:"document_scope,omitempty"`
}

type Directory struct {
	Personas   []Profile   `json:"personas"`
	PostActors []PostActor `json:"post_actors"`
	// ChannelPrivacy is the channel's requirement on agent answers, when the
	// conversation is a channel the server can read it for (AGENTUX-070).
	ChannelPrivacy *ChannelPrivacy `json:"channel_privacy,omitempty"`
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
	// Feedback is the asker's own stored rating of the answer, "helpful" or
	// "not-right", and empty when they hold none (CHATBUG-066).
	Feedback string `json:"feedback,omitempty"`
	// StepKind is what the run is doing now, as a typed word the page turns into a
	// sentence in the person's language: "reading_question", "searching",
	// "reading_document", "reading_documents", "writing" or "working"
	// (AGENTUX-075). StepSubject names the thing it is working on, when there is
	// one (a document's title); it is for the invoker only, like the rest of this
	// row.
	StepKind    string `json:"step_kind,omitempty"`
	StepSubject string `json:"step_subject,omitempty"`
	// ElapsedSeconds is how long a run that is still going has been going, from
	// its admission to this read, by the server's clock (AGENTUX-026). The page
	// counts on from it, so a reload shows the run's real age and not zero.
	ElapsedSeconds int `json:"elapsed_seconds,omitempty"`
}

type Progress struct {
	Invocations []Invocation `json:"invocations"`
	// Answers are the caller's own stored private answers in the conversation.
	// They are sent with the first read of a conversation's activity only
	// (CHATBUG-040), so a finished question is drawn answered at once; an answer
	// delivered later arrives on the conversation's event stream.
	Answers []PrivateAnswer `json:"answers,omitempty"`
}

// PrivateAnswer is one private answer as its recipient's page holds it. It has
// the fields of the event stream's private delivery and nothing else: no
// recipient, and no identifier of the saved copy.
type PrivateAnswer struct {
	ID         string    `json:"id"`
	ThreadID   string    `json:"thread_id"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	ThreadLink string    `json:"thread_link,omitempty"`
	// InvocationID is the caller's own run the answer belongs to.
	InvocationID string `json:"invocation_id,omitempty"`
	// SharedPostID is the message the caller shared this answer to the channel
	// as, while that message stands (CHATUX-026). Empty for an answer that was
	// never shared, or whose shared copy was removed.
	SharedPostID string `json:"shared_post_id,omitempty"`
}

// OpeningSurface is a surface that can answer the first read of a
// conversation's activity with the caller's stored private answers beside it.
// The reads that follow (the watch's once-a-second comparison) stay Progress.
type OpeningSurface interface {
	OpeningProgress(context.Context, string) (Progress, error)
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
// Detail is an optional name the state refers to, such as the source that
// keeps an answer from being shared; it is shown to the caller as given.
type FinalStateConflict struct{ State, Detail string }

func (e *FinalStateConflict) Error() string { return ErrConflict.Error() }
func (e *FinalStateConflict) Unwrap() error { return ErrConflict }

// Surface derives tenant and actor exclusively from the verified context.
type Surface interface {
	Directory(context.Context, string) (Directory, error)
	Progress(context.Context, string) (Progress, error)
	Retry(context.Context, string, string) (RetryResult, error)
}
