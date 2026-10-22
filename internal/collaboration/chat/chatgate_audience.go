package chat

import "context"

// CHATGATE-010 and CHATGATE-011: content addressed to the segments of a channel
// gate. A segment is a set of members defined by their answers; the gate owns
// it, and Chat only asks about it through the ports below.

// PostAudienceRequest asks for a post to be addressed to segments. Restricted
// makes it readable only by the segments' members at the moment it is sent and
// the author; otherwise every member can read it and it only says who it is for.
type PostAudienceRequest struct {
	Segments   []string
	Restricted bool
}

// PostAudience is a post's audience as it is stored with the post: Members is
// the people who may read a restricted post, fixed when it is sent.
type PostAudience struct {
	Segments   []string
	Restricted bool
	Members    []string
}

// SegmentAudiencePort is the gate's segments as the message path needs them.
type SegmentAudiencePort interface {
	// Resolve decides whether the principal may address the segments, in the
	// channel's own levels, and returns the audience to store.
	Resolve(ctx context.Context, p Principal, tenant, conversation string, segments []string, restricted bool) (PostAudience, error)
	// Signature is which of the audience's segments a person is in.
	Signature(ctx context.Context, tenant, conversation, person string, audience []string) ([]string, error)
}

// PostAudienceReader is implemented by a store that keeps post audiences. A
// store without it keeps none, and every post is readable by every member.
type PostAudienceReader interface {
	// PostReadableBy reports whether the person may read the post under its
	// audience. A post with no audience, or one that is not restricted, is
	// readable by anyone the history rules let read it.
	PostReadableBy(ctx context.Context, tenant, conversation, post, person string) (bool, error)
}

type postAudienceKey struct{}

// WithPostAudience carries a resolved audience to the store, which writes it in
// the same transaction as the post: there is no moment the post exists and its
// restriction does not.
func WithPostAudience(ctx context.Context, a PostAudience) context.Context {
	return context.WithValue(ctx, postAudienceKey{}, a)
}

// PostAudienceFrom is the audience a context carries.
func PostAudienceFrom(ctx context.Context) (PostAudience, bool) {
	a, ok := ctx.Value(postAudienceKey{}).(PostAudience)
	return a, ok && len(a.Segments) > 0
}

// SetSegmentAudience installs the gate's segments. Without it a post cannot be
// addressed, and a card addressed to segments cannot be voted on.
func (s *Service) SetSegmentAudience(p SegmentAudiencePort) { s.segments = p }

func (s *Service) resolvePostAudience(ctx context.Context, r SendPostRequest) (PostAudience, error) {
	if s.segments == nil {
		return PostAudience{}, ErrUnavailable
	}
	return s.segments.Resolve(ctx, r.Principal, r.TenantID, r.ConversationID, r.Audience.Segments, r.Audience.Restricted)
}

// postAudienceAllows is the one audience check every read path that works on a
// single post asks (references, pins, saved items, replies). The list and search
// queries apply the same rule in SQL (chatstore chatgate_audience.go). A failed
// check hides the post: it never shows one.
func (s *Service) postAudienceAllows(ctx context.Context, p Principal, post Post) bool {
	reader, ok := s.store.(PostAudienceReader)
	if !ok {
		return true
	}
	allowed, err := reader.PostReadableBy(ctx, post.TenantID, post.ConversationID, post.ID, p.SubjectID)
	return err == nil && allowed
}
