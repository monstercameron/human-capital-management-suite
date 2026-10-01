package chatstore

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ErrAudiencePolicyConflict means the policy is absent or no longer has the
// revision the caller expected.
var ErrAudiencePolicyConflict = errors.New("chat audience policy revision conflict")

// AudiencePolicy is the explicit tenant-owned policy required before a chat
// audience snapshot can be captured. Empty restrictions are never synthesized
// by the store; callers must choose and persist the policy they intend.
type AudiencePolicy struct {
	RequiredRoles          []string
	RoleMode               int
	RequiredQualifications []string
	AllowedPrincipals      []string
	AllowedTenants         []string
	Classification         string
	Residency              string
}

// PutAudiencePolicy provisions or compare-and-swaps a conversation policy.
// expectedRevision zero means create-if-absent; otherwise the existing row
// must match the supplied revision. Policy and audience revision changes are
// committed together under the conversation row lock.
func (s *Store) PutAudiencePolicy(ctx context.Context, tenantID, conversationID string, expectedRevision int64, policy AudiencePolicy) (int64, error) {
	if s == nil || s.pool == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(conversationID) == "" || expectedRevision < 0 || (policy.RoleMode != 1 && policy.RoleMode != 2) || strings.TrimSpace(policy.Classification) == "" {
		return 0, errors.New("tenant, conversation, explicit policy classification and valid role mode are required")
	}
	policy.RequiredRoles = nonNil(policy.RequiredRoles)
	policy.RequiredQualifications = nonNil(policy.RequiredQualifications)
	policy.AllowedPrincipals = nonNil(policy.AllowedPrincipals)
	policy.AllowedTenants = nonNil(policy.AllowedTenants)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if err := tenant(ctx, tx, tenantID); err != nil {
		return 0, err
	}
	var kind string
	if err := tx.QueryRow(ctx, `SELECT kind FROM chat_conversation WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, conversationID).Scan(&kind); err != nil {
		return 0, err
	}
	if !audiencePolicyConversation(kind) {
		return 0, errors.New("audience policy is supported only for private channels, group DMs and direct messages")
	}
	var revision int64
	if expectedRevision == 0 {
		err = tx.QueryRow(ctx, `INSERT INTO chat_channel_policy(tenant_id,conversation_id,revision,required_roles,role_mode,required_qualifications,allowed_principals,allowed_tenants,classification,residency) VALUES($1,$2,1,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(tenant_id,conversation_id) DO NOTHING RETURNING revision`, tenantID, conversationID, policy.RequiredRoles, policy.RoleMode, policy.RequiredQualifications, policy.AllowedPrincipals, policy.AllowedTenants, policy.Classification, policy.Residency).Scan(&revision)
	} else {
		err = tx.QueryRow(ctx, `UPDATE chat_channel_policy SET revision=revision+1,required_roles=$3,role_mode=$4,required_qualifications=$5,allowed_principals=$6,allowed_tenants=$7,classification=$8,residency=$9 WHERE tenant_id=$1 AND conversation_id=$2 AND revision=$10 RETURNING revision`, tenantID, conversationID, policy.RequiredRoles, policy.RoleMode, policy.RequiredQualifications, policy.AllowedPrincipals, policy.AllowedTenants, policy.Classification, policy.Residency, expectedRevision).Scan(&revision)
	}
	if errors.Is(err, dbport.ErrNoRows) {
		return 0, ErrAudiencePolicyConflict
	}
	if err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `UPDATE chat_conversation SET audience_revision=audience_revision+1 WHERE tenant_id=$1 AND id=$2`, tenantID, conversationID); err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return revision, nil
}

func audiencePolicyConversation(kind string) bool {
	switch strings.ToUpper(strings.TrimSpace(kind)) {
	case "PRIVATE_CHANNEL", "GROUP", "GROUP_DM", "DIRECT", "DIRECT_MESSAGE", "DM":
		return true
	default:
		return false
	}
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
