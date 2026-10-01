package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errPersonaAtomicThreadReader = errors.New("application: atomic persona thread read unavailable")

// PersonaThreadSnapshotSource exposes Chat's atomic snapshot operation to the
// application adapter without granting it independent visibility authority.
type PersonaThreadSnapshotSource interface {
	CaptureThreadSnapshot(context.Context, chat.ThreadSnapshotRequest) (chat.ThreadSnapshot, error)
}

// PersonaAtomicThreadReader adapts Chat's atomic, current, invoker-authorized
// thread snapshot to the bounded context port consumed by agentinvoke.
type PersonaAtomicThreadReader struct {
	Snapshots PersonaThreadSnapshotSource
}

var _ agentinvoke.ThreadReader = PersonaAtomicThreadReader{}

// ReadThread captures at most 50 posts from the exact thread as the trusted
// principal currently sees it. It rejects snapshots that do not match the
// request instead of falling back to paginated history.
func (r PersonaAtomicThreadReader) ReadThread(ctx context.Context, req agentinvoke.ThreadReadRequest) ([]agentinvoke.ThreadPost, error) {
	if r.Snapshots == nil || ctx == nil || strings.TrimSpace(req.TenantID) == "" ||
		strings.TrimSpace(req.ConversationID) == "" || strings.TrimSpace(req.ThreadID) == "" ||
		strings.TrimSpace(req.InvokingPostID) == "" || strings.TrimSpace(req.InvokerID) == "" {
		return nil, fmt.Errorf("%w: snapshot source and complete thread identity are required", errPersonaAtomicThreadReader)
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.Tenant().String() != req.TenantID || principal.Subject() != req.InvokerID {
		return nil, fmt.Errorf("%w: request identity does not match trusted principal", errPersonaAtomicThreadReader)
	}
	limit := req.Limit
	if limit <= 0 || limit > agentinvoke.MaxThreadPosts {
		limit = agentinvoke.MaxThreadPosts
	}
	reader := chat.Principal{TenantID: principal.Tenant().String(), SubjectID: principal.Subject(), Roles: principal.Roles()}
	snapshot, err := r.Snapshots.CaptureThreadSnapshot(ctx, chat.ThreadSnapshotRequest{
		Principal: reader, TenantID: req.TenantID, ConversationID: req.ConversationID,
		ThreadID: req.ThreadID, InvokingPostID: req.InvokingPostID, Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	if err := validatePersonaThreadSnapshot(snapshot, req, reader, limit); err != nil {
		return nil, err
	}
	posts := make([]agentinvoke.ThreadPost, 0, len(snapshot.Posts))
	for _, post := range snapshot.Posts {
		posts = append(posts, agentinvoke.ThreadPost{
			TenantID: post.TenantID, ConversationID: post.ConversationID, ThreadID: req.ThreadID,
			ID: post.ID, AuthorID: post.AuthorID, Body: post.Body,
		})
	}
	return posts, nil
}

func validatePersonaThreadSnapshot(snapshot chat.ThreadSnapshot, req agentinvoke.ThreadReadRequest, principal chat.Principal, limit int) error {
	if snapshot.TenantID != req.TenantID || snapshot.ConversationID != req.ConversationID ||
		snapshot.ThreadID != req.ThreadID || snapshot.InvokingPostID != req.InvokingPostID ||
		snapshot.PrincipalTenantID != principal.TenantID || snapshot.PrincipalID != principal.SubjectID ||
		!samePersonaThreadStrings(snapshot.PrincipalRoles, principal.Roles) ||
		!samePersonaThreadStrings(snapshot.PrincipalQualifications, principal.Qualifications) ||
		len(snapshot.Posts) == 0 || len(snapshot.Posts) > limit || len(snapshot.Posts) > agentinvoke.MaxThreadPosts {
		return fmt.Errorf("%w: snapshot scope or size does not match request", errPersonaAtomicThreadReader)
	}
	digest, err := chat.ThreadSnapshotDigest(snapshot)
	if err != nil || digest != snapshot.Digest || snapshot.SnapshotID != "chat-thread-"+digest {
		return fmt.Errorf("%w: snapshot digest is invalid", errPersonaAtomicThreadReader)
	}
	foundInvoker := false
	seen := make(map[string]struct{}, len(snapshot.Posts))
	for _, post := range snapshot.Posts {
		if post.TenantID != req.TenantID || post.ConversationID != req.ConversationID || post.Deleted ||
			(post.ID != req.ThreadID && post.ParentID != req.ThreadID) || strings.TrimSpace(post.ID) == "" {
			return fmt.Errorf("%w: snapshot contains a post outside the requested thread", errPersonaAtomicThreadReader)
		}
		if _, exists := seen[post.ID]; exists {
			return fmt.Errorf("%w: snapshot contains a duplicate post", errPersonaAtomicThreadReader)
		}
		seen[post.ID] = struct{}{}
		if post.ID == req.InvokingPostID && post.AuthorID == req.InvokerID {
			foundInvoker = true
		}
	}
	if !foundInvoker {
		return fmt.Errorf("%w: invoking post is not present in the authorized snapshot", errPersonaAtomicThreadReader)
	}
	return nil
}

func samePersonaThreadStrings(left, right []string) bool {
	left = slices.Clone(left)
	right = slices.Clone(right)
	sort.Strings(left)
	sort.Strings(right)
	return slices.Equal(left, right)
}
