package chat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var ErrThreadSnapshotUnavailable = errors.New("chat thread snapshot unavailable")

const MaxThreadSnapshotPosts = 50

// ThreadSnapshotRequest identifies one authenticated reader and one bounded
// thread. Tenant and principal values are checked by Service before the store
// receives the request.
type ThreadSnapshotRequest struct {
	Principal                Principal
	TenantID, ConversationID string
	ThreadID, InvokingPostID string
	Limit                    int
}

// ThreadSnapshot is the canonical, server-owned image of the posts visible to
// one reader at a single chat-store revision.
type ThreadSnapshot struct {
	TenantID, ConversationID, ThreadID      string
	InvokingPostID                          string
	PrincipalTenantID, PrincipalID          string
	PrincipalRoles, PrincipalQualifications []string
	Revision, AuthorityRevision             uint64
	Posts                                   []Post
	SnapshotID, Digest                      string
}

// ThreadSnapshotStore is an optional store extension. Implementations must
// read and fence the complete bounded thread in one tenant-scoped transaction.
type ThreadSnapshotStore interface {
	CaptureThreadSnapshot(context.Context, ThreadSnapshotRequest) (ThreadSnapshot, error)
	WithThreadSnapshotFence(context.Context, ThreadSnapshot, func() error) error
}

// CaptureThreadSnapshot returns one atomic, current, reader-authorized thread
// image. It refuses stores that implement only the paginated history API.
func (s *Service) CaptureThreadSnapshot(ctx context.Context, request ThreadSnapshotRequest) (ThreadSnapshot, error) {
	if s == nil || s.store == nil || ctx == nil || validatePrincipal(request.Principal, request.TenantID) != nil ||
		strings.TrimSpace(request.ConversationID) == "" || strings.TrimSpace(request.ThreadID) == "" || strings.TrimSpace(request.InvokingPostID) == "" || request.Limit < 1 || request.Limit > MaxThreadSnapshotPosts {
		return ThreadSnapshot{}, ErrThreadSnapshotUnavailable
	}
	if _, err := s.GetConversation(ctx, GetConversationRequest{Principal: request.Principal, TenantID: request.TenantID, ConversationID: request.ConversationID}); err != nil {
		return ThreadSnapshot{}, err
	}
	store, ok := s.store.(ThreadSnapshotStore)
	if !ok {
		return ThreadSnapshot{}, ErrThreadSnapshotUnavailable
	}
	return store.CaptureThreadSnapshot(ctx, request)
}

// WithThreadSnapshotFence runs fn while chat holds the conversation and
// snapshot post locks. A concurrent post, edit, delete, or membership change
// cannot commit between validation and fn's durable admission write.
func (s *Service) WithThreadSnapshotFence(ctx context.Context, snapshot ThreadSnapshot, fn func() error) error {
	if s == nil || s.store == nil || ctx == nil || fn == nil || snapshot.Digest == "" || snapshot.SnapshotID == "" {
		return ErrThreadSnapshotUnavailable
	}
	digest, err := ThreadSnapshotDigest(snapshot)
	if err != nil || digest != snapshot.Digest || snapshot.SnapshotID != "chat-thread-"+digest {
		return ErrThreadSnapshotUnavailable
	}
	principal := Principal{TenantID: snapshot.PrincipalTenantID, SubjectID: snapshot.PrincipalID, Roles: append([]string(nil), snapshot.PrincipalRoles...), Qualifications: append([]string(nil), snapshot.PrincipalQualifications...)}
	if validatePrincipal(principal, snapshot.TenantID) != nil {
		return ErrThreadSnapshotUnavailable
	}
	if _, err := s.GetConversation(ctx, GetConversationRequest{Principal: principal, TenantID: snapshot.TenantID, ConversationID: snapshot.ConversationID}); err != nil {
		return err
	}
	store, ok := s.store.(ThreadSnapshotStore)
	if !ok {
		return ErrThreadSnapshotUnavailable
	}
	return store.WithThreadSnapshotFence(ctx, snapshot, fn)
}

// ThreadSnapshotDigest computes the canonical digest of a snapshot's exact
// reader, scope, revision, and ordered post revisions.
func ThreadSnapshotDigest(snapshot ThreadSnapshot) (string, error) {
	if snapshot.TenantID == "" || snapshot.ConversationID == "" || snapshot.ThreadID == "" || snapshot.InvokingPostID == "" || snapshot.PrincipalTenantID == "" || snapshot.PrincipalID == "" || len(snapshot.Posts) == 0 {
		return "", ErrThreadSnapshotUnavailable
	}
	type canonicalPost struct {
		ID, TenantID, ConversationID, AuthorID, AuthorHomeTenantID, Body, ParentID string
		Sequence, Revision                                                         uint64
	}
	posts := make([]canonicalPost, 0, len(snapshot.Posts))
	for _, post := range snapshot.Posts {
		if post.TenantID != snapshot.TenantID || post.ConversationID != snapshot.ConversationID || post.ID == "" || post.Revision == 0 {
			return "", fmt.Errorf("%w: malformed or foreign post", ErrThreadSnapshotUnavailable)
		}
		posts = append(posts, canonicalPost{ID: post.ID, TenantID: post.TenantID, ConversationID: post.ConversationID, AuthorID: post.AuthorID, AuthorHomeTenantID: post.AuthorHomeTenantID, Body: post.Body, ParentID: post.ParentID, Sequence: post.Sequence, Revision: post.Revision})
	}
	roles := append([]string(nil), snapshot.PrincipalRoles...)
	qualifications := append([]string(nil), snapshot.PrincipalQualifications...)
	sort.Strings(roles)
	sort.Strings(qualifications)
	canonical := struct {
		TenantID, ConversationID, ThreadID, InvokingPostID, PrincipalTenantID, PrincipalID string
		PrincipalRoles, PrincipalQualifications                                            []string
		Revision, AuthorityRevision                                                        uint64
		Posts                                                                              []canonicalPost
	}{snapshot.TenantID, snapshot.ConversationID, snapshot.ThreadID, snapshot.InvokingPostID, snapshot.PrincipalTenantID, snapshot.PrincipalID, roles, qualifications, snapshot.Revision, snapshot.AuthorityRevision, posts}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
