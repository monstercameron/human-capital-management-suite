package application

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// CHATUX-026: once the asker shares a private answer, its card says "Shared
// with #channel" instead of "Only visible to you". The page that shared it
// knows; a page loaded later has to be told. The receipt of a delivery is
// immutable and holds no word about sharing, so the copy is found where it
// lives: a message the asker posted under the question whose text is the
// card's text. When the asker removes that message the card is private again,
// with nothing else to undo.

// personaShareComparable is the text of a private card, or of the message it
// was shared as, without what differs between the two and between readers: the
// line that says why the card is private, and the per-reader mark on each
// Sources line.
func personaShareComparable(body string) string {
	cleaned, _ := chat.SplitPrivateReason(body)
	return strings.TrimSpace(personaShareReadableFlag.ReplaceAllString(cleaned, ""))
}

// sharedAnswerCopies finds the message each of the asker's private answers was
// shared as, by answer id. Only a message that still stands counts: one that
// was deleted is not a shared copy. Sharing happens in public channels only, so
// nothing is read anywhere else.
func (s *PersonaChatSurface) sharedAnswerCopies(ctx context.Context, asker string, room chat.Conversation, receipts []agentinvocationstore.ReplyReceipt, answers []personachat.PrivateAnswer) (map[string]string, error) {
	shared := make(map[string]string)
	if room.Kind != chat.PublicChannel || len(answers) == 0 {
		return shared, nil
	}
	threads := make(map[string]string, len(receipts))
	for _, receipt := range receipts {
		if receipt.InvokerID == asker && receipt.ConversationID == room.ID && receipt.EphemeralPostID != "" {
			threads[receipt.EphemeralPostID] = receipt.ThreadID
		}
	}
	// wanted maps "the thread the copy sits in, and its text" to the answer.
	wanted := make(map[string]string, len(answers))
	for _, answer := range answers {
		if thread := threads[answer.ID]; thread != "" {
			wanted[thread+"\x00"+personaShareComparable(answer.Body)] = answer.ID
		}
	}
	principal := chat.Principal{TenantID: room.TenantID, SubjectID: asker}
	page := chat.Page{PageSize: 200}
	seen := make(map[string]bool)
	for len(wanted) > 0 {
		posts, err := s.Chat.ListPosts(ctx, chat.ListPostsRequest{Principal: principal, TenantID: room.TenantID, ConversationID: room.ID, Page: page})
		if err != nil {
			return nil, surfaceChatError(err)
		}
		for _, post := range posts.Posts {
			if post.Deleted || post.ParentID == "" || post.AuthorID != asker || post.TenantID != room.TenantID || post.ConversationID != room.ID {
				continue
			}
			if answerID, is := wanted[post.ParentID+"\x00"+personaShareComparable(post.Body)]; is {
				shared[answerID] = post.ID
			}
		}
		if posts.NextCursor == "" {
			break
		}
		if seen[posts.NextCursor] {
			return nil, personachat.ErrUnavailable
		}
		seen[posts.NextCursor], page.Cursor = true, posts.NextCursor
	}
	return shared, nil
}
