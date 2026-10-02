package chatstore

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ModeratedAdapter is the ordinary reader boundary for retained removal bodies.
// Composition roots must use this wrapper rather than its embedded Adapter.
type ModeratedAdapter struct {
	*Adapter
	RunTrace     chat.ModerationRunTrace
	CurrentRoles func(context.Context, chat.Principal) ([]string, error)
}

func NewModeratedAdapter(adapter *Adapter) *ModeratedAdapter {
	return &ModeratedAdapter{Adapter: adapter}
}

func (s *ModeratedAdapter) removalTx(ctx context.Context, t string, fn func(dbport.Tx) error) error {
	if s == nil || s.Adapter == nil || t == "" {
		return chat.ErrUnavailable
	}
	var subject string
	var roles []string
	if s.CurrentRoles != nil {
		principal, ok := trust.FromContext(ctx)
		if !ok || principal == nil {
			return chat.ErrPermissionDenied
		}
		// Guest authors retain their appeal and notice rights in the host
		// workspace, but their home workspace roles never confer host authority.
		if principal.SubjectKind() == trust.SubjectKindHuman && principal.Tenant().String() == t {
			var err error
			subject = principal.Subject()
			roles, err = s.CurrentRoles(ctx, chat.Principal{TenantID: t, SubjectID: subject})
			if err != nil {
				return err
			}
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, t); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "chat-moderation:"+t); err != nil {
		return err
	}
	if subject != "" {
		if _, err = tx.Exec(ctx, `DELETE FROM chat_moderation_role WHERE tenant_id=$1 AND subject_id=$2`, t, subject); err != nil {
			return err
		}
		for _, role := range roles {
			if _, err = tx.Exec(ctx, `INSERT INTO chat_moderation_role(tenant_id,subject_id,role) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, t, subject, role); err != nil {
				return err
			}
		}
	}
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// moderationPermission uses current durable roles, never Principal.Roles. It
// holds the membership/role/permission rows through the mutation's commit.
func moderationPermission(ctx context.Context, tx dbport.Tx, p chat.Principal, t, cid, permission string) error {
	if p.SubjectID == "" || p.TenantID != t || !chat.ValidModerationPermission(permission) {
		return chat.ErrPermissionDenied
	}
	delegation, _ := ctx.Value(chatremoveSkillKey{}).(chatremoveSkillDelegate)
	if delegation != (chatremoveSkillDelegate{Tenant: p.TenantID, Subject: p.SubjectID}) {
		if machine, e := machineActor(ctx, p.TenantID, p.SubjectID); e != nil || machine {
			return chat.ErrPermissionDenied
		}
	}
	var kind string
	if err := tx.QueryRow(ctx, `SELECT kind FROM chat_conversation WHERE tenant_id=$1 AND id=$2 FOR SHARE`, t, cid).Scan(&kind); err != nil {
		return chat.ErrPermissionDenied
	}
	var role string
	err := tx.QueryRow(ctx, `SELECT role FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$1 AND member_id=$3 AND state='active' AND history_visibility<>'NONE' FOR SHARE`, t, cid, p.SubjectID).Scan(&role)
	if err != nil && !errors.Is(err, dbport.ErrNoRows) {
		return err
	}
	if role == "" && kind != string(chat.PublicChannel) {
		return chat.ErrPermissionDenied
	}
	roles := []string{}
	if role != "" {
		roles = append(roles, role)
	}
	rows, err := tx.Query(ctx, `SELECT role FROM chat_moderation_role WHERE tenant_id=$1 AND subject_id=$2 FOR SHARE`, t, p.SubjectID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var r string
		if err = rows.Scan(&r); err != nil {
			rows.Close()
			return err
		}
		roles = append(roles, r)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, r := range roles {
		allowed := chat.DefaultModerationPermission(r, r == role && role != "", permission)
		var override bool
		err = tx.QueryRow(ctx, `SELECT allowed FROM chat_moderation_permission WHERE tenant_id=$1 AND role=$2 AND permission=$3 AND conversation_id IN ('',$4) ORDER BY conversation_id DESC LIMIT 1 FOR SHARE`, t, r, permission, cid).Scan(&override)
		if err == nil {
			allowed = override
		} else if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		if allowed {
			return nil
		}
	}
	return chat.ErrPermissionDenied
}

func removalCandidates(ctx context.Context, tx dbport.Tx, r chat.RemovalRequest, lock bool) ([]chat.RemovalCandidate, error) {
	if err := chat.ValidateRemoval(r); err != nil {
		return nil, err
	}
	permission := chat.PermissionRemoveMessages
	if r.Action == "restore" {
		permission = chat.PermissionReviewRemovedMessages
	}
	if err := moderationPermission(ctx, tx, r.Principal, r.TenantID, r.Selection.ConversationID, permission); err != nil {
		return nil, err
	}
	query := `SELECT p.id,p.revision FROM chat_post p LEFT JOIN chat_admin_removal a ON a.tenant_id=p.tenant_id AND a.conversation_id=p.conversation_id AND a.post_id=p.id WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND (($3='remove' AND NOT p.tombstoned) OR ($3='restore' AND p.tombstoned AND a.restored_at IS NULL AND a.removed_at>=now()-interval '30 days')) AND (p.id=ANY($4::text[]) OR (cardinality($4::text[])=0 AND p.author_id=$5 AND p.author_home_tenant_id=$6 AND p.created_at>=$7 AND p.created_at<$8)) AND NOT EXISTS (SELECT 1 FROM chat_membership m WHERE m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.home_tenant_id=$1 AND m.member_id=$9 AND m.state='active' AND m.history_visibility='FROM_JOIN' AND p.created_at<m.joined_at) ORDER BY p.id LIMIT 201`
	if lock {
		query += ` FOR UPDATE OF p`
	}
	ids := r.Selection.PostIDs
	if ids == nil {
		ids = []string{}
	}
	rows, err := tx.Query(ctx, query, r.TenantID, r.Selection.ConversationID, r.Action, ids, r.Selection.AuthorID, r.Selection.AuthorHomeTenantID, r.Selection.From, r.Selection.Until, r.Principal.SubjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []chat.RemovalCandidate
	for rows.Next() {
		var c chat.RemovalCandidate
		if err = rows.Scan(&c.ID, &c.Revision); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(out) > 200 || (len(ids) > 0 && len(out) != len(ids)) {
		return nil, chat.ErrConflict
	}
	return out, nil
}

func (s *ModeratedAdapter) PreviewRemoval(ctx context.Context, r chat.RemovalRequest) ([]chat.RemovalCandidate, error) {
	var rows []chat.RemovalCandidate
	err := s.removalTx(ctx, r.TenantID, func(tx dbport.Tx) error { var err error; rows, err = removalCandidates(ctx, tx, r, false); return err })
	return rows, err
}

func (s *ModeratedAdapter) CommitRemoval(ctx context.Context, r chat.RemovalRequest, at time.Time) (int, error) {
	count := 0
	err := s.removalTx(ctx, r.TenantID, func(tx dbport.Tx) error {
		if err := fenceContextWrite(ctx, tx, r.TenantID, r.Selection.ConversationID); err != nil {
			return err
		}
		rows, err := removalCandidates(ctx, tx, r, true)
		if err != nil {
			return err
		}
		if len(rows) == 0 || len(rows) != r.ConfirmedCount || chat.RemovalConfirmation(r, rows) != r.Confirmation {
			return chat.ErrConflict
		}
		for _, c := range rows {
			if err = s.removeOne(ctx, tx, r, c, at, ""); err != nil {
				return err
			}
		}
		count = len(rows)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (s *ModeratedAdapter) removeOne(ctx context.Context, tx dbport.Tx, r chat.RemovalRequest, c chat.RemovalCandidate, at time.Time, except string) error {
	t, cid := r.TenantID, r.Selection.ConversationID
	runRef := ""
	if s.RunTrace != nil {
		var err error
		runRef, err = s.RunTrace.RunForModerationPost(ctx, t, cid, c.ID)
		if err != nil {
			return err
		}
	}
	var author, home string
	if err := tx.QueryRow(ctx, `SELECT author_id,author_home_tenant_id FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3`, t, cid, c.ID).Scan(&author, &home); err != nil {
		return err
	}
	removed := r.Action == "remove"
	if removed {
		_, err := tx.Exec(ctx, `INSERT INTO chat_admin_removal(tenant_id,conversation_id,post_id,actor_id,removed_at,reason_code,note,run_ref,queue_state) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'CLOSED') ON CONFLICT(tenant_id,conversation_id,post_id) DO UPDATE SET actor_id=EXCLUDED.actor_id,removed_at=EXCLUDED.removed_at,reason_code=EXCLUDED.reason_code,note=EXCLUDED.note,run_ref=EXCLUDED.run_ref,restored_at=NULL,restored_by=NULL,appealed=false,queue_state='CLOSED',revision=chat_admin_removal.revision+1`, t, cid, c.ID, r.Principal.SubjectID, at, r.ReasonCode, r.Note, runRef)
		if err != nil {
			return err
		}
	} else {
		var id string
		if err := tx.QueryRow(ctx, `UPDATE chat_admin_removal SET restored_at=$4,restored_by=$5,queue_state='CLOSED',revision=revision+1 WHERE tenant_id=$1 AND conversation_id=$2 AND post_id=$3 AND restored_at IS NULL AND removed_at >= $4::timestamptz-interval '30 days' RETURNING post_id`, t, cid, c.ID, at, r.Principal.SubjectID).Scan(&id); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return chat.ErrConflict
			}
			return err
		}
	}
	// Body, references, revision ledger and records holds are deliberately retained.
	if _, err := tx.Exec(ctx, `UPDATE chat_post SET tombstoned=$4,revision=revision+1,updated_at=$5 WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3`, t, cid, c.ID, removed, at); err != nil {
		return err
	}
	reason := r.ReasonCode
	if r.Note != "" {
		reason += " — " + r.Note
	}
	if !removed && r.Note != "" {
		// A restore is explained by what the reviewer wrote, not by the removal's
		// reason code, which it does not carry.
		reason = r.Note
	}
	if reason == "" {
		reason = "restored"
	}
	if err := moderationNotice(ctx, tx, t, fmt.Sprintf("%s:%s:%d", r.Action, c.ID, c.Revision), cid, c.ID, home, author, reason, r.Action, at); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO chat_moderation_action(tenant_id,case_id,action,actor_id,reason,evidence_ref,at_time) VALUES($1,$2,$3,$4,$5,$6,$7)`, t, "removal:"+c.ID, r.Action, r.Principal.SubjectID, reason, "post:"+c.ID, at); err != nil {
		return err
	}
	if err := moderationAudit(ctx, tx, r.Principal, t, cid, c.ID, "moderation."+r.Action, reason, c.Revision, at); err != nil {
		return err
	}
	// The decision answers the other open reports about this message too.
	if err := closeOpenReports(ctx, tx, r.Principal, t, cid, c.ID, except, r.Action, reason, at); err != nil {
		return err
	}
	if err := emitAdapterEvent(ctx, tx, t, cid, "moderation."+r.Action, r.Principal.TenantID, r.Principal.SubjectID, c.ID, c.Revision+1, map[string]any{"PostID": c.ID, "Removed": removed, "ReasonCode": r.ReasonCode, "ActorID": r.Principal.SubjectID, "Reason": reason, "At": at}); err != nil {
		return err
	}
	post, err := moderationPost(ctx, tx, t, cid, c.ID, r.Principal.SubjectID, false)
	if err != nil {
		return err
	}
	event := "post.edited"
	if removed {
		event = "post.deleted"
	}
	return emitAdapterEvent(ctx, tx, t, cid, event, r.Principal.TenantID, r.Principal.SubjectID, c.ID, post.Revision, post)
}

func moderationNotice(ctx context.Context, tx dbport.Tx, t, id, cid, post, home, subject, reason, outcome string, at time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO chat_moderation_notice(tenant_id,id,conversation_id,post_id,home_tenant_id,subject_id,reason,outcome,at_time) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING`, t, id, cid, post, home, subject, reason, outcome, at)
	return err
}

func (s *ModeratedAdapter) ReviewRemoved(ctx context.Context, p chat.Principal, t, cid, id, reason string, at time.Time) (chat.Post, error) {
	var post chat.Post
	if strings.TrimSpace(reason) == "" || len(reason) > 2000 {
		return post, chat.ErrInvalidArgument
	}
	err := s.removalTx(ctx, t, func(tx dbport.Tx) error {
		if err := fenceContextWrite(ctx, tx, t, cid); err != nil {
			return err
		}
		if err := moderationPermission(ctx, tx, p, t, cid, chat.PermissionReviewRemovedMessages); err != nil {
			return err
		}
		var err error
		post, err = moderationPost(ctx, tx, t, cid, id, p.SubjectID, true)
		if err != nil {
			return err
		}
		if !post.Deleted {
			return chat.ErrConflict
		}
		return moderationAudit(ctx, tx, p, t, cid, id, "moderation.review", reason, post.Revision, at)
	})
	if err != nil {
		return chat.Post{}, err
	}
	return post, nil
}

func moderationPost(ctx context.Context, tx dbport.Tx, t, cid, id, actor string, original bool) (chat.Post, error) {
	var x Post
	err := tx.QueryRow(ctx, `SELECT p.id,p.tenant_id,p.conversation_id,p.author_id,p.author_home_tenant_id,p.sequence,p.body,p.revision,p.tombstoned,p.created_at,p.parent_id,p.references_json,p.source_attribution FROM chat_post p WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND p.id=$3 AND NOT EXISTS(SELECT 1 FROM chat_membership m WHERE m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.member_id=$4 AND m.home_tenant_id=$1 AND m.state='active' AND (m.history_visibility='NONE' OR (m.history_visibility='FROM_JOIN' AND p.created_at<m.joined_at)))`, t, cid, id, actor).Scan(&x.ID, &x.TenantID, &x.ConversationID, &x.AuthorID, &x.AuthorHomeTenantID, &x.Sequence, &x.Body, &x.Revision, &x.Tombstoned, &x.CreatedAt, &x.ParentID, &x.References, &x.SourceAttribution)
	if errors.Is(err, dbport.ErrNoRows) {
		return chat.Post{}, chat.ErrNotFound
	}
	if err != nil {
		return chat.Post{}, err
	}
	p, err := chatPost(x)
	if p.Deleted && !original {
		p.Body = chat.RemovedByAdministrator
		p.References = nil
		p.SourceAttribution = nil
	}
	return p, err
}

func (s *ModeratedAdapter) projectRemoval(ctx context.Context, p chat.Post) (chat.Post, error) {
	if !p.Deleted {
		return p, nil
	}
	var removed bool
	err := s.removalTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_admin_removal WHERE tenant_id=$1 AND conversation_id=$2 AND post_id=$3 AND restored_at IS NULL)`, p.TenantID, p.ConversationID, p.ID).Scan(&removed)
	})
	if err != nil {
		return chat.Post{}, err
	}
	if removed {
		p.Body = chat.RemovedByAdministrator
		p.References = nil
		p.SourceAttribution = nil
	}
	return p, nil
}

