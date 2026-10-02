package chatstore

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func (s *ModeratedAdapter) SearchModeration(ctx context.Context, p chat.Principal, t, query string) ([]chat.ModerationItem, error) {
	if p.TenantID != t || p.SubjectID == "" || len(query) > 512 {
		return nil, chat.ErrPermissionDenied
	}
	var out []chat.ModerationItem
	err := s.removalTx(ctx, t, func(tx dbport.Tx) error {
		var err error
		out, err = moderationOpenItems(ctx, tx, p, t, query, true, true)
		return err
	})
	if err != nil {
		return nil, err
	}
	sortModerationItems(out)
	return out, nil
}

func (s *ModeratedAdapter) ResolveModeration(ctx context.Context, p chat.Principal, t, id, action, reason string, at time.Time) error {
	if strings.TrimSpace(reason) == "" || len(reason) > 2000 {
		return chat.ErrInvalidArgument
	}
	return s.removalTx(ctx, t, func(tx dbport.Tx) error {
		c := moderationCase{ID: id}
		var err error
		isReport := strings.HasPrefix(id, "report:")
		if isReport {
			err = tx.QueryRow(ctx, `SELECT conversation_id,target_id,reporter_id,COALESCE(NULLIF(reporter_home_tenant_id,''),tenant_id),state FROM chat_moderation_report WHERE tenant_id=$1 AND report_id=$2 FOR UPDATE`, t, strings.TrimPrefix(id, "report:")).Scan(&c.Conversation, &c.Post, &c.Reporter, &c.ReporterHome, &c.State)
		} else if strings.HasPrefix(id, "removal:") {
			err = tx.QueryRow(ctx, `SELECT conversation_id,post_id,queue_state FROM chat_admin_removal WHERE tenant_id=$1 AND post_id=$2 FOR UPDATE`, t, strings.TrimPrefix(id, "removal:")).Scan(&c.Conversation, &c.Post, &c.State)
		} else {
			return chat.ErrInvalidArgument
		}
		if errors.Is(err, dbport.ErrNoRows) {
			return chat.ErrNotFound
		}
		if err != nil {
			return err
		}
		c.Close = func() error {
			var e error
			if isReport {
				_, e = tx.Exec(ctx, `UPDATE chat_moderation_report SET state='CLOSED' WHERE tenant_id=$1 AND report_id=$2`, t, strings.TrimPrefix(id, "report:"))
			} else {
				_, e = tx.Exec(ctx, `UPDATE chat_admin_removal SET queue_state='CLOSED',revision=revision+1 WHERE tenant_id=$1 AND conversation_id=$2 AND post_id=$3`, t, c.Conversation, c.Post)
			}
			return e
		}
		return s.decideModeration(ctx, tx, p, t, c, action, reason, at)
	})
}

// AssignModerationPermission is the role assignment boundary. Only a current
// projected workspace administrator may assign, and private room membership is
// still required. Empty conversation assigns the workspace default.
func (s *ModeratedAdapter) AssignModerationPermission(ctx context.Context, p chat.Principal, t, cid, role, permission string, allowed bool) error {
	if p.TenantID != t || p.SubjectID == "" || role == "" || !chat.ValidModerationPermission(permission) {
		return chat.ErrInvalidArgument
	}
	return s.removalTx(ctx, t, func(tx dbport.Tx) error {
		var current string
		if err := tx.QueryRow(ctx, `SELECT role FROM chat_moderation_role WHERE tenant_id=$1 AND subject_id=$2 AND role='WORKSPACE_ADMIN' FOR SHARE`, t, p.SubjectID).Scan(&current); err != nil {
			return chat.ErrPermissionDenied
		}
		if cid != "" {
			if err := fenceContextWrite(ctx, tx, t, cid); err != nil {
				return err
			}
			if err := moderationPermission(ctx, tx, p, t, cid, chat.PermissionManageFilters); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `INSERT INTO chat_moderation_permission(tenant_id,conversation_id,role,permission,allowed) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,conversation_id,role,permission) DO UPDATE SET allowed=EXCLUDED.allowed,revision=chat_moderation_permission.revision+1`, t, cid, role, permission, allowed)
		return err
	})
}

