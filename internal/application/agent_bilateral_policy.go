package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/bilateral"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// AgentBilateralApprovalScope is issued by each company's trust policy owner.
// It is not an invocation payload or a substitute for a service credential.
// Host and home independently bind the exact terms and membership image.
type AgentBilateralApprovalScope struct {
	SchemaVersion    uint32          `json:"schema_version"`
	Kind             string          `json:"kind"`
	AgentPrincipalID string          `json:"agent_principal_id"`
	SponsorID        string          `json:"sponsor_id"`
	GrantID          string          `json:"grant_id"`
	GrantVersion     uint64          `json:"grant_version"`
	MembershipDigest string          `json:"membership_digest"`
	Terms            bilateral.Terms `json:"terms"`
}

type AgentBilateralPolicy interface {
	CheckAgentBilateral(context.Context, agentrun.Request) (string, error)
}

// AgentBilateralAuthority intersects ordinary sponsored authority with current
// chat and company policy. Install, admission, claim, tool and output owners
// use the same wrapper rather than trusting an earlier accepted snapshot.
type AgentBilateralAuthority struct {
	Delegate agentrun.Authority
	Policy   AgentBilateralPolicy
}

func (a AgentBilateralAuthority) VerifyAdmission(ctx context.Context, req agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	if a.Delegate == nil || a.Policy == nil {
		return agentrun.AuthoritySnapshot{}, agentrun.ErrAuthorityMissing
	}
	snapshot, err := a.Delegate.VerifyAdmission(ctx, req)
	if err != nil {
		return snapshot, err
	}
	proof, err := a.Policy.CheckAgentBilateral(ctx, req)
	if err != nil {
		return agentrun.AuthoritySnapshot{}, err
	}
	sum := sha256.Sum256([]byte(snapshot.PolicyDigest + ":" + proof))
	snapshot.PolicyDigest = "sha256:" + hex.EncodeToString(sum[:])
	return snapshot, nil
}
func (a AgentBilateralAuthority) CheckCurrent(ctx context.Context, req agentrun.Request) error {
	_, err := a.VerifyAdmission(ctx, req)
	return err
}

// AgentBilateralBindingPolicy gates installation/publication and source-owner
// dispatch using binding-only authority. It avoids re-entering the source
// verifier while that same owner is checking a native occurrence.
type AgentBilateralBindingPolicy struct {
	Delegate interface {
		CheckCurrent(context.Context, agentrun.Request) error
	}
	Policy AgentBilateralPolicy
}

func (p AgentBilateralBindingPolicy) CheckCurrent(ctx context.Context, req agentrun.Request) error {
	if p.Delegate == nil || p.Policy == nil {
		return agentrun.ErrAuthorityMissing
	}
	if err := p.Delegate.CheckCurrent(ctx, req); err != nil {
		return err
	}
	_, err := p.Policy.CheckAgentBilateral(ctx, req)
	return err
}

// DatabaseAgentBilateralPolicy reads the chat owner and the trust owner in
// separate tenant-scoped transactions. No cross-company data join is used.
type DatabaseAgentBilateralPolicy struct {
	CoreDB     dbport.Beginner
	ChatDB     dbport.Beginner
	TenantUUID func(values.TenantId) uuid.UUID
	Now        func() time.Time
}

type agentBilateralChatGrant struct {
	ID        string
	Version   uint64
	Region    string
	ExpiresAt time.Time
}

