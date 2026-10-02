package application

import (
	"context"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// CHATMOD-004: the Remove dialog offers the messages around the one it was
// opened on, so a moderator can tick several and remove them together. They
// are read through the chat service as the moderator, so the list holds only
// what that person may read in the conversation; the removal command checks
// every ticked message again when it counts and when it removes.

const (
	// chatmod004Newer is how many messages after the target are offered and
	// chatmod004Window how many in all.
	chatmod004Newer  = 10
	chatmod004Window = 30
)

// chatmod004Choices lists the target and its neighbours, oldest first. A
// message already removed or deleted is left out: there is nothing to remove.
// A read that fails leaves the dialog with its other two forms; the list is a
// convenience, not a precondition for removing the one message.
func (h ChatModerationPageHTTP) chatmod004Choices(ctx context.Context, p chat.Principal, target chat.Post) []chatui.ModerationChoice {
	if h.Context == nil || target.ID == "" {
		return nil
	}
	page, err := h.Context.ListPosts(ctx, chat.ListPostsRequest{Principal: p, TenantID: p.TenantID, ConversationID: target.ConversationID,
		Descending: true, BeforeSequence: target.Sequence + chatmod004Newer + 1, Page: chat.Page{PageSize: chatmod004Window}})
	if err != nil {
		return nil
	}
	posts := make([]chat.Post, 0, len(page.Posts)+1)
	found := false
	for _, post := range page.Posts {
		if post.Deleted || post.Body == chat.RemovedByAdministrator || post.ConversationID != target.ConversationID {
			continue
		}
		found = found || post.ID == target.ID
		posts = append(posts, post)
	}
	if !found {
		// A reply in a thread is not in the conversation's own page.
		posts = append(posts, target)
	}
	sort.SliceStable(posts, func(i, j int) bool { return posts[i].Sequence < posts[j].Sequence })
	names := map[string]string{}
	if h.Directory != nil {
		ids := make([]string, 0, len(posts))
		for _, post := range posts {
			ids = append(ids, post.AuthorID)
		}
		if read, e := h.Directory.ModerationNames(ctx, p, p.TenantID, ids); e == nil {
			names = read
		}
	}
	out := make([]chatui.ModerationChoice, 0, len(posts))
	for _, post := range posts {
		out = append(out, chatui.ModerationChoice{ID: post.ID, AuthorName: names[post.AuthorID], Body: post.Body, At: post.CreatedAt})
	}
	return out
}
