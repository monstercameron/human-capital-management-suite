package chatstore

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var errBackgroundThreadSnapshotRequest = errors.New("chatstore: invalid background thread snapshot request")

// CaptureBackgroundThreadSnapshot reads a durable invocation's current
// invoker-visible thread under tenant RLS. ReaderID is used only in the
// membership/history predicate; this method does not accept or manufacture a
// chat.Principal or trust.Principal.
func (s *Adapter) CaptureBackgroundThreadSnapshot(ctx context.Context, request chat.BackgroundThreadSnapshotRequest) (chat.BackgroundThreadSnapshot, error) {
	if s == nil || s.Store == nil || ctx == nil || strings.TrimSpace(request.TenantID) == "" || strings.TrimSpace(request.ReaderID) == "" ||
		strings.TrimSpace(request.ConversationID) == "" || strings.TrimSpace(request.ThreadID) == "" || strings.TrimSpace(request.InvokingPostID) == "" ||
		request.Limit < 1 || request.Limit > chat.MaxThreadSnapshotPosts {
		return chat.BackgroundThreadSnapshot{}, errBackgroundThreadSnapshotRequest
	}
	if tx, ok := sealedBackgroundTxFromContext(ctx, s.Store, request.TenantID); ok {
		return readBackgroundThreadSnapshot(ctx, tx, request)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return chat.BackgroundThreadSnapshot{}, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, request.TenantID); err != nil {
		return chat.BackgroundThreadSnapshot{}, err
	}
	snapshot, err := readBackgroundThreadSnapshot(ctx, tx, request)
	if err != nil {
		return chat.BackgroundThreadSnapshot{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return chat.BackgroundThreadSnapshot{}, err
	}
	return snapshot, nil
}

func readBackgroundThreadSnapshot(ctx context.Context, tx dbport.Tx, request chat.BackgroundThreadSnapshotRequest) (chat.BackgroundThreadSnapshot, error) {
	snapshot := chat.BackgroundThreadSnapshot{
		TenantID: request.TenantID, ConversationID: request.ConversationID, ThreadID: request.ThreadID, InvokingPostID: request.InvokingPostID,
		ReaderTenantID: request.TenantID, ReaderID: request.ReaderID,
	}
	var revision, authorityRevision int64
	if err := tx.QueryRow(ctx, `SELECT post_sequence,audience_revision FROM chat_conversation WHERE tenant_id=$1 AND id=$2 FOR SHARE`, request.TenantID, request.ConversationID).Scan(&revision, &authorityRevision); err != nil {
		return snapshot, err
	}
	if revision <= 0 || authorityRevision <= 0 {
		return snapshot, chat.ErrUnavailable
	}
	snapshot.Revision, snapshot.AuthorityRevision = uint64(revision), uint64(authorityRevision)
	rows, err := tx.Query(ctx, `SELECT p.id,p.tenant_id,p.conversation_id,p.author_id,p.author_home_tenant_id,p.sequence,p.body,p.revision,p.tombstoned,p.created_at,p.updated_at,p.parent_id,p.references_json,p.source_attribution FROM chat_post p JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.home_tenant_id=$3 AND m.member_id=$4 AND m.state='active' WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND p.created_at>=m.joined_at)) AND p.tombstoned=false AND (p.id=$5 OR p.parent_id=$5) ORDER BY p.sequence DESC LIMIT $6`, request.TenantID, request.ConversationID, request.TenantID, request.ReaderID, request.ThreadID, request.Limit)
	if err != nil {
		return snapshot, err
	}
	defer rows.Close()
	for rows.Next() {
		var row Post
		if err := rows.Scan(&row.ID, &row.TenantID, &row.ConversationID, &row.AuthorID, &row.AuthorHomeTenantID, &row.Sequence, &row.Body, &row.Revision, &row.Tombstoned, &row.CreatedAt, &row.UpdatedAt, &row.ParentID, &row.References, &row.SourceAttribution); err != nil {
			return snapshot, err
		}
		post, err := chatPost(row)
		if err != nil {
			return snapshot, err
		}
		snapshot.Posts = append(snapshot.Posts, post)
	}
	if err := rows.Err(); err != nil {
		return snapshot, err
	}
	if len(snapshot.Posts) == 0 {
		return snapshot, chat.ErrNotFound
	}
	sort.Slice(snapshot.Posts, func(i, j int) bool { return snapshot.Posts[i].Sequence < snapshot.Posts[j].Sequence })
	foundInvoker := false
	seen := make(map[string]struct{}, len(snapshot.Posts))
	for _, post := range snapshot.Posts {
		if post.ID == "" || post.Sequence == 0 || post.Revision == 0 || post.Deleted || (post.ID != request.ThreadID && post.ParentID != request.ThreadID) {
			return snapshot, chat.ErrThreadSnapshotUnavailable
		}
		if _, ok := seen[post.ID]; ok {
			return snapshot, chat.ErrThreadSnapshotUnavailable
		}
		seen[post.ID] = struct{}{}
		if post.ID == request.InvokingPostID && post.AuthorID == request.ReaderID && post.AuthorHomeTenantID == request.TenantID {
			foundInvoker = true
		}
	}
	if !foundInvoker {
		return snapshot, chat.ErrPermissionDenied
	}
	digest, err := chat.BackgroundThreadSnapshotDigest(snapshot)
	if err != nil {
		return snapshot, fmt.Errorf("chatstore: canonicalize background thread snapshot: %w", err)
	}
	snapshot.Digest = digest
	snapshot.SnapshotID = "chat-background-thread-" + digest
	return snapshot, nil
}
