package chat

import "context"

// CHATGATE-014: what a reader is told of a post's audience, for the quiet tag
// under a message: who it is for, and for a restricted one how many people could
// read it when it was sent.

// PostAudienceInfo is the audience of one post as one reader may know it.
type PostAudienceInfo struct {
	Segments   []string
	Restricted bool
	// People is how many people a restricted post was delivered to, its author
	// included; zero for a post every member can read.
	People int
}

// PostAudienceInfoReader is implemented by a store that keeps post audiences. It
// answers only for posts the person may read, so an audience is never told of a
// post the reader cannot open.
type PostAudienceInfoReader interface {
	PostAudienceInfos(ctx context.Context, tenant, person string, ids []string) (map[string]PostAudienceInfo, error)
}

// PostAudienceLister is the transport's read of the tags of a page of posts.
type PostAudienceLister interface {
	PostAudiences(ctx context.Context, p Principal, tenant string, ids []string) map[string]PostAudienceInfo
}

// PostAudiences tells the audience of the posts that have one. It is best effort
// by contract, like the author names beside it: a read that fails leaves the
// tags out and never fails the answer that carries the posts.
func (s *Service) PostAudiences(ctx context.Context, p Principal, tenant string, ids []string) map[string]PostAudienceInfo {
	reader, ok := s.store.(PostAudienceInfoReader)
	if !ok || len(ids) == 0 || p.SubjectID == "" || tenant == "" {
		return nil
	}
	out, err := reader.PostAudienceInfos(ctx, tenant, p.SubjectID, ids)
	if err != nil {
		return nil
	}
	return out
}
