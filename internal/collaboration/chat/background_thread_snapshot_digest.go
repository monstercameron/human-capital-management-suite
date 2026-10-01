package chat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

var ErrBackgroundThreadSnapshotUnavailable = errors.New("chat background thread snapshot unavailable")

// BackgroundThreadSnapshotDigest hashes the exact tenant, reader, revisions,
// and ordered canonical post image returned by the tenant-scoped chat store.
func BackgroundThreadSnapshotDigest(snapshot BackgroundThreadSnapshot) (string, error) {
	if snapshot.TenantID == "" || snapshot.ConversationID == "" || snapshot.ThreadID == "" || snapshot.InvokingPostID == "" ||
		snapshot.ReaderTenantID != snapshot.TenantID || snapshot.ReaderID == "" || len(snapshot.Posts) == 0 {
		return "", ErrBackgroundThreadSnapshotUnavailable
	}
	type canonicalPost struct {
		ID, TenantID, ConversationID, AuthorID, AuthorHomeTenantID, Body, ParentID string
		Sequence, Revision                                                         uint64
	}
	posts := make([]canonicalPost, 0, len(snapshot.Posts))
	for _, post := range snapshot.Posts {
		if post.TenantID != snapshot.TenantID || post.ConversationID != snapshot.ConversationID || post.ID == "" || post.Revision == 0 || post.Deleted {
			return "", fmt.Errorf("%w: malformed or foreign post", ErrBackgroundThreadSnapshotUnavailable)
		}
		posts = append(posts, canonicalPost{ID: post.ID, TenantID: post.TenantID, ConversationID: post.ConversationID, AuthorID: post.AuthorID, AuthorHomeTenantID: post.AuthorHomeTenantID, Body: post.Body, ParentID: post.ParentID, Sequence: post.Sequence, Revision: post.Revision})
	}
	canonical := struct {
		TenantID, ConversationID, ThreadID, InvokingPostID, ReaderTenantID, ReaderID string
		Revision, AuthorityRevision                                                  uint64
		Posts                                                                        []canonicalPost
	}{snapshot.TenantID, snapshot.ConversationID, snapshot.ThreadID, snapshot.InvokingPostID, snapshot.ReaderTenantID, snapshot.ReaderID, snapshot.Revision, snapshot.AuthorityRevision, posts}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
