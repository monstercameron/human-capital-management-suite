package chatstore

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func moderationMatches(x chat.ModerationItem, query string) bool {
	return strings.Contains(strings.ToLower(x.Reason+" "+x.Kind+" "+x.State+" "+x.Message.Body+" "+x.Rule), strings.ToLower(query))
}

// moderationItemDetail completes one queue candidate for one viewer: the
// message, its neighbours and what the viewer may do with it. keep is false when
// the viewer cannot open the item, which is also why it is never counted.
func moderationItemDetail(ctx context.Context, tx dbport.Tx, p chat.Principal, t string, x chat.ModerationItem, query string, withContext, audit bool) (chat.ModerationItem, bool, error) {
	reviewErr := moderationPermission(ctx, tx, p, t, x.ConversationID, chat.PermissionReviewRemovedMessages)
	removeErr := moderationPermission(ctx, tx, p, t, x.ConversationID, chat.PermissionRemoveMessages)
	if reviewErr != nil && !errors.Is(reviewErr, chat.ErrPermissionDenied) {
		return x, false, reviewErr
	}
	if removeErr != nil && !errors.Is(removeErr, chat.ErrPermissionDenied) {
		return x, false, removeErr
	}
	if reviewErr != nil && removeErr != nil {
		return x, false, nil
	}
	if e := tx.QueryRow(ctx, `SELECT name FROM chat_conversation WHERE tenant_id=$1 AND id=$2`, t, x.ConversationID).Scan(&x.ConversationName); e != nil && !errors.Is(e, dbport.ErrNoRows) {
		return x, false, e
	}
	if x.State != "OPEN" {
		// A resolved item says how it was closed: what, by whom, why and when.
		if e := tx.QueryRow(ctx, `SELECT action,actor_id,reason,at_time FROM chat_moderation_action WHERE tenant_id=$1 AND case_id=$2 ORDER BY id DESC LIMIT 1`, t, x.ID).Scan(&x.Decision, &x.DecidedBy, &x.DecisionReason, &x.DecidedAt); e != nil && !errors.Is(e, dbport.ErrNoRows) {
			return x, false, e
		}
	}
	if x.PostID == "" {
		// A hit that no message could be matched to still shows which rule matched.
		if query != "" && !moderationMatches(x, query) {
			return x, false, nil
		}
		return x, true, nil
	}
	var err error
	x.Message, err = moderationPost(ctx, tx, t, x.ConversationID, x.PostID, p.SubjectID, true)
	if errors.Is(err, chat.ErrNotFound) {
		return x, false, nil
	}
	if err != nil {
		return x, false, err
	}
	if x.Message.Deleted && reviewErr != nil {
		return x, false, nil
	}
	x.CanRemove = removeErr == nil && !x.Message.Deleted
	x.CanRestore = reviewErr == nil && x.Message.Deleted
	if query != "" && !moderationMatches(x, query) {
		return x, false, nil
	}
	if audit && x.Message.Deleted {
		if err = moderationAudit(ctx, tx, p, t, x.ConversationID, x.PostID, "moderation.review", "moderation queue", x.Message.Revision, time.Now().UTC()); err != nil {
			return x, false, err
		}
	}
	x.AuthorID = x.Message.AuthorID
	if !withContext {
		return x, true, nil
	}
	contextRows, e := tx.Query(ctx, `SELECT id FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2 AND sequence BETWEEN $3-2 AND $3+2 ORDER BY sequence`, t, x.ConversationID, x.Message.Sequence)
	if e != nil {
		return x, false, e
	}
	var ids []string
	for contextRows.Next() {
		var id string
		if e = contextRows.Scan(&id); e != nil {
			contextRows.Close()
			return x, false, e
		}
		ids = append(ids, id)
	}
	contextRows.Close()
	if e = contextRows.Err(); e != nil {
		return x, false, e
	}
	for _, id := range ids {
		contextPost, e := moderationPost(ctx, tx, t, x.ConversationID, id, p.SubjectID, false)
		if errors.Is(e, chat.ErrNotFound) {
			continue
		}
		if e != nil {
			return x, false, e
		}
		x.Context = append(x.Context, contextPost)
	}
	return x, true, nil
}

