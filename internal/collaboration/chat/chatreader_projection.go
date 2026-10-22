package chat

import (
	"context"
	"strings"
)

// ReaderBody is what one reader may be shown instead of a post's stored body.
// Replaced is false when the reader may read the post as written.
type ReaderBody struct {
	Text     string
	Replaced bool
}

// ReaderBodyProjector decides, for one reader, which of the posts they may not
// read as written (a channel whose administrator does not let members view
// messages as written) and what stands in for them: the reworded text when it
// is ready, otherwise a neutral line. The stored post is never changed.
//
// ProjectReaderBodies returns one entry per post, in order, and never fails
// open: a post it cannot decide about is replaced, not passed through.
type ReaderBodyProjector interface {
	ProjectReaderBodies(ctx context.Context, reader Principal, posts []Post) []ReaderBody
}

// SetReaderBodyProjector installs the projector every reader view of a post
// body asks (history, search hits, pins, saved messages, links and the live
// stream). It is intended for composition roots and tests.
func (s *Service) SetReaderBodyProjector(p ReaderBodyProjector) { s.readerBodies = p }

// projectReaderBodies asks the projector about the stored bodies, before any
// mask is applied, and returns its answer; nil when none is installed.
func (s *Service) projectReaderBodies(ctx context.Context, reader Principal, posts []Post) []ReaderBody {
	if s.readerBodies == nil || len(posts) == 0 {
		return nil
	}
	out := s.readerBodies.ProjectReaderBodies(ctx, reader, posts)
	if len(out) != len(posts) {
		// An answer that does not line up with the posts cannot be trusted for
		// any of them.
		out = make([]ReaderBody, len(posts))
		for i := range posts {
			out[i] = ReaderBody{Replaced: !posts[i].Deleted && strings.TrimSpace(posts[i].Body) != ""}
		}
	}
	return out
}

// HideUnreadableOriginal is the reader's view of one post for a caller that
// reads a post by its identity (a project link, a share link): the stored body
// is replaced when this reader may not read it as written.
func (s *Service) HideUnreadableOriginal(ctx context.Context, reader Principal, post Post) Post {
	answers := s.projectReaderBodies(ctx, reader, []Post{post})
	if len(answers) == 1 && answers[0].Replaced {
		post.Body = answers[0].Text
	}
	return post
}

// readerMayReadAsWritten reports whether the reader may be shown the post's
// stored body, for an operation that would copy the body elsewhere.
func (s *Service) readerMayReadAsWritten(ctx context.Context, reader Principal, post Post) bool {
	answers := s.projectReaderBodies(ctx, reader, []Post{post})
	return len(answers) != 1 || !answers[0].Replaced
}

// searchWordsShown reports whether every word of a search query is in the text
// a reader is shown. A hit whose words are only in a body the reader may not
// read is no hit for them.
func searchWordsShown(shown, query string) bool {
	shown = strings.ToLower(shown)
	words := strings.Fields(strings.ToLower(query))
	for _, word := range words {
		word = strings.Trim(word, `.,;:!?"'()[]{}`)
		if word != "" && !strings.Contains(shown, word) {
			return false
		}
	}
	return len(words) > 0
}
