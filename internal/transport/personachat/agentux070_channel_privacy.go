package personachat

import "context"

// ChannelPrivacy is the channel's requirement on how agents answer in it
// (AGENTUX-070). Private is true when the channel's administrator requires every
// agent answer there to be private to the person who asked. CanChange is true
// for the viewer who may change that: a manager of the channel. It is sent with
// the conversation's agent directory, so the page draws the setting with the
// agents it applies to.
type ChannelPrivacy struct {
	Private   bool `json:"private"`
	CanChange bool `json:"can_change"`
}

// ChannelPrivacySurface changes the requirement. It is separate from Surface so
// a read-only embedding cannot expose the route merely by serving the directory.
// The surface derives the actor from the verified context and refuses anyone who
// does not manage the channel.
type ChannelPrivacySurface interface {
	SetChannelPrivacy(ctx context.Context, conversationID string, private bool) (ChannelPrivacy, error)
}
