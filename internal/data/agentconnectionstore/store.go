// Package agentconnectionstore persists the agentconnect.Registry state in
// PostgreSQL: immutable connection revisions, per-user external account links
// (credential references and custody handles only, never secret material),
// user and connection epochs, and connection revocation evidence. Every call
// runs in a transaction bound to one tenant (tenancy.WithTenant), so the
// tenant_isolation policies apply. Credential leases are not stored; they are
// short-lived and a restart invalidates them.
package agentconnectionstore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentconnect"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

// callTimeout bounds each Store call, because agentconnect.Store carries no
// context.
const callTimeout = 30 * time.Second

// DB is the transaction opener the store needs.
type DB interface{ dbport.Beginner }

// Store implements agentconnect.Store.
type Store struct {
	db         DB
	tenantUUID func(values.TenantId) uuid.UUID
}

var _ agentconnect.Store = (*Store)(nil)

// New builds a Store. tenantUUID maps a tenant key to its canonical UUID and
// must return uuid.Nil for unknown tenants.
func New(db DB, tenantUUID func(values.TenantId) uuid.UUID) (*Store, error) {
	if db == nil || tenantUUID == nil {
		return nil, fmt.Errorf("%w: database and canonical tenant mapper are required", agentconnect.ErrInvalid)
	}
	return &Store{db: db, tenantUUID: tenantUUID}, nil
}

type skillDoc struct {
	ID                  string   `json:"id"`
	Version             string   `json:"version"`
	Tier                string   `json:"tier"`
	CredentialOperation string   `json:"credential_operation"`
	SharedRead          bool     `json:"shared_read"`
	RecordFilter        string   `json:"record_filter"`
	ToolName            string   `json:"tool_name"`
	ToolCapability      string   `json:"tool_capability"`
	ToolVersion         uint32   `json:"tool_version"`
	ToolClass           string   `json:"tool_class"`
	ToolDataScope       []string `json:"tool_data_scope"`
	ToolCost            int      `json:"tool_cost"`
	ToolSchema          string   `json:"tool_schema"`
}

type grantDoc struct {
	ID                 string   `json:"id"`
	Roles              []string `json:"roles"`
	Population         string   `json:"population"`
	OrganizationScopes []string `json:"organization_scopes"`
	Skills             []string `json:"skills"`
}

type approvalDoc struct {
	RequestedBy string    `json:"requested_by"`
	ApprovedBy  string    `json:"approved_by"`
	RequestHash string    `json:"request_hash"`
	ApprovedAt  time.Time `json:"approved_at"`
	StepUp      bool      `json:"step_up"`
}