func (s *ModeratedAdapter) GetPost(ctx context.Context, t, cid, id string) (chat.Post, error) {
	p, err := s.Adapter.GetPost(ctx, t, cid, id)
	if err != nil {
		return chat.Post{}, err
	}
	return s.projectRemoval(ctx, p)
}

func (s *ModeratedAdapter) SendPost(ctx context.Context, r chat.SendPostRequest, p chat.Post) (chat.Post, error) {
	post, err := s.Adapter.SendPost(ctx, r, p)
	if err != nil {
		return chat.Post{}, err
	}
	return s.projectRemoval(ctx, post)
}

func (s *ModeratedAdapter) DeletePost(ctx context.Context, r chat.DeletePostRequest) (chat.Post, error) {
	post, err := s.Adapter.DeletePost(ctx, r)
	if err != nil {
		return chat.Post{}, err
	}
	return s.projectRemoval(ctx, post)
}

func (s *ModeratedAdapter) CommitPersonaReply(ctx context.Context, r chat.PersonaReplyCommitRequest) (chat.Post, error) {
	post, err := s.Adapter.CommitPersonaReply(ctx, r)
	if err != nil {
		return chat.Post{}, err
	}
	return s.projectRemoval(ctx, post)
}

func (s *ModeratedAdapter) ListPosts(ctx context.Context, p chat.Principal, t, cid string, after uint64, page chat.Page, window chat.PostWindow) (chat.ListPostsResponse, error) {
	out, err := s.Adapter.ListPosts(ctx, p, t, cid, after, page, window)
	if err != nil {
		return chat.ListPostsResponse{}, err
	}
	for i := range out.Posts {
		out.Posts[i], err = s.projectRemoval(ctx, out.Posts[i])
		if err != nil {
			return chat.ListPostsResponse{}, err
		}
	}
	return out, nil
}