func (p DatabaseAgentBilateralPolicy) CheckAgentBilateral(ctx context.Context, req agentrun.Request) (string, error) {
	if ctx == nil || p.CoreDB == nil || p.ChatDB == nil || p.TenantUUID == nil || p.Now == nil {
		return "", agentrun.ErrAuthorityMissing
	}
	now := p.Now().UTC()
	if now.IsZero() {
		return "", bilateral.ErrDenied
	}
	tx, err := p.ChatDB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	host, conversation := req.Source.TenantID, req.Audience.ID
	if _, err = tx.Exec(ctx, "SELECT set_config('hcmnext.tenant_id',$1,true)", host); err != nil {
		return "", err
	}
	var lifecycle string
	if err = tx.QueryRow(ctx, "SELECT lifecycle FROM chat_conversation WHERE tenant_id=$1 AND id=$2", host, conversation).Scan(&lifecycle); err != nil || lifecycle != "ACTIVE" {
		return "", bilateral.ErrDenied
	}
	rows, err := tx.Query(ctx, `SELECT home_tenant_id,member_id,revision,state,COALESCE(left_at::text,'') FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 ORDER BY home_tenant_id,member_id`, host, conversation)
	if err != nil {
		return "", err
	}
	var members []string
	var membership []any
	seen := map[string]bool{host: true}
	for rows.Next() {
		var home, id, state, left string
		var revision uint64
		if err = rows.Scan(&home, &id, &revision, &state, &left); err != nil {
			rows.Close()
			return "", err
		}
		membership = append(membership, []any{home, id, revision, state, left})
		if home == "" {
			home = host
		}
		if state == "active" && left == "" && !seen[home] {
			members = append(members, home)
			seen[home] = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	members = append(members, host)
	sort.Strings(members)
	if len(members) == 1 {
		return "LOCAL_AUDIENCE", nil
	}
	raw, _ := json.Marshal(membership)
	sum := sha256.Sum256(raw)
	membershipDigest := "sha256:" + hex.EncodeToString(sum[:])
	revision := binary.BigEndian.Uint64(sum[:8])
	if revision == 0 {
		revision = 1
	}
	grants := make([]bilateral.Grant, 0, len(members)-1)
	for _, home := range members {
		if home == host {
			continue
		}
		var chat agentBilateralChatGrant
		if err = tx.QueryRow(ctx, `SELECT id,version,residency,expires_at FROM chat_share_grant WHERE tenant_id=$1 AND conversation_id=$2 AND consumer_tenant=$3 AND accepted_at IS NOT NULL AND revoked_at IS NULL AND expires_at>$4`, host, conversation, home, now).Scan(&chat.ID, &chat.Version, &chat.Region, &chat.ExpiresAt); err != nil {
			return "", bilateral.ErrDenied
		}
		hostScope, hostAt, err := p.readApproval(ctx, host, req, home, chat, membershipDigest, now)
		if err != nil {
			return "", err
		}
		homeScope, homeAt, err := p.readApproval(ctx, home, req, home, chat, membershipDigest, now)
		if err != nil {
			return "", err
		}
		hostDigest, err := bilateral.TermsDigest(hostScope.Terms)
		if err != nil {
			return "", err
		}
		homeDigest, err := bilateral.TermsDigest(homeScope.Terms)
		if err != nil || homeDigest != hostDigest {
			return "", bilateral.ErrDenied
		}
		grants = append(grants, bilateral.Grant{ID: chat.ID, Version: chat.Version, Terms: hostScope.Terms, HostConsent: bilateral.Consent{TenantID: host, GrantVersion: chat.Version, TermsDigest: hostDigest, AcceptedAt: hostAt}, HomeConsent: bilateral.Consent{TenantID: home, GrantVersion: chat.Version, TermsDigest: homeDigest, AcceptedAt: homeAt}})
	}
	terms := grants[0].Terms
	install, err := bilateral.AuthorizeInstall(bilateral.InstallRequest{ConversationID: conversation, HostTenant: host, AgentID: req.Agent.AgentID, AgentVersion: req.Agent.Version, InstallationID: req.InstallationID, Purpose: req.Purpose,
		ProcessingRegion: terms.ProcessingRegion, Retention: terms.Retention, Egress: terms.Egress, IncidentContact: terms.IncidentContact, ExitBehavior: terms.ExitBehavior, ExpiresAt: terms.ExpiresAt, MemberTenants: members, MembershipRevision: revision, Grants: grants, Now: now})
	if err != nil {
		return "", err
	}
	if err = bilateral.AuthorizeOutput(bilateral.OutputRequest{Installation: install, AudienceTenants: members, CurrentMembershipRevision: revision, CurrentGrants: grants, Now: now}); err != nil {
		return "", err
	}
	proof, _ := json.Marshal([]any{install, membershipDigest})
	digest := sha256.Sum256(proof)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (p DatabaseAgentBilateralPolicy) readApproval(ctx context.Context, company string, req agentrun.Request, home string, chat agentBilateralChatGrant, membership string, now time.Time) (AgentBilateralApprovalScope, time.Time, error) {
	tenant := p.TenantUUID(values.TenantId(company))
	if tenant == uuid.Nil {
		return AgentBilateralApprovalScope{}, time.Time{}, bilateral.ErrDenied
	}
	tx, err := p.CoreDB.Begin(ctx)
	if err != nil {
		return AgentBilateralApprovalScope{}, time.Time{}, err
	}
	defer tx.Rollback(ctx)
	if err = tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return AgentBilateralApprovalScope{}, time.Time{}, err
	}
	rows, err := tx.Query(ctx, `SELECT b.scope,b.valid_from FROM authority_binding b JOIN principal p ON p.tenant_id=b.tenant_id AND p.principal_id=b.principal_id JOIN authority_source s ON s.tenant_id=b.tenant_id AND s.authority_source_id=b.authority_source_id
 WHERE b.tenant_id=$1 AND p.lifecycle='ACTIVE' AND (p.expires_at IS NULL OR p.expires_at>$2) AND b.valid_from<=$2 AND (b.valid_to IS NULL OR b.valid_to>$2)
 AND s.kind='POLICY_BUNDLE' AND s.version>0 AND p.revocation_epoch>0 AND s.content_digest ~ '^[0-9a-f]{64}$'
 AND s.valid_interval @> $2::timestamptz AND b.scope->>'kind'='AGENT_BILATERAL_APPROVAL'`, tenant, now)
	if err != nil {
		return AgentBilateralApprovalScope{}, time.Time{}, err
	}
	defer rows.Close()
	var selected AgentBilateralApprovalScope
	var accepted time.Time
	found := false
	for rows.Next() {
		var raw []byte
		var at time.Time
		if err = rows.Scan(&raw, &at); err != nil {
			return selected, accepted, err
		}
		var scope AgentBilateralApprovalScope
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&scope) != nil || decoder.Decode(new(any)) != io.EOF {
			continue
		}
		terms := scope.Terms
		if !slices.Contains(terms.Egress, "chat") || !slices.Contains(terms.Egress, "agent-output") {
			continue
		}
		if scope.SchemaVersion != 1 || scope.Kind != "AGENT_BILATERAL_APPROVAL" || scope.AgentPrincipalID != req.Principal.AgentPrincipalID || scope.SponsorID != req.Principal.SponsorID || scope.GrantID != chat.ID || scope.GrantVersion != chat.Version || scope.MembershipDigest != membership || terms.HostTenant != req.Source.TenantID || terms.ConsumerTenant != home || terms.ConversationID != req.Audience.ID || terms.AgentID != req.Agent.AgentID || terms.AgentVersion != req.Agent.Version || terms.InstallationID != req.InstallationID || terms.Purpose != req.Purpose || terms.ProcessingRegion != chat.Region || terms.ExpiresAt.After(chat.ExpiresAt) {
			continue
		}
		if found {
			return selected, accepted, fmt.Errorf("%w: ambiguous company approval", bilateral.ErrDenied)
		}
		found = true
		selected = scope
		accepted = at
	}
	if err = rows.Err(); err != nil {
		return selected, accepted, err
	}
	if !found {
		return selected, accepted, bilateral.ErrDenied
	}
	return selected, accepted, nil
}