func (s *ModeratedAdapter) CanModerate(ctx context.Context, p chat.Principal, t, cid, permission string) error {
	return s.removalTx(ctx, t, func(tx dbport.Tx) error { return moderationPermission(ctx, tx, p, t, cid, permission) })
}

func (s *ModeratedAdapter) CanReportMessage(ctx context.Context, p chat.Principal, t, cid, id string) error {
	return s.removalTx(ctx, t, func(tx dbport.Tx) error {
		if err := moderationPermission(ctx, tx, p, t, cid, chat.PermissionReport); err != nil {
			return err
		}
		_, err := moderationPost(ctx, tx, t, cid, id, p.SubjectID, false)
		return err
	})
}

// QueueTool grants read-only access to an agent through a skill's explicit
// authority. The agent receives summaries with reporter identity stripped.
type QueueSkillGrant interface {
	AuthorizeModerationSummary(context.Context, chat.Principal, string, string) (chat.Principal, error)
}

type chatremoveSkillKey struct{}
type chatremoveSkillDelegate struct{ Tenant, Subject string }

func chatremoveSkillAuthority(ctx context.Context, agent chat.Principal, t, skill string, grant QueueSkillGrant) (context.Context, chat.Principal, error) {
	if grant == nil || skill == "" || agent.TenantID != t {
		return ctx, chat.Principal{}, chat.ErrPermissionDenied
	}
	machine, err := machineActor(ctx, agent.TenantID, agent.SubjectID)
	if err != nil || !machine {
		return ctx, chat.Principal{}, chat.ErrPermissionDenied
	}
	reviewer, err := grant.AuthorizeModerationSummary(ctx, agent, t, skill)
	if err != nil || reviewer.TenantID != t || reviewer.SubjectID == "" {
		return ctx, chat.Principal{}, chat.ErrPermissionDenied
	}
	return context.WithValue(ctx, chatremoveSkillKey{}, chatremoveSkillDelegate{Tenant: reviewer.TenantID, Subject: reviewer.SubjectID}), reviewer, nil
}

func (s *ModeratedAdapter) QueueTool(ctx context.Context, agent chat.Principal, t, skill string, grant QueueSkillGrant) ([]chat.ModerationItem, error) {
	readCtx, reviewer, err := chatremoveSkillAuthority(ctx, agent, t, skill, grant)
	if err != nil {
		return nil, err
	}
	items, err := s.SearchModeration(readCtx, reviewer, t, "")
	for i := range items {
		items[i].ReporterID = ""
		items[i].CanRemove = false
		items[i].CanRestore = false
		items[i].ID = fmt.Sprintf("summary:%d", i)
	}
	return items, err
}

func (s *ModeratedAdapter) FlagQueueTool(ctx context.Context, agent chat.Principal, t, skill, cid, post, reason string, grant QueueSkillGrant) error {
	readCtx, reviewer, err := chatremoveSkillAuthority(ctx, agent, t, skill, grant)
	if err != nil {
		return err
	}
	if strings.TrimSpace(reason) == "" || len(reason) > 2000 {
		return chat.ErrInvalidArgument
	}
	return s.removalTx(readCtx, t, func(tx dbport.Tx) error {
		if err := moderationPermission(readCtx, tx, reviewer, t, cid, chat.PermissionReport); err != nil {
			return err
		}
		message, err := moderationPost(readCtx, tx, t, cid, post, reviewer.SubjectID, false)
		if err != nil {
			return err
		}
		at := time.Now().UTC()
		id := uuid.NewString()
		if _, err = tx.Exec(readCtx, `INSERT INTO chat_moderation_report(tenant_id,report_id,conversation_id,target_id,reporter_id,reason,created_at,state) VALUES($1,$2,$3,$4,$5,$6,$7,'OPEN')`, t, id, cid, post, agent.SubjectID, reason, at); err != nil {
			return err
		}
		return moderationAudit(readCtx, tx, agent, t, cid, post, "moderation.flag", reason+"; skill="+skill, message.Revision, at)
	})
}
