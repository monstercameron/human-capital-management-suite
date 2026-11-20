package chat

import (
	"context"
	"strings"
)

// maskForwardNotes applies the reader's content mask to the sharer's note on
// forwarded posts (issue 3919). The note is stored beside the copy, not in the
// body, so a mask that covers message text would otherwise let the same word
// through in the note. It works on copies: the stored attribution is shared and
// is never changed.
func (s *Service) maskForwardNotes(ctx context.Context, reader Principal, posts []Post) {
	var index []int
	var notes []Post
	for i := range posts {
		attribution := posts[i].SourceAttribution
		if attribution == nil || strings.TrimSpace(attribution.Note) == "" {
			continue
		}
		index = append(index, i)
		notes = append(notes, Post{ID: posts[i].ID, TenantID: posts[i].TenantID, ConversationID: posts[i].ConversationID, AuthorID: posts[i].AuthorID, AuthorHomeTenantID: posts[i].AuthorHomeTenantID, Body: attribution.Note, Deleted: posts[i].Deleted})
	}
	if len(notes) == 0 {
		return
	}
	s.maskByContentPolicy(ctx, reader, notes)
	for k, i := range index {
		masked := *posts[i].SourceAttribution
		masked.Note = notes[k].Body
		posts[i].SourceAttribution = &masked
	}
}
