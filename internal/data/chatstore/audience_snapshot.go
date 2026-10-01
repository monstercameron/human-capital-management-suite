package chatstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var (
	// ErrAudienceChanged means the authority used for a snapshot changed before
	// the fenced operation could commit.
	ErrAudienceChanged = errors.New("chat audience changed")
	// ErrAudienceEligibilityUnavailable means chat cannot prove the external
	// eligibility population in the same transaction as its member rows.
	ErrAudienceEligibilityUnavailable = errors.New("chat audience eligibility cannot be fenced")
)

// AudienceMember is one exact active conversation member. HomeTenantID makes
// guest and external members explicit; an ID alone is never an audience key.
type AudienceMember struct {
	HomeTenantID string `json:"home_tenant_id"`
	MemberID     string `json:"member_id"`
	Role         string `json:"role"`
	Revision     int64  `json:"revision"`
	External     bool   `json:"external"`
}

// AudienceSnapshot is a canonical, tenant-scoped view of chat-owned audience
// authority. Public-channel eligibility is deliberately unavailable until an
// external authority can participate in the same fence.
type AudienceSnapshot struct {
	TenantID             string
	ConversationID       string
	Kind                 string
	Classification       string
	ConversationRevision int64
	PolicyRevision       int64
	EligibilityRevision  string
	Members              []AudienceMember
	Digest               string
	SnapshotID           string
}

type audiencePolicy struct {
	Revision               int64
	RequiredRoles          []string
	RoleMode               int
	RequiredQualifications []string
	AllowedPrincipals      []string
	AllowedTenants         []string
	Classification         string
	Residency              string
}

type audienceCanonical struct {
	TenantID             string           `json:"tenant_id"`
	ConversationID       string           `json:"conversation_id"`
	Kind                 string           `json:"kind"`
	Classification       string           `json:"classification"`
	ConversationRevision int64            `json:"conversation_revision"`
	Policy               audiencePolicy   `json:"policy"`
	Members              []AudienceMember `json:"members"`
}

// CaptureAudienceSnapshot reads the complete chat-owned audience atomically.
// A public channel is refused because its eligibility population lives outside
// chatstore and no shared transaction or lease currently fences that authority.
func (s *Store) CaptureAudienceSnapshot(ctx context.Context, tenantID, conversationID string) (AudienceSnapshot, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(conversationID) == "" {
		return AudienceSnapshot{}, errors.New("tenant and conversation are required")
	}
	if tx, ok := sealedBackgroundTxFromContext(ctx, s, tenantID); ok {
		snapshot, err := readAudienceSnapshot(ctx, tx, tenantID, conversationID, true)
		if err != nil {
			return AudienceSnapshot{}, err
		}
		if strings.EqualFold(snapshot.Kind, "PUBLIC_CHANNEL") || strings.EqualFold(snapshot.Kind, "PUBLIC") {
			return AudienceSnapshot{}, ErrAudienceEligibilityUnavailable
		}
		return snapshot, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AudienceSnapshot{}, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, tenantID); err != nil {
		return AudienceSnapshot{}, err
	}
	snapshot, err := readAudienceSnapshot(ctx, tx, tenantID, conversationID, true)
	if err != nil {
		return AudienceSnapshot{}, err
	}
	if strings.EqualFold(snapshot.Kind, "PUBLIC_CHANNEL") || strings.EqualFold(snapshot.Kind, "PUBLIC") {
		return AudienceSnapshot{}, ErrAudienceEligibilityUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return AudienceSnapshot{}, err
	}
	return snapshot, nil
}

// WithAudienceFence validates a snapshot and runs fn while the conversation
// authority row remains locked. Membership and policy mutations therefore
// cannot commit between validation and the caller's durable chat write.
func (s *Store) WithAudienceFence(ctx context.Context, snapshot AudienceSnapshot, fn func(dbport.Tx) error) error {
	if snapshot.TenantID == "" || snapshot.ConversationID == "" || snapshot.Digest == "" || fn == nil {
		return errors.New("audience snapshot and callback are required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, snapshot.TenantID); err != nil {
		return err
	}
	current, err := readAudienceSnapshot(ctx, tx, snapshot.TenantID, snapshot.ConversationID, true)
	if err != nil {
		return err
	}
	if strings.EqualFold(current.Kind, "PUBLIC_CHANNEL") || strings.EqualFold(current.Kind, "PUBLIC") {
		return ErrAudienceEligibilityUnavailable
	}
	if current.Digest != snapshot.Digest || current.SnapshotID != snapshot.SnapshotID {
		return ErrAudienceChanged
	}
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func readAudienceSnapshot(ctx context.Context, tx dbport.Tx, tenantID, conversationID string, lock bool) (AudienceSnapshot, error) {
	lockClause := ""
	if lock {
		lockClause = " FOR UPDATE"
	}
	var out AudienceSnapshot
	if err := tx.QueryRow(ctx, `SELECT kind,audience_revision FROM chat_conversation WHERE tenant_id=$1 AND id=$2`+lockClause, tenantID, conversationID).Scan(&out.Kind, &out.ConversationRevision); err != nil {
		return out, err
	}
	out.TenantID, out.ConversationID = tenantID, conversationID
	var policy audiencePolicy
	policyLock := ""
	if lock {
		policyLock = " FOR SHARE"
	}
	if err := tx.QueryRow(ctx, `SELECT revision,required_roles,role_mode,required_qualifications,allowed_principals,allowed_tenants,classification,residency FROM chat_channel_policy WHERE tenant_id=$1 AND conversation_id=$2`+policyLock, tenantID, conversationID).Scan(&policy.Revision, &policy.RequiredRoles, &policy.RoleMode, &policy.RequiredQualifications, &policy.AllowedPrincipals, &policy.AllowedTenants, &policy.Classification, &policy.Residency); err != nil {
		if !errors.Is(err, dbport.ErrNoRows) {
			return out, err
		}
		// An absent policy is an absence of current authority. Treating it as
		// an empty policy would admit a result while a policy insert races the
		// fence, so all callers fail closed until policy exists.
		return out, ErrAudienceEligibilityUnavailable
	}
	out.PolicyRevision, out.Classification = policy.Revision, policy.Classification
	rows, err := tx.Query(ctx, `SELECT home_tenant_id,member_id,role,revision FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND state='active' ORDER BY home_tenant_id,member_id`, tenantID, conversationID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var member AudienceMember
		if err := rows.Scan(&member.HomeTenantID, &member.MemberID, &member.Role, &member.Revision); err != nil {
			return out, err
		}
		member.External = member.HomeTenantID != tenantID
		out.Members = append(out.Members, member)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	canonical := audienceCanonical{TenantID: tenantID, ConversationID: conversationID, Kind: out.Kind, Classification: out.Classification, ConversationRevision: out.ConversationRevision, Policy: policy, Members: out.Members}
	// Sorting policy arrays makes equivalent authority state hash identically.
	sort.Strings(canonical.Policy.RequiredRoles)
	sort.Strings(canonical.Policy.RequiredQualifications)
	sort.Strings(canonical.Policy.AllowedPrincipals)
	sort.Strings(canonical.Policy.AllowedTenants)
	raw, err := json.Marshal(canonical)
	if err != nil {
		return out, err
	}
	digest := sha256.Sum256(raw)
	out.Digest = "sha256:" + hex.EncodeToString(digest[:])
	out.SnapshotID = "chat-audience-" + out.Digest
	return out, nil
}
