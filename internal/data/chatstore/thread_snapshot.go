package chatstore

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var ErrThreadSnapshotChanged = errors.New("chat thread snapshot changed")

// CaptureThreadSnapshot reads one bounded thread and its reader scope under a
// single tenant transaction, locking the conversation against new posts.
func (s *Adapter) CaptureThreadSnapshot(ctx context.Context, request chat.ThreadSnapshotRequest) (chat.ThreadSnapshot, error) {
	if s == nil || s.Store == nil || request.Limit < 1 || request.Limit > 50 || request.TenantID == "" || request.ConversationID == "" || request.ThreadID == "" || request.InvokingPostID == "" {
		return chat.ThreadSnapshot{}, chat.ErrInvalidArgument
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return chat.ThreadSnapshot{}, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, request.TenantID); err != nil {
		return chat.ThreadSnapshot{}, err
	}
	snapshot, err := readThreadSnapshot(ctx, tx, request, false)
	if err != nil {
		return chat.ThreadSnapshot{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return chat.ThreadSnapshot{}, err
	}
	return snapshot, nil
}

// WithThreadSnapshotFence re-reads the exact snapshot while holding locks over
// the conversation and source posts. The callback runs before the chat
// transaction commits, allowing a durable admission write in another store to
// be fenced against source mutation.
func (s *Adapter) WithThreadSnapshotFence(ctx context.Context, snapshot chat.ThreadSnapshot, fn func() error) error {
	if s == nil || s.Store == nil || fn == nil || snapshot.SnapshotID == "" || snapshot.Digest == "" {
		return chat.ErrThreadSnapshotUnavailable
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, snapshot.TenantID); err != nil {
		return err
	}
	request := chat.ThreadSnapshotRequest{
		Principal: chat.Principal{TenantID: snapshot.PrincipalTenantID, SubjectID: snapshot.PrincipalID, Roles: append([]string(nil), snapshot.PrincipalRoles...), Qualifications: append([]string(nil), snapshot.PrincipalQualifications...)},
		TenantID:  snapshot.TenantID, ConversationID: snapshot.ConversationID, ThreadID: snapshot.ThreadID, InvokingPostID: snapshot.InvokingPostID, Limit: len(snapshot.Posts),
	}
	current, err := readThreadSnapshot(ctx, tx, request, true)
	if err != nil {
		return err
	}
	if current.Digest != snapshot.Digest || current.SnapshotID != snapshot.SnapshotID {
		return ErrThreadSnapshotChanged
	}
	if err := fn(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// readThreadSnapshot is the only implementation path for both capture and
// fence. It reads all visible rows needed for the snapshot in one transaction.
func readThreadSnapshot(ctx context.Context, tx dbport.Tx, request chat.ThreadSnapshotRequest, lockPosts bool) (chat.ThreadSnapshot, error) {
	var snapshot chat.ThreadSnapshot
	var revision, authorityRevision int64
	lock := " FOR SHARE"
	if lockPosts {
		lock = " FOR UPDATE"
	}
	if err := tx.QueryRow(ctx, `SELECT post_sequence,audience_revision FROM chat_conversation WHERE tenant_id=$1 AND id=$2`+lock, request.TenantID, request.ConversationID).Scan(&revision, &authorityRevision); err != nil {
		return snapshot, err
	}
	snapshot = chat.ThreadSnapshot{
		TenantID: request.TenantID, ConversationID: request.ConversationID, ThreadID: request.ThreadID, InvokingPostID: request.InvokingPostID,
		PrincipalTenantID: request.Principal.TenantID, PrincipalID: request.Principal.SubjectID,
		PrincipalRoles: append([]string(nil), request.Principal.Roles...), PrincipalQualifications: append([]string(nil), request.Principal.Qualifications...),
		Revision: uint64(revision), AuthorityRevision: uint64(authorityRevision),
	}
	machine, err := machineActor(ctx, request.Principal.TenantID, request.Principal.SubjectID)
	if err != nil {
		return snapshot, chat.ErrPermissionDenied
	}
	query := `SELECT p.id,p.tenant_id,p.conversation_id,p.author_id,p.author_home_tenant_id,p.sequence,p.body,p.revision,p.tombstoned,p.created_at,p.updated_at,p.parent_id,p.references_json,p.source_attribution FROM chat_post p JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.home_tenant_id=$3 AND m.member_id=$4 AND m.state='active' WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND p.created_at>=m.joined_at)) AND p.tombstoned=false AND (p.id=$5 OR p.parent_id=$5) ORDER BY p.sequence DESC LIMIT $6`
	if machine {
		query = `SELECT p.id,p.tenant_id,p.conversation_id,p.author_id,p.author_home_tenant_id,p.sequence,p.body,p.revision,p.tombstoned,p.created_at,p.updated_at,p.parent_id,p.references_json,p.source_attribution FROM chat_post p JOIN chat_app_installation i ON i.tenant_id=p.tenant_id AND i.conversation_id=p.conversation_id AND i.tenant_id=$3 AND i.app_id=$4 AND i.id=i.tenant_id||':'||i.conversation_id||':'||i.app_id AND i.status='ACTIVE' AND i.version>0 AND i.created_at<=now() AND 'chat.posts.read'=ANY(i.granted_scopes) WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND p.created_at>=i.created_at AND p.tombstoned=false AND (p.id=$5 OR p.parent_id=$5) ORDER BY p.sequence DESC LIMIT $6`
	}
	if lockPosts {
		if machine {
			query += " FOR UPDATE OF p,i"
		} else {
			query += " FOR UPDATE OF p,m"
		}
	}
	rows, err := tx.Query(ctx, query, request.TenantID, request.ConversationID, request.Principal.TenantID, request.Principal.SubjectID, request.ThreadID, request.Limit)
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
	for _, post := range snapshot.Posts {
		if post.ID == request.InvokingPostID && post.AuthorID == request.Principal.SubjectID {
			foundInvoker = true
		}
	}
	if !foundInvoker {
		return snapshot, chat.ErrPermissionDenied
	}
	digest, err := chat.ThreadSnapshotDigest(snapshot)
	if err != nil {
		return snapshot, fmt.Errorf("chatstore: canonicalize thread snapshot: %w", err)
	}
	snapshot.Digest = digest
	snapshot.SnapshotID = "chat-thread-" + digest
	return snapshot, nil
}
