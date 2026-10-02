// Package agentconsolestore keeps the administrator console's connection
// revisions and its audit trail in the agent database (migration 00047). A
// revision that has been live is immutable there: the database refuses a change
// to its content, and no row is ever deleted. It implements
// agentaccess.RevisionStore.
package agentconsolestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/agentconnect"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ErrInvalid is returned for a request that names no tenant or an unknown one.
var ErrInvalid = errors.New("agentconsolestore: invalid request")

const callTimeout = 15 * time.Second

// Runner opens a tenant-bound transaction. *agentstore.Store satisfies it.
type Runner interface {
	RunTenantTx(ctx context.Context, tenantID uuid.UUID, fn func(dbport.Tx) error) error
}

// Store implements agentaccess.RevisionStore.
type Store struct {
	db         Runner
	tenantUUID func(string) uuid.UUID
}

var _ agentaccess.RevisionStore = (*Store)(nil)

// New builds a Store. tenantUUID maps a tenant key to its canonical UUID and
// must return uuid.Nil for an unknown tenant.
func New(db Runner, tenantUUID func(string) uuid.UUID) (*Store, error) {
	if db == nil || tenantUUID == nil {
		return nil, ErrInvalid
	}
	return &Store{db: db, tenantUUID: tenantUUID}, nil
}

type skillDoc struct {
	ID           string `json:"id"`
	Tier         string `json:"tier"`
	Capability   string `json:"capability"`
	SharedRead   bool   `json:"shared_read"`
	RecordFilter string `json:"record_filter"`
}

type grantDoc struct {
	ID                 string   `json:"id"`
	Roles              []string `json:"roles"`
	Population         string   `json:"population"`
	OrganizationScopes []string `json:"organization_scopes"`
	Skills             []string `json:"skills"`
}

type bodyDoc struct {
	Provider       string     `json:"provider"`
	CredentialMode string     `json:"credential_mode"`
	MCPSnapshotID  string     `json:"mcp_snapshot_id"`
	Skills         []skillDoc `json:"skills"`
	Grants         []grantDoc `json:"grants"`
}

