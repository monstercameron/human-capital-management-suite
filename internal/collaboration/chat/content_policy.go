package chat

import "context"

// ContentInput is what a content policy sees for one post body: the author,
// the conversation it is written into, the body after trimming and the
// references it will commit. Edit is true when the body replaces an existing
// revision rather than starting a new post.
type ContentInput struct {
	Principal    Principal
	Conversation Conversation
	Body         string
	References   []Reference
	Edit         bool
}

// ContentPolicy owns DLP and classification decisions for post bodies. When
// configured it is consulted for every new post and every edit before the
// store is touched, so an edit cannot slip content past the check a new post
// would face. A refusal is returned to the caller unchanged; a nil policy
// allows every body, as posting always has.
type ContentPolicy interface {
	CheckContent(context.Context, ContentInput) error
}

// SetContentPolicy installs the DLP/classification policy used by subsequent
// sends and edits. It is intended for composition roots and tests.
func (s *Service) SetContentPolicy(p ContentPolicy) { s.contentPolicy = p }

func (s *Service) checkContent(ctx context.Context, in ContentInput) error {
	if s.contentPolicy == nil {
		return nil
	}
	return s.contentPolicy.CheckContent(ctx, in)
}
