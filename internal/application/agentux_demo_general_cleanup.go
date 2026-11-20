package application

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type AgentUXDemoCleanupChat interface {
	ListPosts(context.Context, chat.ListPostsRequest) (chat.ListPostsResponse, error)
	DeletePost(context.Context, chat.DeletePostRequest) (chat.Post, error)
}

// The authority supplies an authenticated author, never a principal built
// from a post's display fields or an administrator's borrowed roles.
type AgentUXDemoCleanupAuthors interface {
	DemoPostAuthor(context.Context, string, string) (*trust.Principal, error)
}

type AgentUXDemoGeneralCleanup struct {
	Chat    AgentUXDemoCleanupChat
	Authors AgentUXDemoCleanupAuthors
}

// Run tombstones only the specified verification questions and their proven
// Policy Helper descendants, through the chat service as each post's author.
// Its receipt contains identifiers only, including successful partial work.
func (c AgentUXDemoGeneralCleanup) Run(ctx context.Context, admin *trust.Principal, now time.Time) ([]string, error) {
	if ctx == nil || c.Chat == nil || c.Authors == nil || admin == nil || admin.SubjectKind() != trust.SubjectKindHuman || !isLocalAgentDemoTenant(admin.Tenant().String()) || admin.Subject() != localAgentDemoAdminFor(admin.Tenant().String()) || now.IsZero() {
		return nil, chat.ErrPermissionDenied
	}
	conversation := localDevPersonaDemoConversationID(admin.Tenant().String(), "general")
	principal := chat.Principal{TenantID: admin.Tenant().String(), SubjectID: admin.Subject()}
	ctx = trust.WithPrincipal(ctx, admin)
	var posts []chat.Post
	cursor, cursors := "", map[string]bool{}
	for {
		page, err := c.Chat.ListPosts(ctx, chat.ListPostsRequest{Principal: principal, TenantID: principal.TenantID, ConversationID: conversation, Page: chat.Page{Cursor: cursor, PageSize: 100}})
		if err != nil {
			return nil, err
		}
		for _, p := range page.Posts {
			if p.TenantID != principal.TenantID || p.ConversationID != conversation || p.ID == "" || p.Revision == 0 {
				return nil, chat.ErrPermissionDenied
			}
			posts = append(posts, p)
		}
		if page.NextCursor == "" {
			break
		}
		if cursors[page.NextCursor] {
			return nil, chat.ErrUnavailable
		}
		cursor, cursors[page.NextCursor] = page.NextCursor, true
	}
	selected := agentuxDemoVerificationPostsFor(principal.TenantID, posts, now)
	// Resolve every author before deleting anything. An unavailable machine
	// authority leaves the channel intact rather than half-cleaned.
	authors := map[string]*trust.Principal{}
	for _, p := range selected {
		if authors[p.AuthorID] != nil {
			continue
		}
		author, err := c.Authors.DemoPostAuthor(ctx, p.TenantID, p.AuthorID)
		if err != nil {
			return nil, err
		}
		if author == nil || author.Tenant().String() != p.TenantID || author.Subject() != p.AuthorID || !now.Before(author.ExpiresAt()) || (p.AuthorID == localAgentDemoAdminFor(principal.TenantID) && author.SubjectKind() != trust.SubjectKindHuman) || (p.AuthorID != localAgentDemoAdminFor(principal.TenantID) && author.SubjectKind() != trust.SubjectKindAgent && author.SubjectKind() != trust.SubjectKindService) {
			return nil, chat.ErrPermissionDenied
		}
		authors[p.AuthorID] = author
	}
	var removed []string
	for _, p := range selected {
		author := authors[p.AuthorID]
		deleted, err := c.Chat.DeletePost(trust.WithPrincipal(ctx, author), chat.DeletePostRequest{Principal: chat.Principal{TenantID: p.TenantID, SubjectID: author.Subject()}, TenantID: p.TenantID, ConversationID: p.ConversationID, PostID: p.ID, ExpectedRevision: p.Revision})
		if err != nil {
			return removed, err
		}
		if deleted.ID != p.ID || deleted.TenantID != p.TenantID || deleted.ConversationID != p.ConversationID || deleted.AuthorID != p.AuthorID || !deleted.Deleted {
			return removed, errors.Join(chat.ErrConflict, errors.New("demo cleanup returned an unrelated post"))
		}
		removed = append(removed, p.ID)
	}
	return removed, nil
}

func agentuxDemoVerificationPosts(posts []chat.Post, now time.Time) []chat.Post {
	return agentuxDemoVerificationPostsFor(localAgentDemoTenant, posts, now)
}

func agentuxDemoVerificationPostsFor(tenant string, posts []chat.Post, now time.Time) []chat.Post {
	start := time.Date(2026, time.October, 1, 1, 0, 0, 0, time.UTC)
	conversation := localDevPersonaDemoConversationID(tenant, "general")
	selected := map[string]bool{}
	valid := func(p chat.Post) bool {
		return !p.Deleted && p.TenantID == tenant && p.AuthorHomeTenantID == tenant && p.ConversationID == conversation && !p.CreatedAt.Before(start) && !p.CreatedAt.After(now)
	}
	for _, p := range posts {
		if valid(p) && p.AuthorID == localAgentDemoAdminFor(tenant) && (strings.HasPrefix(p.Body, "@Policy Helper") || strings.HasPrefix(p.Body, "@pol")) {
			selected[p.ID] = true
		}
	}
	// Parent linkage, not proximity or text similarity, proves a reply belongs
	// to a selected question. Iteration handles pages arriving in any order.
	for changed := true; changed; {
		changed = false
		for _, p := range posts {
			if valid(p) && (p.AuthorID == localAgentDemoAgentID || p.AuthorID == localAgentDemoPersonaID) && p.ParentID != "" && selected[p.ParentID] && !selected[p.ID] {
				selected[p.ID], changed = true, true
			}
		}
	}
	var result []chat.Post
	for _, p := range posts {
		if selected[p.ID] {
			result = append(result, p)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Sequence > result[j].Sequence })
	return result
}
