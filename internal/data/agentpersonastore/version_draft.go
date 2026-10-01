package agentpersonastore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CreateVersionDraft atomically appends the next immutable persona version and
// its initial DRAFT lifecycle event. The actor must be a persisted owner, and
// the version number must immediately follow the latest version.
func (s *TenantStore) CreateVersionDraft(ctx context.Context, version PersonaVersion, actor string, at time.Time) error {
	if err := validateVersion(version, s.tenant); err != nil {
		return err
	}
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(actor) != actor || at.IsZero() {
		return fmt.Errorf("%w: actor and creation timestamp are required", ErrInvalid)
	}
	profile, err := json.Marshal(version.Profile)
	if err != nil {
		return fmt.Errorf("%w: profile: %v", ErrInvalid, err)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var lockTaken bool
	if err := tx.QueryRow(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || ':' || $2 || ':' || $3::text, 0)) IS NULL`,
		string(s.tenant), version.PersonaID, fmt.Sprintf("%d", version.Version)).Scan(&lockTaken); err != nil {
		return fmt.Errorf("agentpersonastore: lock version draft: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT owner_role,principal_id FROM persona_owners
		WHERE tenant_id=$1 AND persona_id=$2 ORDER BY owner_role FOR UPDATE`, s.tenantID, version.PersonaID)
	if err != nil {
		return fmt.Errorf("agentpersonastore: lock persona owners: %w", err)
	}
	var businessOwner string
	actorIsOwner := false
	for rows.Next() {
		var role OwnerRole
		var principal string
		if err := rows.Scan(&role, &principal); err != nil {
			rows.Close()
			return fmt.Errorf("agentpersonastore: scan persona owner: %w", err)
		}
		if role == BusinessOwner {
			businessOwner = principal
		}
		if principal == actor && (role == BusinessOwner || role == TechnicalSteward) {
			actorIsOwner = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("agentpersonastore: read persona owners: %w", err)
	}
	rows.Close()
	if !actorIsOwner || businessOwner == "" {
		return fmt.Errorf("%w: current persona owner is required", ErrNotFound)
	}
	var profileOwner struct {
		Owner string `json:"owner"`
	}
	if err := json.Unmarshal(version.Profile, &profileOwner); err != nil || profileOwner.Owner != businessOwner {
		return fmt.Errorf("%w: profile owner must match the persisted business owner", ErrInvalid)
	}
	var latest int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version),0) FROM persona_versions WHERE tenant_id=$1 AND persona_id=$2`, s.tenantID, version.PersonaID).Scan(&latest); err != nil {
		return fmt.Errorf("agentpersonastore: read latest persona version: %w", err)
	}
	if version.Version != latest+1 {
		return fmt.Errorf("%w: version %d must follow current version %d", ErrConflict, version.Version, latest)
	}
	createdAt := at.UTC()
	n, err := tx.Exec(ctx, `INSERT INTO persona_versions
		(tenant_id,persona_id,version,agent_version,handle,display_name,profile,content_digest,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9) ON CONFLICT DO NOTHING`,
		s.tenantID, version.PersonaID, version.Version, version.AgentVersion, version.Handle,
		version.DisplayName, string(profile), version.ContentDigest, createdAt)
	if err != nil {
		return fmt.Errorf("agentpersonastore: create version draft: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: persona version already exists", ErrConflict)
	}
	n, err = tx.Exec(ctx, `INSERT INTO persona_lifecycle_events
		(tenant_id,event_id,persona_id,persona_version,from_state,to_state,reason,actor_id,occurred_at)
		VALUES ($1,$2,$3,$4,'',$5,$6,$7,$8) ON CONFLICT DO NOTHING`,
		s.tenantID, uuid.NewString(), version.PersonaID, version.Version, StateDraft,
		"Persona version draft created", actor, createdAt)
	if err != nil {
		return fmt.Errorf("agentpersonastore: create version draft lifecycle: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: initial lifecycle event already exists", ErrConflict)
	}
	return commit(ctx, tx)
}