// FilterModeration puts filter hits flagged for review into the moderation
// queue. A hit is recorded before its message exists, so the message is the
// author's post in that conversation written at the time of the hit (a new post
// or an edit); a hit that matches no post is still listed, with its rule.
type FilterModeration struct{ Adapter *ModeratedAdapter }

func NewFilterModeration(a *ModeratedAdapter) *FilterModeration { return &FilterModeration{Adapter: a} }

const filterHitSelect = `SELECT h.id,h.channel_id,h.subject_id,h.created_at,COALESCE(NULLIF(h.hit->>'RuleName',''),h.rule_id),h.rule_version,
 COALESCE(r.post_id,COALESCE((SELECT p.id FROM chat_post p WHERE p.tenant_id=h.tenant_id AND p.conversation_id=h.channel_id AND p.author_id=h.subject_id
  AND p.updated_at>=h.created_at-interval '5 seconds' AND p.updated_at<h.created_at+interval '2 minutes'
  ORDER BY abs(extract(epoch FROM p.updated_at-h.created_at)),p.sequence LIMIT 1),'')),
 r.hit_id IS NULL,COALESCE(r.decision,'')
 FROM chat_filter_hit h LEFT JOIN chat_filter_hit_review r ON r.tenant_id=h.tenant_id AND r.hit_id=h.id
 WHERE h.tenant_id=$1 AND h.hit->>'Action'='flag' AND COALESCE(h.hit->>'DryRun','false')<>'true'`

func filterHitItem(rows dbport.Rows) (chat.ModerationItem, int64, error) {
	var x chat.ModerationItem
	var id int64
	var version, decision string
	var open bool
	if err := rows.Scan(&id, &x.ConversationID, &x.AuthorID, &x.At, &x.Rule, &version, &x.PostID, &open, &decision); err != nil {
		return x, 0, err
	}
	x.ID, x.Kind = "filter:"+strconv.FormatInt(id, 10), "filter"
	x.Reason = x.Rule
	if version != "" {
		x.Rule += " " + version
	}
	x.State = "CLOSED"
	if open {
		x.State = "OPEN"
	}
	return x, id, nil
}

