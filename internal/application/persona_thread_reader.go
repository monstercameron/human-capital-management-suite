package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	errPersonaThreadReaderInvalid = errors.New("persona thread reader: invalid request")
	errPersonaThreadReaderNoAuth  = errors.New("persona thread reader: authenticated principal required")
)

// PersonaThreadReader adapts Chat's current, principal-scoped history read to
// the persona invocation port. It has no authority of its own: Chat remains
// the owner of membership, history visibility, and post disclosure.
type PersonaThreadReader struct {
	Chat chat.ConversationService
}

// PersonaThreadReadResult contains the authorized thread posts and immutable
// source references for each returned post. Provenance is kept beside the
// agentinvoke projection because that port predates retrieval envelopes.
type PersonaThreadReadResult struct {
	Posts      []agentinvoke.ThreadPost
	Provenance []PersonaThreadPostProvenance
}

// PersonaThreadPostProvenance identifies the exact Chat revision represented
// by a returned post. It is a locator and digest input, never an authority.
type PersonaThreadPostProvenance struct {
	Owner          string
	TenantID       string
	ConversationID string
	PostID         string
	Version        uint64
	Citation       string
	Source         *chat.SourceAttribution
}

var _ agentinvoke.ThreadReader = PersonaThreadReader{}

// ReadThread reads at most the last 50 posts in one thread. The authenticated
// principal is taken from ctx; request fields cannot select another authority.
func (r PersonaThreadReader) ReadThread(ctx context.Context, req agentinvoke.ThreadReadRequest) ([]agentinvoke.ThreadPost, error) {
	result, err := r.ReadThreadWithProvenance(ctx, req)
	if err != nil {
		return nil, err
	}
	return result.Posts, nil
}

// ReadThreadWithProvenance reads a thread through Chat and returns source
// references for the exact revisions disclosed by Chat.
func (r PersonaThreadReader) ReadThreadWithProvenance(ctx context.Context, req agentinvoke.ThreadReadRequest) (PersonaThreadReadResult, error) {
	if r.Chat == nil {
		return PersonaThreadReadResult{}, fmt.Errorf("%w: chat service is required", errPersonaThreadReaderInvalid)
	}
	if strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.ConversationID) == "" || strings.TrimSpace(req.ThreadID) == "" || strings.TrimSpace(req.InvokerID) == "" {
		return PersonaThreadReadResult{}, fmt.Errorf("%w: tenant, conversation, thread, and invoker are required", errPersonaThreadReaderInvalid)
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil {
		return PersonaThreadReadResult{}, errPersonaThreadReaderNoAuth
	}
	if principal.Tenant().String() != req.TenantID || principal.Subject() != req.InvokerID {
		return PersonaThreadReadResult{}, fmt.Errorf("%w: request identity does not match trusted principal", errPersonaThreadReaderNoAuth)
	}

	limit := req.Limit
	if limit <= 0 || limit > agentinvoke.MaxThreadPosts {
		limit = agentinvoke.MaxThreadPosts
	}
	pageCursor := ""
	all := make([]chat.Post, 0, limit)
	seenCursors := map[string]struct{}{}
	for {
		if _, seen := seenCursors[pageCursor]; seen {
			return PersonaThreadReadResult{}, fmt.Errorf("%w: chat returned a repeated page cursor", errPersonaThreadReaderInvalid)
		}
		seenCursors[pageCursor] = struct{}{}
		response, err := r.Chat.ListPosts(ctx, chat.ListPostsRequest{
			Principal: chat.Principal{TenantID: principal.Tenant().String(), SubjectID: principal.Subject(), Roles: principal.Roles()},
			TenantID:  req.TenantID, ConversationID: req.ConversationID,
			Descending: true, Page: chat.Page{Cursor: pageCursor, PageSize: 200},
		})
		if err != nil {
			return PersonaThreadReadResult{}, err
		}
		for _, post := range response.Posts {
			if post.TenantID != req.TenantID || post.ConversationID != req.ConversationID || post.Deleted {
				continue
			}
			if inPersonaThread(post, req.ThreadID) {
				all = append(all, post)
				if len(all) == limit {
					return threadResult(all, req.ThreadID), nil
				}
			}
		}
		if response.NextCursor == "" {
			return threadResult(all, req.ThreadID), nil
		}
		pageCursor = response.NextCursor
	}
}

func inPersonaThread(post chat.Post, threadID string) bool {
	return post.ID == threadID || post.ParentID == threadID
}

func threadResult(posts []chat.Post, threadID string) PersonaThreadReadResult {
	result := PersonaThreadReadResult{Posts: make([]agentinvoke.ThreadPost, 0, len(posts)), Provenance: make([]PersonaThreadPostProvenance, 0, len(posts))}
	for i := len(posts) - 1; i >= 0; i-- {
		post := posts[i]
		result.Posts = append(result.Posts, agentinvoke.ThreadPost{TenantID: post.TenantID, ConversationID: post.ConversationID, ThreadID: threadID, ID: post.ID, AuthorID: post.AuthorID, Body: post.Body})
		result.Provenance = append(result.Provenance, PersonaThreadPostProvenance{Owner: "chat", TenantID: post.TenantID, ConversationID: post.ConversationID, PostID: post.ID, Version: post.Revision, Citation: fmt.Sprintf("chat://%s/%s#%s", post.TenantID, post.ConversationID, post.ID), Source: clonePersonaSource(post.SourceAttribution)})
	}
	return result
}

func clonePersonaSource(source *chat.SourceAttribution) *chat.SourceAttribution {
	if source == nil {
		return nil
	}
	copy := *source
	return &copy
}