func (s *ModeratedAdapter) ModerationNotices(ctx context.Context, p chat.Principal, t string) ([]chat.ModerationNotice, error) {
	if p.TenantID == "" || p.SubjectID == "" || t == "" {
		return nil, chat.ErrPermissionDenied
	}
	var out []chat.ModerationNotice
	err := s.removalTx(ctx, t, func(tx dbport.Tx) error {
		var err error
		out, err = moderationNoticesTx(ctx, tx, p, t)
		return err
	})
	return out, err
}

func (s *ModeratedAdapter) AppealRemoval(ctx context.Context, p chat.Principal, t, cid, id string, at time.Time) error {
	if p.TenantID == "" || p.SubjectID == "" || t == "" {
		return chat.ErrPermissionDenied
	}
	if machine, err := machineActor(ctx, p.TenantID, p.SubjectID); err != nil || machine {
		return chat.ErrPermissionDenied
	}
	return s.removalTx(ctx, t, func(tx dbport.Tx) error {
		if err := fenceContextWrite(ctx, tx, t, cid); err != nil {
			return err
		}
		var revision uint64
		if err := tx.QueryRow(ctx, `UPDATE chat_admin_removal a SET appealed=true,revision=a.revision+1 FROM chat_post p WHERE a.tenant_id=$1 AND a.conversation_id=$2 AND a.post_id=$3 AND a.restored_at IS NULL AND NOT a.appealed AND p.tenant_id=a.tenant_id AND p.id=a.post_id AND p.author_id=$4 AND p.author_home_tenant_id=$5 RETURNING a.revision`, t, cid, id, p.SubjectID, p.TenantID).Scan(&revision); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return chat.ErrConflict
			}
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO chat_moderation_report(tenant_id,report_id,conversation_id,target_id,reporter_id,reason,created_at,state,reporter_home_tenant_id) VALUES($1,$2,$3,$4,$5,'appeal',$6,'OPEN',$7)`, t, fmt.Sprintf("appeal:%s:%d", id, revision), cid, id, p.SubjectID, at, p.TenantID); err != nil {
			return err
		}
		return emitAdapterEvent(ctx, tx, t, cid, "moderation.appeal", p.TenantID, p.SubjectID, id, revision, map[string]string{"PostID": id})
	})
}

var _ chat.Store = (*ModeratedAdapter)(nil)
var _ chat.ModerationStore = (*ModeratedAdapter)(nil)

// sort is used by queue assembly so preview and queue order remain stable.
func sortModerationItems(items []chat.ModerationItem) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].At.Equal(items[j].At) {
			return items[i].ID < items[j].ID
		}
		return items[i].At.Before(items[j].At)
	})
}
