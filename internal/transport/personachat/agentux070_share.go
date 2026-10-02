package personachat

import "context"

// ShareResult names the message an answer was shared as.
type ShareResult struct {
	InvocationID string `json:"invocation_id"`
	PostID       string `json:"post_id"`
}

// ShareSurface posts a private answer to the channel it was asked in, for the
// person who asked. It is separate from ActionSurface so an embedding that
// cannot share does not have to carry the method. A refusal that says why
// (the agent answers privately, a source is not open to every member) is a
// FinalStateConflict whose State is the reason.
type ShareSurface interface {
	ShareAnswer(ctx context.Context, invocationID, idempotencyKey string) (ShareResult, error)
}
