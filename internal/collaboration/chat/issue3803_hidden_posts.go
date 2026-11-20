package chat

import "context"

// PostAudienceHider is implemented by a store that keeps post audiences. It
// names, among some posts, the ones a person may not read (#3803), by the same
// rule as PostAudienceReader.
type PostAudienceHider interface {
	PostsHiddenFrom(ctx context.Context, tenant, person string, ids []string) (map[string]bool, error)
}

// PostsHiddenFrom says which of the posts a person may not read because they
// were addressed to an audience that leaves the person out. Something that
// stands beside a post (an outcome receipt) is shown only where the post is.
// A store that keeps no audiences hides nothing; a failed read is an error, and
// the caller shows nothing rather than guessing.
func (s *Service) PostsHiddenFrom(ctx context.Context, p Principal, tenant string, ids []string) (map[string]bool, error) {
	if p.SubjectID == "" || tenant == "" {
		return nil, ErrInvalidArgument
	}
	hider, ok := s.store.(PostAudienceHider)
	if !ok {
		return map[string]bool{}, nil
	}
	return hider.PostsHiddenFrom(ctx, tenant, p.SubjectID, ids)
}