func (f *FilterModeration) candidates(ctx context.Context, tx dbport.Tx, t string, history bool) ([]chat.ModerationItem, error) {
	query := filterHitSelect
	if !history {
		query += ` AND r.hit_id IS NULL`
	}
	rows, err := tx.Query(ctx, query+` ORDER BY h.id DESC LIMIT 200`, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []chat.ModerationItem
	for rows.Next() {
		x, _, e := filterHitItem(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (f *FilterModeration) SearchModeration(ctx context.Context, p chat.Principal, t, query string) ([]chat.ModerationItem, error) {
	if f == nil || f.Adapter == nil {
		return nil, chat.ErrUnavailable
	}
	if p.TenantID != t || p.SubjectID == "" || len(query) > 512 {
		return nil, chat.ErrPermissionDenied
	}
	text, state := moderationQueryState(query)
	var out []chat.ModerationItem
	err := f.Adapter.removalTx(ctx, t, func(tx dbport.Tx) error {
		candidates, err := f.candidates(ctx, tx, t, state != "open")
		if err != nil {
			return err
		}
		for _, x := range candidates {
			if state == "closed" && x.State == "OPEN" {
				continue
			}
			item, keep, e := moderationItemDetail(ctx, tx, p, t, x, text, true, true)
			if e != nil {
				return e
			}
			if keep {
				out = append(out, item)
			}
		}
		return nil
	})
	return out, err
}

// CountModeration counts the open flagged hits this person can open.
func (f *FilterModeration) CountModeration(ctx context.Context, p chat.Principal, t string) (int, error) {
	if f == nil || f.Adapter == nil {
		return 0, chat.ErrUnavailable
	}
	if p.TenantID != t || p.SubjectID == "" {
		return 0, chat.ErrPermissionDenied
	}
	count := 0
	err := f.Adapter.removalTx(ctx, t, func(tx dbport.Tx) error {
		candidates, err := f.candidates(ctx, tx, t, false)
		if err != nil {
			return err
		}
		for _, x := range candidates {
			if _, keep, e := moderationItemDetail(ctx, tx, p, t, x, "", false, false); e != nil {
				return e
			} else if keep {
				count++
			}
		}
		return nil
	})
	return count, err
}

// ConversationOf names the conversation of one filter item, for the caller that
// must take the conversation's write lease before the decision is recorded.
func (f *FilterModeration) ConversationOf(ctx context.Context, t, id string) (string, error) {
	hit, err := strconv.ParseInt(strings.TrimPrefix(id, "filter:"), 10, 64)
	if err != nil || !strings.HasPrefix(id, "filter:") || f == nil || f.Adapter == nil {
		return "", chat.ErrInvalidArgument
	}
	var cid string
	err = f.Adapter.removalTx(ctx, t, func(tx dbport.Tx) error {
		e := tx.QueryRow(ctx, `SELECT channel_id FROM chat_filter_hit WHERE tenant_id=$1 AND id=$2`, t, hit).Scan(&cid)
		if errors.Is(e, dbport.ErrNoRows) {
			return chat.ErrNotFound
		}
		return e
	})
	return cid, err
}

// ResolveModeration records the decision on one flagged hit: Remove takes the
// matched message down, Restore brings it back, Dismiss closes the hit, and
// Message the author tells the author. Each is recorded with its actor and
// reason and closes the item.
func (f *FilterModeration) ResolveModeration(ctx context.Context, p chat.Principal, t, id, action, reason string, at time.Time) error {
	if f == nil || f.Adapter == nil {
		return chat.ErrUnavailable
	}
	if strings.TrimSpace(reason) == "" || len(reason) > 2000 {
		return chat.ErrInvalidArgument
	}
	hit, err := strconv.ParseInt(strings.TrimPrefix(id, "filter:"), 10, 64)
	if err != nil || !strings.HasPrefix(id, "filter:") {
		return chat.ErrInvalidArgument
	}
	return f.Adapter.removalTx(ctx, t, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, filterHitSelect+` AND h.id=$2`, t, hit)
		if err != nil {
			return err
		}
		var x chat.ModerationItem
		found := false
		for rows.Next() {
			if x, _, err = filterHitItem(rows); err != nil {
				rows.Close()
				return err
			}
			found = true
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		if !found {
			return chat.ErrNotFound
		}
		return f.Adapter.decideModeration(ctx, tx, p, t, moderationCase{ID: id, Conversation: x.ConversationID, Post: x.PostID, State: x.State, Close: func() error {
			_, e := tx.Exec(ctx, `INSERT INTO chat_filter_hit_review(tenant_id,hit_id,conversation_id,post_id,decision,reason,decided_by,decided_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, t, hit, x.ConversationID, x.PostID, action, reason, p.SubjectID, at)
			return e
		}}, action, reason, at)
	})
}

var _ chat.FilterModerationPort = (*FilterModeration)(nil)
var _ chat.FilterModerationCounter = (*FilterModeration)(nil)

// moderationNoticesTx reads the notices sent to one person in one workspace.
func moderationNoticesTx(ctx context.Context, tx dbport.Tx, p chat.Principal, t string) ([]chat.ModerationNotice, error) {
	rows, err := tx.Query(ctx, `SELECT n.id,n.conversation_id,n.post_id,n.reason,n.outcome,n.at_time,COALESCE(n.outcome='remove' AND n.id NOT LIKE 'outcome:%' AND a.restored_at IS NULL AND NOT a.appealed AND n.at_time=a.removed_at,false) FROM chat_moderation_notice n LEFT JOIN chat_admin_removal a ON a.tenant_id=n.tenant_id AND a.conversation_id=n.conversation_id AND a.post_id=n.post_id WHERE n.tenant_id=$1 AND n.home_tenant_id=$2 AND n.subject_id=$3 ORDER BY n.at_time DESC,n.id LIMIT 200`, t, p.TenantID, p.SubjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []chat.ModerationNotice
	for rows.Next() {
		var n chat.ModerationNotice
		n.TenantID = t
		if err = rows.Scan(&n.ID, &n.ConversationID, &n.PostID, &n.Reason, &n.Outcome, &n.At, &n.CanAppeal); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ModerationSummary is the one small read behind the Moderation entry of the
// Chat sidebar and the message menu. It writes no audit rows: nothing is opened.
func (s *ModeratedAdapter) ModerationSummary(ctx context.Context, p chat.Principal, t string) (chat.ModerationSummary, error) {
	var out chat.ModerationSummary
	if p.TenantID == "" || p.SubjectID == "" || t == "" || p.TenantID != t {
		return out, chat.ErrPermissionDenied
	}
	err := s.removalTx(ctx, t, func(tx dbport.Tx) error {
		notices, err := moderationNoticesTx(ctx, tx, p, t)
		if err != nil {
			return err
		}
		cutoff := time.Now().Add(-chat.ModerationNoticeWindow)
		for _, n := range notices {
			if n.At.After(cutoff) {
				out.NoticeCount++
			}
		}
		if len(notices) > 50 {
			notices = notices[:50]
		}
		out.Notices = notices
		var admin bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_moderation_role WHERE tenant_id=$1 AND subject_id=$2 AND role='WORKSPACE_ADMIN')`, t, p.SubjectID).Scan(&admin); err != nil {
			return err
		}
		if admin {
			for permission, flag := range map[string]*bool{chat.PermissionRemoveMessages: &out.RemoveEverywhere, chat.PermissionReviewRemovedMessages: &out.ReviewEverywhere} {
				var denied bool
				if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_moderation_permission WHERE tenant_id=$1 AND conversation_id='' AND role='WORKSPACE_ADMIN' AND permission=$2 AND NOT allowed)`, t, permission).Scan(&denied); err != nil {
					return err
				}
				*flag = !denied
			}
		}
		rows, err := tx.Query(ctx, `SELECT m.conversation_id FROM chat_membership m WHERE m.tenant_id=$1 AND m.home_tenant_id=$1 AND m.member_id=$2 AND m.state='active' AND m.history_visibility<>'NONE' AND (m.role=$3 OR EXISTS(SELECT 1 FROM chat_moderation_permission o WHERE o.tenant_id=$1 AND o.role=m.role AND o.allowed AND o.permission IN ($4,$5))) ORDER BY m.conversation_id LIMIT 400`, t, p.SubjectID, string(chat.Manager), chat.PermissionRemoveMessages, chat.PermissionReviewRemovedMessages)
		if err != nil {
			return err
		}
		var cids []string
		for rows.Next() {
			var cid string
			if err = rows.Scan(&cid); err != nil {
				rows.Close()
				return err
			}
			cids = append(cids, cid)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		for _, cid := range cids {
			if e := moderationPermission(ctx, tx, p, t, cid, chat.PermissionRemoveMessages); e == nil {
				out.Removable = append(out.Removable, cid)
			} else if !errors.Is(e, chat.ErrPermissionDenied) {
				return e
			}
			if e := moderationPermission(ctx, tx, p, t, cid, chat.PermissionReviewRemovedMessages); e == nil {
				out.Reviewable = append(out.Reviewable, cid)
			} else if !errors.Is(e, chat.ErrPermissionDenied) {
				return e
			}
		}
		out.Moderator = out.RemoveEverywhere || out.ReviewEverywhere || len(out.Removable) > 0 || len(out.Reviewable) > 0
		if !out.Moderator {
			return nil
		}
		items, err := moderationOpenItems(ctx, tx, p, t, "", false, false)
		if err != nil {
			return err
		}
		out.Open = len(items)
		return nil
	})
	return out, err
}

// ModerationTarget reads the message a dialog is about, as the actor may see it.
func (s *ModeratedAdapter) ModerationTarget(ctx context.Context, p chat.Principal, t, cid, id string) (chat.Post, error) {
	var post chat.Post
	err := s.removalTx(ctx, t, func(tx dbport.Tx) error {
		var e error
		post, e = moderationPost(ctx, tx, t, cid, id, p.SubjectID, false)
		return e
	})
	return post, err
}

// ModerationCaseConversation names the conversation a queue item is about,
// open or resolved, for someone who holds a moderation permission there. To
// anyone else the item does not exist. The routing layer uses it to take the
// conversation's write lease before a resolved item's message is restored.
func (s *ModeratedAdapter) ModerationCaseConversation(ctx context.Context, p chat.Principal, t, id string) (string, error) {
	if p.TenantID != t || p.SubjectID == "" {
		return "", chat.ErrPermissionDenied
	}
	var conversation string
	err := s.removalTx(ctx, t, func(tx dbport.Tx) error {
		var err error
		switch {
		case strings.HasPrefix(id, "report:"):
			err = tx.QueryRow(ctx, `SELECT conversation_id FROM chat_moderation_report WHERE tenant_id=$1 AND report_id=$2`, t, strings.TrimPrefix(id, "report:")).Scan(&conversation)
		case strings.HasPrefix(id, "removal:"):
			err = tx.QueryRow(ctx, `SELECT conversation_id FROM chat_admin_removal WHERE tenant_id=$1 AND post_id=$2`, t, strings.TrimPrefix(id, "removal:")).Scan(&conversation)
		default:
			return chat.ErrNotFound
		}
		if errors.Is(err, dbport.ErrNoRows) {
			return chat.ErrNotFound
		}
		if err != nil {
			return err
		}
		reviewErr := moderationPermission(ctx, tx, p, t, conversation, chat.PermissionReviewRemovedMessages)
		if reviewErr == nil {
			return nil
		}
		if !errors.Is(reviewErr, chat.ErrPermissionDenied) {
			return reviewErr
		}
		if removeErr := moderationPermission(ctx, tx, p, t, conversation, chat.PermissionRemoveMessages); removeErr != nil {
			if errors.Is(removeErr, chat.ErrPermissionDenied) {
				return chat.ErrNotFound
			}
			return removeErr
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return conversation, nil
}

// moderationQueryState reads the state a queue read asks for out of its query:
// no query is the open items, "state:closed" is the resolved ones, and any other
// text searches every item, open or resolved, for that text.
func moderationQueryState(query string) (text, state string) {
	if rest, ok := strings.CutPrefix(query, "state:closed"); ok {
		return strings.TrimSpace(rest), "closed"
	}
	if query == "" {
		return "", "open"
	}
	return query, "all"
}

func moderationOpenItems(ctx context.Context, tx dbport.Tx, p chat.Principal, t, query string, withContext, audit bool) ([]chat.ModerationItem, error) {
	text, state := moderationQueryState(query)
	rows, err := tx.Query(ctx, `SELECT * FROM (SELECT 'report:'||report_id,CASE WHEN reason='appeal' THEN 'appeal' ELSE 'report' END,conversation_id,target_id,reporter_id,reason,state,created_at FROM chat_moderation_report WHERE tenant_id=$1 UNION ALL SELECT 'removal:'||post_id,'removal',conversation_id,post_id,'',reason_code||CASE WHEN note='' THEN '' ELSE ' — '||note END,queue_state,removed_at FROM chat_admin_removal WHERE tenant_id=$1) u WHERE $2='all' OR ($2='open')=(u.state='OPEN') ORDER BY 8 DESC LIMIT 500`, t, state)
	if err != nil {
		return nil, err
	}
	var candidates []chat.ModerationItem
	for rows.Next() {
		var x chat.ModerationItem
		if err = rows.Scan(&x.ID, &x.Kind, &x.ConversationID, &x.PostID, &x.ReporterID, &x.Reason, &x.State, &x.At); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, x)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	var out []chat.ModerationItem
	for _, x := range candidates {
		item, keep, e := moderationItemDetail(ctx, tx, p, t, x, text, withContext, audit)
		if e != nil {
			return nil, e
		}
		if keep {
			out = append(out, item)
		}
	}
	return out, nil
}

// moderationCase is one queue item about to be decided.
type moderationCase struct {
	ID, Conversation, Post, State string
	// Reporter is who asked for the decision, and the workspace they belong to.
	Reporter, ReporterHome string
	// Close marks the item decided in its own table.
	Close func() error
}

// decideModeration carries out one decision on one queue item inside the
// caller's transaction: the permission and the item's state are checked again
// here, the action is applied, the item is closed, the person who asked is told
// the outcome, and the decision is recorded with its actor and reason.
func (s *ModeratedAdapter) decideModeration(ctx context.Context, tx dbport.Tx, p chat.Principal, t string, c moderationCase, action, reason string, at time.Time) error {
	reviewErr := moderationPermission(ctx, tx, p, t, c.Conversation, chat.PermissionReviewRemovedMessages)
	removeErr := moderationPermission(ctx, tx, p, t, c.Conversation, chat.PermissionRemoveMessages)
	if reviewErr != nil && removeErr != nil {
		return chat.ErrPermissionDenied
	}
	// A decided item is decided once. Restore is the way back from a removal
	// and is offered on resolved items for thirty days: it runs on a closed
	// item whose message is still removed, and removalCandidates refuses it
	// (a conflict) once the message is back or the thirty days are over.
	if c.State != "OPEN" && action != "restore" {
		return chat.ErrConflict
	}
	var message chat.Post
	if c.Post != "" {
		var err error
		message, err = moderationPost(ctx, tx, t, c.Conversation, c.Post, p.SubjectID, false)
		if err != nil {
			return err
		}
		if message.Deleted && reviewErr != nil {
			return chat.ErrPermissionDenied
		}
	} else if action != "dismiss" {
		// Nothing was matched to this hit, so there is no message to act on.
		return chat.ErrConflict
	}
	if err := fenceContextWrite(ctx, tx, t, c.Conversation); err != nil {
		return err
	}
	switch action {
	case "remove", "restore":
		if action == "remove" {
			if err := moderationPermission(ctx, tx, p, t, c.Conversation, chat.PermissionRemoveMessages); err != nil {
				return err
			}
		}
		code, note := "policy_violation", reason
		parts := strings.SplitN(reason, " — ", 2)
		if chat.ValidRemovalReason(parts[0]) {
			code = parts[0]
			note = ""
			if len(parts) == 2 {
				note = parts[1]
			}
		}
		r := chat.RemovalRequest{Principal: p, TenantID: t, Selection: chat.RemovalSelection{ConversationID: c.Conversation, PostIDs: []string{c.Post}}, ReasonCode: code, Note: note, Action: action}
		candidates, err := removalCandidates(ctx, tx, r, true)
		if err != nil {
			return err
		}
		if len(candidates) != 1 {
			return chat.ErrConflict
		}
		if err = s.removeOne(ctx, tx, r, candidates[0], at, c.ID); err != nil {
			return err
		}
	case "dismiss":
	case "message_author":
		if err := moderationNotice(ctx, tx, t, "message:"+c.ID, c.Conversation, c.Post, message.AuthorHomeTenantID, message.AuthorID, reason, "message_author", at); err != nil {
			return err
		}
	default:
		return chat.ErrInvalidArgument
	}
	if err := c.Close(); err != nil {
		return err
	}
	if c.Reporter != "" {
		// The author who asked for a review reads the decision's own words. Anyone
		// else who raised the item reads the outcome without the note to the author.
		told := reason
		if c.Reporter != message.AuthorID || c.ReporterHome != message.AuthorHomeTenantID {
			told = reporterOutcomeReason(action, reason)
		}
		if err := moderationNotice(ctx, tx, t, "outcome:"+c.ID, c.Conversation, c.Post, c.ReporterHome, c.Reporter, told, action, at); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO chat_moderation_action(tenant_id,case_id,action,actor_id,reason,evidence_ref,at_time) VALUES($1,$2,$3,$4,$5,$6,$7)`, t, c.ID, action, p.SubjectID, reason, "post:"+c.Post, at); err != nil {
		return err
	}
	target := c.Post
	if target == "" {
		target = fmt.Sprintf("conversation:%s", c.Conversation)
	}
	if err := moderationAudit(ctx, tx, p, t, c.Conversation, target, "moderation."+action, reason, message.Revision, at); err != nil {
		return err
	}
	return emitAdapterEvent(ctx, tx, t, c.Conversation, "moderation.resolved", p.TenantID, p.SubjectID, c.Post, message.Revision, map[string]string{"CaseID": c.ID, "Action": action, "ActorID": p.SubjectID, "Reason": reason})
}

// reporterOutcomeReason is what a person who reported a message is told with
// the outcome. What a moderator writes to the author is the author's: the
// dialog says "Only {name} sees this", and the reporter's notice used to carry
// it word for word. The reporter reads the action, the reason chosen from the
// list when the message was removed, and the explanation of a dismissal.
func reporterOutcomeReason(action, reason string) string {
	switch action {
	case "remove":
		if code := strings.SplitN(reason, " — ", 2)[0]; chat.ValidRemovalReason(code) {
			return code
		}
		return "policy_violation"
	case "dismiss":
		return reason
	}
	return ""
}

// closeOpenReports closes the other open items about a message once it has been
// removed (reports) or restored (appeals): the decision answers them all, each
// is recorded with the actor and reason, and each person who raised one is told
// the outcome. except is the item the caller is deciding itself, which the
// caller closes and answers.
func closeOpenReports(ctx context.Context, tx dbport.Tx, p chat.Principal, t, cid, post, except, action, reason string, at time.Time) error {
	rows, err := tx.Query(ctx, `SELECT report_id,reporter_id,COALESCE(NULLIF(reporter_home_tenant_id,''),tenant_id) FROM chat_moderation_report WHERE tenant_id=$1 AND target_id=$2 AND state='OPEN' AND ((reason='appeal')=$3) AND 'report:'||report_id<>$4 ORDER BY report_id FOR UPDATE`, t, post, action == "restore", except)
	if err != nil {
		return err
	}
	type open struct{ id, reporter, home string }
	var found []open
	for rows.Next() {
		var o open
		if err = rows.Scan(&o.id, &o.reporter, &o.home); err != nil {
			rows.Close()
			return err
		}
		found = append(found, o)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, o := range found {
		if _, err = tx.Exec(ctx, `UPDATE chat_moderation_report SET state='CLOSED' WHERE tenant_id=$1 AND report_id=$2`, t, o.id); err != nil {
			return err
		}
		// A restore tells the author itself; the appeal needs no second notice.
		if o.reporter != "" && action != "restore" {
			if err = moderationNotice(ctx, tx, t, "outcome:report:"+o.id, cid, post, o.home, o.reporter, reporterOutcomeReason(action, reason), action, at); err != nil {
				return err
			}
		}
		if _, err = tx.Exec(ctx, `INSERT INTO chat_moderation_action(tenant_id,case_id,action,actor_id,reason,evidence_ref,at_time) VALUES($1,$2,$3,$4,$5,$6,$7)`, t, "report:"+o.id, action, p.SubjectID, reason, "post:"+post, at); err != nil {
			return err
		}
	}
	return nil
}
