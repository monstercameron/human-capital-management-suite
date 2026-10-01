package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// PersonaChannelPolicy is the administrator's current room ceiling. It is
// independent of persona and installation definitions and is read on each use.
type PersonaChannelPolicy struct {
	PlacementClass            string   `json:"placement_class"`
	MaxTier                   string   `json:"max_tier"`
	AllowedDataClasses        []string `json:"allowed_data_classes"`
	AlwaysPrivate             bool     `json:"always_private"`
	ConversationSearchAllowed bool     `json:"conversation_search_allowed"`
	AllowedChannelClasses     []string `json:"allowed_channel_classes"`
	AllowExternalMembers      bool     `json:"allow_external_members"`
	AllowCrossCompanyMembers  bool     `json:"allow_cross_company_members"`
}

type PersonaChannelPolicySnapshot struct {
	TenantID, ConversationID, Kind, ManagerID string
	Revision                                  uint64
	PolicyRevision                            int64
	ExternalMembers, GuestMembers             bool
	Policy                                    PersonaChannelPolicy
}

func validPersonaChannelPolicy(policy PersonaChannelPolicy) bool {
	if !slices.Contains([]string{"T0", "T1", "T2", "T3"}, policy.MaxTier) || !canonicalPersonaPolicyClass(policy.PlacementClass) || len(policy.AllowedDataClasses) == 0 || len(policy.AllowedChannelClasses) == 0 {
		return false
	}
	classes := map[string]bool{}
	for _, class := range policy.AllowedDataClasses {
		if !canonicalPersonaPolicyClass(class) || classes[class] {
			return false
		}
		classes[class] = true
	}
	classes = map[string]bool{}
	for _, class := range policy.AllowedChannelClasses {
		if !slices.Contains([]string{"PUBLIC", "PRIVATE", "GROUP_DM", "ONE_TO_ONE", "EXTERNAL", "CROSS_COMPANY"}, class) || classes[class] {
			return false
		}
		classes[class] = true
	}
	return true
}

func canonicalPersonaPolicyClass(class string) bool {
	if class == "" {
		return false
	}
	for i, value := range class {
		if value >= 'A' && value <= 'Z' {
			continue
		}
		if i > 0 && (value == '_' || value >= '0' && value <= '9') {
			continue
		}
		return false
	}
	return true
}

// PutPersonaChannelPolicy stores an explicit reviewed ceiling with optimistic
// concurrency. The conversation lock is shared by installation and reply.
func (s *Store) PutPersonaChannelPolicy(ctx context.Context, tenantID, conversationID string, expectedRevision int64, policy PersonaChannelPolicy) (int64, error) {
	if s == nil || s.pool == nil || ctx == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(conversationID) == "" || expectedRevision < 0 || !validPersonaChannelPolicy(policy) {
		return 0, ErrAudienceEligibilityUnavailable
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		return 0, err
	}
	var revision int64
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var lifecycle string
		if err := tx.QueryRow(ctx, `SELECT lifecycle FROM chat_conversation WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, conversationID).Scan(&lifecycle); err != nil {
			return err
		}
		if lifecycle != "ACTIVE" {
			return ErrAudienceEligibilityUnavailable
		}
		if expectedRevision == 0 {
			err = tx.QueryRow(ctx, `INSERT INTO chat_persona_channel_policy(tenant_id,conversation_id,revision,policy_json) VALUES($1,$2,1,$3) ON CONFLICT DO NOTHING RETURNING revision`, tenantID, conversationID, raw).Scan(&revision)
		} else {
			err = tx.QueryRow(ctx, `UPDATE chat_persona_channel_policy SET revision=revision+1,policy_json=$3 WHERE tenant_id=$1 AND conversation_id=$2 AND revision=$4 RETURNING revision`, tenantID, conversationID, raw, expectedRevision).Scan(&revision)
		}
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrAudiencePolicyConflict
		}
		return err
	})
	return revision, err
}

// CapturePersonaChannelPolicy resolves policy and audience flags atomically.
// A nonempty managerID additionally requires current same-tenant manager status.
func (s *Store) CapturePersonaChannelPolicy(ctx context.Context, tenantID, conversationID, managerID string) (PersonaChannelPolicySnapshot, error) {
	var out PersonaChannelPolicySnapshot
	if s == nil || s.pool == nil || ctx == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(conversationID) == "" {
		return out, ErrAudienceEligibilityUnavailable
	}
	read := func(tx dbport.Tx) error {
		var lifecycle string
		if err := tx.QueryRow(ctx, `SELECT kind,lifecycle,audience_revision FROM chat_conversation WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, conversationID).Scan(&out.Kind, &lifecycle, &out.Revision); err != nil {
			return err
		}
		if lifecycle != "ACTIVE" {
			return ErrAudienceEligibilityUnavailable
		}
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT revision,policy_json FROM chat_persona_channel_policy WHERE tenant_id=$1 AND conversation_id=$2`, tenantID, conversationID).Scan(&out.PolicyRevision, &raw); err != nil {
			return err
		}
		if json.Unmarshal(raw, &out.Policy) != nil || !validPersonaChannelPolicy(out.Policy) {
			return ErrAudienceEligibilityUnavailable
		}
		if managerID != "" {
			var found bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$1 AND member_id=$3 AND role='MANAGER' AND state='active')`, tenantID, conversationID, managerID).Scan(&found); err != nil {
				return err
			}
			if !found {
				return ErrNotMember
			}
			out.ManagerID = managerID
		}
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND state='active' AND home_tenant_id<>$1), EXISTS(SELECT 1 FROM chat_public_audience_principal p JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.home_tenant_id=p.home_tenant_id AND m.member_id=p.subject_id WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND p.guest AND m.state='active')`, tenantID, conversationID).Scan(&out.ExternalMembers, &out.GuestMembers); err != nil {
			return err
		}
		out.TenantID, out.ConversationID = tenantID, conversationID
		return nil
	}
	if tx, ok := sealedBackgroundTxFromContext(ctx, s, tenantID); ok {
		err := read(tx)
		return out, err
	}
	err := s.RunTenantTx(ctx, tenantID, read)
	return out, err
}
