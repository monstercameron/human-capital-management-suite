package chatstore

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// PublicAudiencePrincipal is one explicitly admitted present or future reader.
// HomeTenantID is part of the identity, including for guests and externals.
type PublicAudiencePrincipal struct {
	HomeTenantID string
	SubjectID    string
	Guest        bool
}

// PublicAudiencePolicy is the complete admission population of a governed
// public channel. It is authoritative in chat, rather than a cached assertion
// that a separately stored worker population cannot change.
type PublicAudiencePolicy struct {
	Classification string
	Principals     []PublicAudiencePrincipal
}

// PublicAudienceSnapshot enumerates present and eligible readers under the
// same conversation authority row used by CommitPersonaReply.
type PublicAudienceSnapshot struct {
	TenantID, ConversationID, Name, Classification string
	Revision                                       uint64
	PolicyRevision                                 int64
	Current, Eligible                              []PublicAudiencePrincipal
}

// PutPublicAudiencePolicy explicitly provisions or replaces complete channel
// admission authority. All current readers must remain in the population.
func (s *Store) PutPublicAudiencePolicy(ctx context.Context, tenantID, conversationID string, expectedRevision int64, policy PublicAudiencePolicy) (int64, error) {
	if s == nil || s.pool == nil || ctx == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(conversationID) == "" || expectedRevision < 0 || !validPublicAudiencePolicy(policy) {
		return 0, ErrAudienceEligibilityUnavailable
	}
	var revision int64
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var kind, lifecycle string
		if err := tx.QueryRow(ctx, `SELECT kind,lifecycle FROM chat_conversation WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, conversationID).Scan(&kind, &lifecycle); err != nil {
			return err
		}
		if kind != "PUBLIC_CHANNEL" || lifecycle != "ACTIVE" {
			return ErrAudienceEligibilityUnavailable
		}
		var present int64
		err := tx.QueryRow(ctx, `SELECT revision FROM chat_public_audience_policy WHERE tenant_id=$1 AND conversation_id=$2`, tenantID, conversationID).Scan(&present)
		if err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		if present != expectedRevision {
			return ErrAudiencePolicyConflict
		}
		rows, err := tx.Query(ctx, `SELECT home_tenant_id,member_id FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND state='active'`, tenantID, conversationID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var home, subject string
			if err := rows.Scan(&home, &subject); err != nil {
				rows.Close()
				return err
			}
			if !publicAudienceContains(policy.Principals, home, subject) {
				rows.Close()
				return ErrAudienceEligibilityUnavailable
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		revision = present + 1
		if _, err := tx.Exec(ctx, `INSERT INTO chat_public_audience_policy(tenant_id,conversation_id,revision,classification) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,conversation_id) DO UPDATE SET revision=EXCLUDED.revision,classification=EXCLUDED.classification`, tenantID, conversationID, revision, policy.Classification); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM chat_public_audience_principal WHERE tenant_id=$1 AND conversation_id=$2`, tenantID, conversationID); err != nil {
			return err
		}
		for _, member := range policy.Principals {
			if _, err := tx.Exec(ctx, `INSERT INTO chat_public_audience_principal(tenant_id,conversation_id,home_tenant_id,subject_id,guest) VALUES($1,$2,$3,$4,$5)`, tenantID, conversationID, member.HomeTenantID, member.SubjectID, member.Guest); err != nil {
				return err
			}
		}
		return nil
	})
	return revision, err
}

func validPublicAudiencePolicy(policy PublicAudiencePolicy) bool {
	if (policy.Classification != "PUBLIC" && policy.Classification != "INTERNAL") || len(policy.Principals) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, member := range policy.Principals {
		key := member.HomeTenantID + "\x00" + member.SubjectID
		if strings.TrimSpace(member.HomeTenantID) == "" || strings.TrimSpace(member.SubjectID) == "" || seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

func publicAudienceContains(members []PublicAudiencePrincipal, home, subject string) bool {
	for _, member := range members {
		if member.HomeTenantID == home && member.SubjectID == subject {
			return true
		}
	}
	return false
}

// CapturePublicAudienceSnapshot never infers tenant-wide eligibility. Missing
// explicit admission authority, archived rooms and omitted members fail closed.
func (s *Store) CapturePublicAudienceSnapshot(ctx context.Context, tenantID, conversationID string) (PublicAudienceSnapshot, error) {
	var out PublicAudienceSnapshot
	if s == nil || s.pool == nil || ctx == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(conversationID) == "" {
		return out, ErrAudienceEligibilityUnavailable
	}
	read := func(tx dbport.Tx) error {
		var kind, lifecycle string
		if err := tx.QueryRow(ctx, `SELECT kind,lifecycle,name,audience_revision FROM chat_conversation WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, conversationID).Scan(&kind, &lifecycle, &out.Name, &out.Revision); err != nil {
			return err
		}
		if kind != "PUBLIC_CHANNEL" || lifecycle != "ACTIVE" {
			return ErrAudienceEligibilityUnavailable
		}
		if err := tx.QueryRow(ctx, `SELECT revision,classification FROM chat_public_audience_policy WHERE tenant_id=$1 AND conversation_id=$2`, tenantID, conversationID).Scan(&out.PolicyRevision, &out.Classification); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT home_tenant_id,subject_id,guest FROM chat_public_audience_principal WHERE tenant_id=$1 AND conversation_id=$2 ORDER BY home_tenant_id,subject_id`, tenantID, conversationID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var member PublicAudiencePrincipal
			if err := rows.Scan(&member.HomeTenantID, &member.SubjectID, &member.Guest); err != nil {
				rows.Close()
				return err
			}
			out.Eligible = append(out.Eligible, member)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `SELECT m.home_tenant_id,m.member_id,p.guest FROM chat_membership m LEFT JOIN chat_public_audience_principal p ON p.tenant_id=m.tenant_id AND p.conversation_id=m.conversation_id AND p.home_tenant_id=m.home_tenant_id AND p.subject_id=m.member_id WHERE m.tenant_id=$1 AND m.conversation_id=$2 AND m.state='active' ORDER BY m.home_tenant_id,m.member_id`, tenantID, conversationID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var member PublicAudiencePrincipal
			var guest *bool
			if err := rows.Scan(&member.HomeTenantID, &member.SubjectID, &guest); err != nil {
				rows.Close()
				return err
			}
			if guest == nil {
				rows.Close()
				return ErrAudienceEligibilityUnavailable
			}
			member.Guest = *guest
			out.Current = append(out.Current, member)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(out.Current) == 0 || !validPublicAudiencePolicy(PublicAudiencePolicy{Classification: out.Classification, Principals: out.Eligible}) {
			return ErrAudienceEligibilityUnavailable
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

// WithConversationAuthorityFence holds current active conversation authority
// through fn. Placement owners use it to serialize installation with changes
// to channel lifecycle, policy or admission population.
func (s *Store) WithConversationAuthorityFence(ctx context.Context, tenantID, conversationID string, expectedRevision uint64, fn func(dbport.Tx) error) error {
	if s == nil || s.pool == nil || ctx == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(conversationID) == "" || expectedRevision == 0 || fn == nil {
		return ErrAudienceEligibilityUnavailable
	}
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var revision uint64
		var lifecycle string
		if err := tx.QueryRow(ctx, `SELECT audience_revision,lifecycle FROM chat_conversation WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, conversationID).Scan(&revision, &lifecycle); err != nil {
			return err
		}
		if revision != expectedRevision || lifecycle != "ACTIVE" {
			return ErrAudienceChanged
		}
		return fn(tx)
	})
}