func (s *Store) begin(ctx context.Context, tenant string) (dbport.Tx, uuid.UUID, error) {
	if s == nil || s.db == nil || s.tenantUUID == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(tenant) != tenant {
		return nil, uuid.Nil, agentconnect.ErrInvalid
	}
	id := s.tenantUUID(values.TenantId(tenant))
	if id == uuid.Nil {
		return nil, uuid.Nil, fmt.Errorf("%w: unknown tenant %q", agentconnect.ErrInvalid, tenant)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("agentconnectionstore: begin: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, id); err != nil {
		_ = tx.Rollback(ctx)
		return nil, uuid.Nil, err
	}
	return tx, id, nil
}

// PutRevision inserts a new immutable revision at epoch one.
func (s *Store) PutRevision(record agentconnect.RevisionRecord) error {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	rev := record.Revision
	if rev.ID == "" || record.ConnectorID == "" || record.ConnectorVersion == "" {
		return fmt.Errorf("%w: revision record is incomplete", agentconnect.ErrInvalid)
	}
	skills := make([]skillDoc, len(rev.Skills))
	for i, skill := range rev.Skills {
		skills[i] = skillDoc{ID: skill.ID, Version: skill.Version, Tier: string(skill.Tier), CredentialOperation: string(skill.CredentialOperation),
			SharedRead: skill.SharedRead, RecordFilter: skill.RecordFilter, ToolName: skill.Tool.Name, ToolCapability: skill.Tool.Capability,
			ToolVersion: skill.Tool.Version, ToolClass: string(skill.Tool.Class), ToolDataScope: nonNil(skill.Tool.DataScope), ToolCost: skill.Tool.Cost, ToolSchema: skill.Tool.Schema}
	}
	grants := make([]grantDoc, len(rev.Grants))
	for i, grant := range rev.Grants {
		grants[i] = grantDoc{ID: grant.ID, Roles: nonNil(grant.Roles), Population: grant.Population, OrganizationScopes: nonNil(grant.OrganizationScopes), Skills: nonNil(grant.Skills)}
	}
	skillsJSON, err := json.Marshal(skills)
	if err != nil {
		return fmt.Errorf("%w: encoding skills: %v", agentconnect.ErrInvalid, err)
	}
	grantsJSON, err := json.Marshal(grants)
	if err != nil {
		return fmt.Errorf("%w: encoding grants: %v", agentconnect.ErrInvalid, err)
	}
	var brokered, approval any
	if rev.CredentialMode == agentconnect.Brokered {
		b, err := json.Marshal(rev.BrokeredCredential)
		if err != nil {
			return fmt.Errorf("%w: encoding credential binding: %v", agentconnect.ErrInvalid, err)
		}
		brokered = string(b)
	}
	if rev.Approval != nil {
		a, err := json.Marshal(approvalDoc{RequestedBy: rev.Approval.RequestedBy, ApprovedBy: rev.Approval.ApprovedBy, RequestHash: rev.Approval.RequestHash, ApprovedAt: rev.Approval.ApprovedAt.UTC(), StepUp: rev.Approval.StepUp})
		if err != nil {
			return fmt.Errorf("%w: encoding approval: %v", agentconnect.ErrInvalid, err)
		}
		approval = string(a)
	}
	tx, tenantID, err := s.begin(ctx, rev.TenantID)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	affected, err := tx.Exec(ctx, `INSERT INTO agent_connection_revision
		(tenant_id,connection_id,revision,endpoint,connector_id,connector_version,credential_mode,brokered_credential,skills,grants,approval)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb,$10::jsonb,$11::jsonb)
		ON CONFLICT (tenant_id, connection_id) DO NOTHING`,
		tenantID, rev.ID, int64(rev.Revision), rev.Endpoint, record.ConnectorID, record.ConnectorVersion, string(rev.CredentialMode), brokered, string(skillsJSON), string(grantsJSON), approval)
	if err != nil {
		return fmt.Errorf("agentconnectionstore: insert revision: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: connection revision already exists", agentconnect.ErrInvalid)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("agentconnectionstore: commit revision: %w", err)
	}
	return nil
}

// ListRevisions returns a tenant's revisions ordered by connection id.
func (s *Store) ListRevisions(tenant string) ([]agentconnect.RevisionRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	tx, tenantID, err := s.begin(ctx, tenant)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT connection_id,revision,endpoint,connector_id,connector_version,credential_mode,
		brokered_credential::text,skills::text,grants::text,approval::text,connection_epoch,revoked_at,revoked_by,revoked_reason,revoked_evidence_ref
		FROM agent_connection_revision WHERE tenant_id=$1 ORDER BY connection_id`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("agentconnectionstore: list revisions: %w", err)
	}
	defer rows.Close()
	var out []agentconnect.RevisionRecord
	for rows.Next() {
		var (
			record                                    agentconnect.RevisionRecord
			revision, epoch                           int64
			mode                                      string
			brokered, approval                        *string
			skillsText, grantsText                    string
			revokedAt                                 *time.Time
			revokedBy, revokedReason, revokedEvidence *string
		)
		rev := &record.Revision
		if err := rows.Scan(&rev.ID, &revision, &rev.Endpoint, &record.ConnectorID, &record.ConnectorVersion, &mode,
			&brokered, &skillsText, &grantsText, &approval, &epoch, &revokedAt, &revokedBy, &revokedReason, &revokedEvidence); err != nil {
			return nil, fmt.Errorf("agentconnectionstore: scan revision: %w", err)
		}
		rev.TenantID, rev.Revision, rev.CredentialMode, record.Epoch = tenant, uint64(revision), agentconnect.CredentialMode(mode), uint64(epoch)
		if err := decodeRevision(rev, brokered, skillsText, grantsText, approval); err != nil {
			return nil, err
		}
		if revokedAt != nil {
			record.Revocation = &agentconnect.RevocationRecord{TenantID: tenant, ConnectionID: rev.ID, RevokedAt: revokedAt.UTC(), Epoch: uint64(epoch),
				Actor: deref(revokedBy), Reason: deref(revokedReason), EvidenceRef: deref(revokedEvidence)}
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agentconnectionstore: read revisions: %w", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeRevision(rev *agentconnect.ConnectionRevision, brokered *string, skillsText, grantsText string, approval *string) error {
	var skills []skillDoc
	if err := json.Unmarshal([]byte(skillsText), &skills); err != nil {
		return fmt.Errorf("agentconnectionstore: decode skills: %w", err)
	}
	for _, skill := range skills {
		rev.Skills = append(rev.Skills, agentconnect.SkillExposure{ID: skill.ID, Version: skill.Version, Tier: agentconnect.SideEffectTier(skill.Tier),
			CredentialOperation: custody.Operation(skill.CredentialOperation), SharedRead: skill.SharedRead, RecordFilter: skill.RecordFilter,
			Tool: agentsecurity.ToolDescriptor{Name: skill.ToolName, Capability: skill.ToolCapability, Version: skill.ToolVersion,
				Class: agentsecurity.ToolClass(skill.ToolClass), DataScope: skill.ToolDataScope, Cost: skill.ToolCost, Schema: skill.ToolSchema}})
	}
	var grants []grantDoc
	if err := json.Unmarshal([]byte(grantsText), &grants); err != nil {
		return fmt.Errorf("agentconnectionstore: decode grants: %w", err)
	}
	for _, grant := range grants {
		rev.Grants = append(rev.Grants, agentconnect.GrantScope{ID: grant.ID, Roles: grant.Roles, Population: grant.Population, OrganizationScopes: grant.OrganizationScopes, Skills: grant.Skills})
	}
	if brokered != nil {
		if err := json.Unmarshal([]byte(*brokered), &rev.BrokeredCredential); err != nil {
			return fmt.Errorf("agentconnectionstore: decode credential binding: %w", err)
		}
	}
	if approval != nil {
		var doc approvalDoc
		if err := json.Unmarshal([]byte(*approval), &doc); err != nil {
			return fmt.Errorf("agentconnectionstore: decode approval: %w", err)
		}
		rev.Approval = &agentconnect.SecondAdminApproval{RequestedBy: doc.RequestedBy, ApprovedBy: doc.ApprovedBy, RequestHash: doc.RequestHash, ApprovedAt: doc.ApprovedAt, StepUp: doc.StepUp}
	}
	return nil
}

// PutUserState upserts one user's link state; the epoch only moves forward.
func (s *Store) PutUserState(state agentconnect.UserLinkState) error {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if strings.TrimSpace(state.ConnectionID) == "" || strings.TrimSpace(state.UserID) == "" || state.Epoch == 0 {
		return fmt.Errorf("%w: user state is incomplete", agentconnect.ErrInvalid)
	}
	var externalID, binding, linkedAt any
	linked := state.Link != nil
	if linked {
		b, err := json.Marshal(state.Link.Binding)
		if err != nil {
			return fmt.Errorf("%w: encoding credential binding: %v", agentconnect.ErrInvalid, err)
		}
		externalID, binding, linkedAt = state.Link.ExternalAccountID, string(b), state.Link.LinkedAt.UTC()
	}
	tx, tenantID, err := s.begin(ctx, state.TenantID)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO agent_connection_user_state
		(tenant_id,connection_id,user_id,user_epoch,linked,external_account_id,binding,linked_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8)
		ON CONFLICT (tenant_id,connection_id,user_id) DO UPDATE SET
			user_epoch=GREATEST(agent_connection_user_state.user_epoch, EXCLUDED.user_epoch),
			linked=EXCLUDED.linked, external_account_id=EXCLUDED.external_account_id,
			binding=EXCLUDED.binding, linked_at=EXCLUDED.linked_at, updated_at=now()`,
		tenantID, state.ConnectionID, state.UserID, int64(state.Epoch), linked, externalID, binding, linkedAt); err != nil {
		return fmt.Errorf("agentconnectionstore: put user state: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("agentconnectionstore: commit user state: %w", err)
	}
	return nil
}

// ListUserStates returns a tenant's user link states.
func (s *Store) ListUserStates(tenant string) ([]agentconnect.UserLinkState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	tx, tenantID, err := s.begin(ctx, tenant)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT connection_id,user_id,user_epoch,linked,external_account_id,binding::text,linked_at
		FROM agent_connection_user_state WHERE tenant_id=$1 ORDER BY connection_id,user_id`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("agentconnectionstore: list user states: %w", err)
	}
	defer rows.Close()
	var out []agentconnect.UserLinkState
	for rows.Next() {
		var (
			state    agentconnect.UserLinkState
			epoch    int64
			linked   bool
			external *string
			binding  *string
			linkedAt *time.Time
		)
		if err := rows.Scan(&state.ConnectionID, &state.UserID, &epoch, &linked, &external, &binding, &linkedAt); err != nil {
			return nil, fmt.Errorf("agentconnectionstore: scan user state: %w", err)
		}
		state.TenantID, state.Epoch = tenant, uint64(epoch)
		if linked && external != nil && binding != nil && linkedAt != nil {
			link := agentconnect.AccountLink{UserID: state.UserID, ConnectionID: state.ConnectionID, ExternalAccountID: *external, LinkedAt: linkedAt.UTC()}
			if err := json.Unmarshal([]byte(*binding), &link.Binding); err != nil {
				return nil, fmt.Errorf("agentconnectionstore: decode credential binding: %w", err)
			}
			state.Link = &link
		}
		out = append(out, state)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agentconnectionstore: read user states: %w", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// PutRevocation durably records a connection revocation. The first actor,
// reason and time are kept; the epoch only moves forward.
func (s *Store) PutRevocation(record agentconnect.RevocationRecord) error {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if strings.TrimSpace(record.ConnectionID) == "" || strings.TrimSpace(record.Actor) == "" || strings.TrimSpace(record.Reason) == "" ||
		strings.TrimSpace(record.EvidenceRef) == "" || record.RevokedAt.IsZero() || record.Epoch == 0 {
		return fmt.Errorf("%w: revocation record is incomplete", agentconnect.ErrInvalid)
	}
	tx, tenantID, err := s.begin(ctx, record.TenantID)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	affected, err := tx.Exec(ctx, `UPDATE agent_connection_revision SET
			connection_epoch=GREATEST(connection_epoch,$3),
			revoked_at=COALESCE(revoked_at,$4), revoked_by=COALESCE(revoked_by,$5),
			revoked_reason=COALESCE(revoked_reason,$6), revoked_evidence_ref=COALESCE(revoked_evidence_ref,$7)
		WHERE tenant_id=$1 AND connection_id=$2`,
		tenantID, record.ConnectionID, int64(record.Epoch), record.RevokedAt.UTC(), record.Actor, record.Reason, record.EvidenceRef)
	if err != nil {
		return fmt.Errorf("agentconnectionstore: put revocation: %w", err)
	}
	if affected == 0 {
		return agentconnect.ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("agentconnectionstore: commit revocation: %w", err)
	}
	return nil
}

func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func deref(in *string) string {
	if in == nil {
		return ""
	}
	return *in
}
