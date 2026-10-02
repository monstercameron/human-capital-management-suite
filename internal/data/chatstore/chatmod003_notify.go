package chatstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// CHATMOD-003: "notify a named channel". The delivery port was a function that
// returned nil while the editor said the target is told. It now tells the
// managers of the channel the filter names, each by a moderation notice: the
// private notices every person already reads on the Moderation page and is
// counted for in the sidebar. Chat has no author that is not a member, so
// nothing is posted in the named channel itself, and an agent is not a target:
// telling one would start a run nobody asked for.
//
// The notice carries the filter's name and the conversation the message was
// sent in. It carries no text of the message: the hit record holds a digest
// and a masked form only, and the notice holds neither.

// filterTargetName is the channel name a target names: "#security" and
// "security" are the same channel.
func filterTargetName(target string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(target), "#"))
}

// filterTargetChannel finds the channel a target names. With a subject it is
// limited to what that person may know exists: a public channel, or a private
// one they are in. Delivery passes no subject; the filter was checked when it
// was saved.
func filterTargetChannel(ctx context.Context, tx dbport.Tx, tenantID, subject, target string) (string, error) {
	name := filterTargetName(target)
	if name == "" || len(name) > 200 {
		return "", chatfilter.ErrUnknownTarget
	}
	var id string
	err := tx.QueryRow(ctx, `SELECT c.id FROM chat_conversation c WHERE c.tenant_id=$1 AND lower(c.name)=lower($2) AND c.lifecycle<>'ARCHIVED' AND (c.kind='PUBLIC_CHANNEL' OR (c.kind='PRIVATE_CHANNEL' AND ($3='' OR EXISTS(SELECT 1 FROM chat_membership m WHERE m.tenant_id=c.tenant_id AND m.conversation_id=c.id AND m.home_tenant_id=$1 AND m.member_id=$3 AND m.state='active')))) ORDER BY c.kind DESC,c.created_at,c.id LIMIT 1`, tenantID, name, subject).Scan(&id)
	if errors.Is(err, dbport.ErrNoRows) {
		return "", chatfilter.ErrUnknownTarget
	}
	return id, err
}

// ResolveFilterTarget is asked when a "notify" filter is saved: the channel
// must exist, be one the person saving may know of, and have a manager to tell.
func (s *FilterStore) ResolveFilterTarget(ctx context.Context, actor chatfilter.Actor, target string) error {
	if actor.Subject == "" {
		return chatfilter.ErrDenied
	}
	return s.tx(ctx, actor.Tenant, func(tx dbport.Tx) error {
		id, err := filterTargetChannel(ctx, tx, actor.Tenant, actor.Subject, target)
		if err != nil {
			return err
		}
		var managed bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$1 AND role='MANAGER' AND state='active')`, actor.Tenant, id).Scan(&managed); err != nil {
			return err
		}
		if !managed {
			return chatfilter.ErrUnknownTarget
		}
		return nil
	})
}

// notifyFilterTarget writes one notice for each manager of the named channel.
// A channel renamed or archived since the filter was saved has nobody to tell;
// the hit is still in chat_filter_hit, so that is not an error for the person
// whose message matched.
func (s *FilterStore) notifyFilterTarget(ctx context.Context, r chatfilter.Record) error {
	return s.tx(ctx, r.Tenant, func(tx dbport.Tx) error {
		target, err := filterTargetChannel(ctx, tx, r.Tenant, "", r.Hit.Target)
		if errors.Is(err, chatfilter.ErrUnknownTarget) {
			return nil
		}
		if err != nil {
			return err
		}
		// The notice is about the conversation the message was sent in. A hit
		// outside a conversation (a display name, a gate answer) has none, and the
		// notice table requires one: the named channel stands in.
		about := r.Channel
		var known bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_conversation WHERE tenant_id=$1 AND id=$2)`, r.Tenant, about).Scan(&known); err != nil {
			return err
		}
		if !known {
			about = target
		}
		id := fmt.Sprintf("filter:%s:%s:%d", r.Hit.RuleID, strings.TrimPrefix(r.Hit.Digest, "sha256:"), r.At.UnixNano())
		_, err = tx.Exec(ctx, `INSERT INTO chat_moderation_notice(tenant_id,id,conversation_id,post_id,home_tenant_id,subject_id,reason,outcome,at_time) SELECT $1,$2,$3,'',$1,m.member_id,$4,$5,$6 FROM chat_membership m WHERE m.tenant_id=$1 AND m.conversation_id=$7 AND m.home_tenant_id=$1 AND m.role='MANAGER' AND m.state='active' ON CONFLICT DO NOTHING`, r.Tenant, id, about, r.Hit.RuleName, chat.ModerationOutcomeFilterNotify, r.At, target)
		return err
	})
}

var _ chatfilter.TargetResolver = (*FilterStore)(nil)
