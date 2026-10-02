package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// EnsurePersonaDirectPolicies gives a direct conversation with an agent the
// two policies it needs before a question there can be admitted (AGENTUX-038):
// its audience policy and its one-to-one agent policy. Both are written in one
// transaction under the conversation's row lock, so the conversation is never
// left with one and not the other: either write failing leaves neither.
//
// A policy that is already there is left exactly as it is; an administrator may
// have narrowed it. It reports which of the two it created.
func (s *Store) EnsurePersonaDirectPolicies(ctx context.Context, tenantID, conversationID string, audience AudiencePolicy, agent PersonaChannelPolicy) (audienceCreated, agentCreated bool, err error) {
	if s == nil || s.pool == nil || ctx == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(conversationID) == "" ||
		(audience.RoleMode != 1 && audience.RoleMode != 2) || strings.TrimSpace(audience.Classification) == "" || !validPersonaChannelPolicy(agent) {
		return false, false, ErrAudienceEligibilityUnavailable
	}
	raw, err := json.Marshal(agent)
	if err != nil {
		return false, false, err
	}
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		audienceCreated, agentCreated = false, false
		var kind, lifecycle string
		if err := tx.QueryRow(ctx, `SELECT kind,lifecycle FROM chat_conversation WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, conversationID).Scan(&kind, &lifecycle); err != nil {
			return err
		}
		switch strings.ToUpper(strings.TrimSpace(kind)) {
		case "DIRECT", "DIRECT_MESSAGE", "DM":
		default:
			// The one-to-one policy is for a direct conversation and no other.
			return ErrAudienceEligibilityUnavailable
		}
		if lifecycle != "ACTIVE" {
			return ErrAudienceEligibilityUnavailable
		}
		// Every writer of these two tables holds the conversation's row lock, so
		// what is read here still holds when the rows are written below. A policy
		// that exists is not written at all: this runs before every private
		// delivery, and an insert that is then discarded still moves the
		// conversation's audience revision, which would make the snapshot a run
		// took a moment ago look out of date.
		var hasAudience, hasAgent bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_channel_policy WHERE tenant_id=$1 AND conversation_id=$2), EXISTS(SELECT 1 FROM chat_persona_channel_policy WHERE tenant_id=$1 AND conversation_id=$2)`, tenantID, conversationID).Scan(&hasAudience, &hasAgent); err != nil {
			return err
		}
		var revision int64
		if !hasAudience {
			err := tx.QueryRow(ctx, `INSERT INTO chat_channel_policy(tenant_id,conversation_id,revision,required_roles,role_mode,required_qualifications,allowed_principals,allowed_tenants,classification,residency) VALUES($1,$2,1,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(tenant_id,conversation_id) DO NOTHING RETURNING revision`,
				tenantID, conversationID, nonNil(audience.RequiredRoles), audience.RoleMode, nonNil(audience.RequiredQualifications), nonNil(audience.AllowedPrincipals), nonNil(audience.AllowedTenants), audience.Classification, audience.Residency).Scan(&revision)
			switch {
			case err == nil:
				audienceCreated = true
				// A new audience policy is a change of who may be here, like any other.
				if _, err := tx.Exec(ctx, `UPDATE chat_conversation SET audience_revision=audience_revision+1 WHERE tenant_id=$1 AND id=$2`, tenantID, conversationID); err != nil {
					return err
				}
			case !errors.Is(err, dbport.ErrNoRows):
				return err
			}
		}
		if !hasAgent {
			err := tx.QueryRow(ctx, `INSERT INTO chat_persona_channel_policy(tenant_id,conversation_id,revision,policy_json) VALUES($1,$2,1,$3) ON CONFLICT DO NOTHING RETURNING revision`, tenantID, conversationID, raw).Scan(&revision)
			switch {
			case err == nil:
				agentCreated = true
			case !errors.Is(err, dbport.ErrNoRows):
				return err
			}
		}
		return nil
	})
	if err != nil {
		return false, false, err
	}
	return audienceCreated, agentCreated, nil
}
