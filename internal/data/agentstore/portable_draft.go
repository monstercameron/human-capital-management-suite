package agentstore

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var ErrPortableDraftInvalid = errors.New("agentstore: invalid portable draft")

type PortableDefinitionDraft struct {
	TenantID     uuid.UUID
	Manifest     agentmanifest.Manifest
	Instructions string
	State        string
	ActorID      string
	CreatedAt    time.Time
}

// SavePortableDefinitionDraft atomically stores verified instruction content,
// the immutable manifest image, its head, and a DRAFT marker.
func (s *Store) SavePortableDefinitionDraft(ctx context.Context, tenantID uuid.UUID, actor string, manifest agentmanifest.Manifest, instructions string) error {
	if tenantID == uuid.Nil || strings.TrimSpace(actor) == "" || actor != manifest.OwnerID || manifest.Version != 1 || len(manifest.ContextGrants) != 0 || strings.TrimSpace(instructions) == "" || len(instructions) > 1<<20 || !utf8.ValidString(instructions) {
		return ErrPortableDraftInvalid
	}
	if err := manifest.Validate(); err != nil || instructionDigest(instructions) != manifest.InstructionsDigest {
		return ErrPortableDraftInvalid
	}
	canonical, err := manifest.CanonicalJSON()
	if err != nil {
		return ErrPortableDraftInvalid
	}
	digest, err := manifest.Digest()
	if err != nil {
		return ErrPortableDraftInvalid
	}
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO agent_instruction_content (tenant_id,digest,content) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, tenantID, manifest.InstructionsDigest, instructions); err != nil {
			return err
		}
		if err := insertManifestVersion(ctx, tx, tenantID, manifest, canonical, digest); err != nil {
			return err
		}
		n, err := tx.Exec(ctx, `INSERT INTO agent_definition_head (tenant_id,definition_id,current_version,revision) VALUES ($1,$2,1,1) ON CONFLICT DO NOTHING`, tenantID, manifest.ID)
		if err != nil || n != 1 {
			if err != nil {
				return err
			}
			return ErrConflict
		}
		n, err = tx.Exec(ctx, `INSERT INTO agent_portable_draft (tenant_id,definition_id,version,state,actor_id,created_at) VALUES ($1,$2,1,'DRAFT',$3,now())`, tenantID, manifest.ID, actor)
		if err != nil || n != 1 {
			if err != nil {
				return err
			}
			return ErrConflict
		}
		return nil
	})
}

func (s *Store) GetPortableDefinitionDraft(ctx context.Context, tenantID uuid.UUID, definitionID string) (PortableDefinitionDraft, error) {
	if tenantID == uuid.Nil || strings.TrimSpace(definitionID) == "" {
		return PortableDefinitionDraft{}, ErrPortableDraftInvalid
	}
	var out PortableDefinitionDraft
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var raw []byte
		var schema, version int32
		var digest, state, actor string
		var created time.Time
		if err := tx.QueryRow(ctx, `SELECT v.schema_version,v.version,v.digest,v.manifest,d.state,d.actor_id,d.created_at FROM agent_portable_draft d JOIN agent_definition_version v ON v.tenant_id=d.tenant_id AND v.definition_id=d.definition_id AND v.version=d.version WHERE d.tenant_id=$1 AND d.definition_id=$2`, tenantID, definitionID).Scan(&schema, &version, &digest, &raw, &state, &actor, &created); err != nil {
			return err
		}
		manifest, err := checkedManifest(raw, digest, definitionID, uint64(version), uint32(schema))
		if err != nil {
			return err
		}
		var instructions string
		if err := tx.QueryRow(ctx, `SELECT content FROM agent_instruction_content WHERE tenant_id=$1 AND digest=$2`, tenantID, manifest.InstructionsDigest).Scan(&instructions); err != nil {
			return err
		}
		if state != "DRAFT" || len(manifest.ContextGrants) != 0 || manifest.OwnerID != actor || instructionDigest(instructions) != manifest.InstructionsDigest {
			return ErrPortableDraftInvalid
		}
		out = PortableDefinitionDraft{TenantID: tenantID, Manifest: manifest, Instructions: instructions, State: state, ActorID: actor, CreatedAt: created}
		return nil
	})
	return out, err
}