func (s *Store) run(tenant string, fn func(ctx context.Context, tx dbport.Tx, id uuid.UUID) error) error {
	if s == nil || tenant == "" || tenant != strings.TrimSpace(tenant) {
		return ErrInvalid
	}
	id := s.tenantUUID(tenant)
	if id == uuid.Nil {
		return fmt.Errorf("%w: unknown tenant", ErrInvalid)
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return s.db.RunTenantTx(ctx, id, func(tx dbport.Tx) error { return fn(ctx, tx, id) })
}

func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func encode(revision agentaccess.Revision) (string, error) {
	doc := bodyDoc{Provider: revision.Provider, CredentialMode: string(revision.CredentialMode), MCPSnapshotID: revision.MCPSnapshotID, Skills: []skillDoc{}, Grants: []grantDoc{}}
	for _, skill := range revision.Skills {
		doc.Skills = append(doc.Skills, skillDoc{ID: skill.ID, Tier: string(skill.Tier), Capability: skill.Capability, SharedRead: skill.SharedRead, RecordFilter: skill.RecordFilter})
	}
	for _, grant := range revision.Grants {
		doc.Grants = append(doc.Grants, grantDoc{ID: grant.ID, Roles: nonNil(grant.Roles), Population: grant.Population, OrganizationScopes: nonNil(grant.OrganizationScopes), Skills: nonNil(grant.Skills)})
	}
	raw, err := json.Marshal(doc)
	return string(raw), err
}

// SaveRevisions upserts the revisions and appends the audit event in one
// transaction.
func (s *Store) SaveRevisions(tenant string, at time.Time, event agentaccess.AuditEvent, revisions []agentaccess.Revision) error {
	if strings.TrimSpace(event.Actor) == "" || strings.TrimSpace(event.Action) == "" || event.Tenant != tenant {
		return ErrInvalid
	}
	return s.run(tenant, func(ctx context.Context, tx dbport.Tx, id uuid.UUID) error {
		for _, revision := range revisions {
			if revision.TenantID != tenant || strings.TrimSpace(revision.ConnectionID) == "" || revision.Number == 0 {
				return ErrInvalid
			}
			body, err := encode(revision)
			if err != nil {
				return err
			}
			var approvedAt, publishedAt any
			if !revision.ApprovedAt.IsZero() {
				approvedAt = revision.ApprovedAt.UTC()
			}
			if !revision.PublishedAt.IsZero() {
				publishedAt = revision.PublishedAt.UTC()
			}
			if _, err := tx.Exec(ctx, `INSERT INTO agent_connection_drafts
				(tenant_id,connection_id,number,body,status,created_by,requested_by,approved_by,approved_at,step_up,digest,published_at,created_at,updated_at)
				VALUES ($1,$2,$3,$4::jsonb,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13)
				ON CONFLICT (tenant_id,connection_id,number) DO UPDATE SET
					body=EXCLUDED.body,status=EXCLUDED.status,requested_by=EXCLUDED.requested_by,approved_by=EXCLUDED.approved_by,
					approved_at=EXCLUDED.approved_at,step_up=EXCLUDED.step_up,digest=EXCLUDED.digest,published_at=EXCLUDED.published_at,updated_at=EXCLUDED.updated_at`,
				id, revision.ConnectionID, int64(revision.Number), body, revision.Status, revision.CreatedBy, revision.RequestedBy, revision.ApprovedBy,
				approvedAt, revision.StepUp, revision.Digest, publishedAt, at.UTC()); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `INSERT INTO agent_connection_console_audit (tenant_id,actor,action,revision,at) VALUES ($1,$2,$3,$4,$5)`,
			id, event.Actor, event.Action, event.Revision, event.At.UTC())
		return err
	})
}

// LoadRevisions returns every revision of the tenant.
func (s *Store) LoadRevisions(tenant string) ([]agentaccess.Revision, error) {
	var out []agentaccess.Revision
	err := s.run(tenant, func(ctx context.Context, tx dbport.Tx, id uuid.UUID) error {
		rows, err := tx.Query(ctx, `SELECT connection_id,number,body::text,status,created_by,requested_by,approved_by,approved_at,step_up,digest,published_at
			FROM agent_connection_drafts WHERE tenant_id=$1 ORDER BY connection_id,number`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				revision                = agentaccess.Revision{TenantID: tenant}
				number                  int64
				body                    string
				approvedAt, publishedAt *time.Time
			)
			if err := rows.Scan(&revision.ConnectionID, &number, &body, &revision.Status, &revision.CreatedBy, &revision.RequestedBy, &revision.ApprovedBy, &approvedAt, &revision.StepUp, &revision.Digest, &publishedAt); err != nil {
				return err
			}
			var doc bodyDoc
			if err := json.Unmarshal([]byte(body), &doc); err != nil {
				return fmt.Errorf("agentconsolestore: decode revision: %w", err)
			}
			revision.Number = uint64(number)
			revision.Provider, revision.CredentialMode, revision.MCPSnapshotID = doc.Provider, agentconnect.CredentialMode(doc.CredentialMode), doc.MCPSnapshotID
			for _, skill := range doc.Skills {
				revision.Skills = append(revision.Skills, agentaccess.Skill{ID: skill.ID, Tier: agentconnect.SideEffectTier(skill.Tier), Capability: skill.Capability, SharedRead: skill.SharedRead, RecordFilter: skill.RecordFilter})
			}
			for _, grant := range doc.Grants {
				revision.Grants = append(revision.Grants, agentconnect.GrantScope{ID: grant.ID, Roles: grant.Roles, Population: grant.Population, OrganizationScopes: grant.OrganizationScopes, Skills: grant.Skills})
			}
			if approvedAt != nil {
				revision.ApprovedAt = approvedAt.UTC()
			}
			if publishedAt != nil {
				revision.PublishedAt = publishedAt.UTC()
			}
			out = append(out, revision)
		}
		return rows.Err()
	})
	return out, err
}

// Audit returns the tenant's console actions, oldest first.
func (s *Store) Audit(tenant string) ([]agentaccess.AuditEvent, error) {
	var out []agentaccess.AuditEvent
	err := s.run(tenant, func(ctx context.Context, tx dbport.Tx, id uuid.UUID) error {
		rows, err := tx.Query(ctx, `SELECT actor,action,revision,at FROM agent_connection_console_audit WHERE tenant_id=$1 ORDER BY id`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			event := agentaccess.AuditEvent{Tenant: tenant}
			if err := rows.Scan(&event.Actor, &event.Action, &event.Revision, &event.At); err != nil {
				return err
			}
			event.At = event.At.UTC()
			out = append(out, event)
		}
		return rows.Err()
	})
	return out, err
}
