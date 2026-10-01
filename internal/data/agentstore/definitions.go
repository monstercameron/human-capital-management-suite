package agentstore

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var (
	ErrConflict = errors.New("agentstore: definition revision conflict")
	ErrNotFound = errors.New("agentstore: definition version not found")
)

// SaveManifest appends a canonical immutable manifest version and advances its
// tenant-scoped current pointer if expectedRevision still matches. The initial
// pointer is revision 1 and requires expectedRevision 0. Instruction content
// must first be stored with SaveInstructionContent and match the manifest's
// InstructionsDigest exactly.
func (s *Store) SaveManifest(ctx context.Context, tenantID uuid.UUID, manifest agentmanifest.Manifest, expectedRevision uint64) (uint64, error) {
	if tenantID == uuid.Nil || expectedRevision >= math.MaxInt64 || manifest.Version > math.MaxInt64 {
		return 0, fmt.Errorf("%w: invalid tenant or revision", ErrInvalidConfig)
	}
	canonical, err := manifest.CanonicalJSON()
	if err != nil {
		return 0, fmt.Errorf("agentstore: validate manifest: %w", err)
	}
	digest, err := manifest.Digest()
	if err != nil {
		return 0, fmt.Errorf("agentstore: digest manifest: %w", err)
	}
	var nextRevision uint64
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var instructions string
		if err := tx.QueryRow(ctx, `SELECT content FROM agent_instruction_content
			WHERE tenant_id=$1 AND digest=$2`, tenantID, manifest.InstructionsDigest).Scan(&instructions); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return ErrInstructionContentNotFound
			}
			return fmt.Errorf("agentstore: verify manifest instruction content: %w", err)
		}
		if instructionDigest(instructions) != manifest.InstructionsDigest {
			return ErrInstructionContentNotFound
		}
		var currentVersion, revision int64
		err := tx.QueryRow(ctx, `SELECT current_version,revision FROM agent_definition_head
			WHERE tenant_id=$1 AND definition_id=$2 FOR UPDATE`, tenantID, manifest.ID).Scan(&currentVersion, &revision)
		switch {
		case errors.Is(err, dbport.ErrNoRows):
			if expectedRevision != 0 {
				return ErrConflict
			}
			if err := insertManifestVersion(ctx, tx, tenantID, manifest, canonical, digest); err != nil {
				return err
			}
			n, err := tx.Exec(ctx, `INSERT INTO agent_definition_head
				(tenant_id,definition_id,current_version,revision)
				VALUES ($1,$2,$3,1) ON CONFLICT DO NOTHING`, tenantID, manifest.ID, int64(manifest.Version))
			if err != nil {
				return fmt.Errorf("agentstore: create manifest head: %w", err)
			}
			if n != 1 {
				return ErrConflict
			}
			nextRevision = 1
			return nil
		case err != nil:
			return fmt.Errorf("agentstore: read manifest head: %w", err)
		}
		if uint64(revision) != expectedRevision || manifest.Version <= uint64(currentVersion) {
			return ErrConflict
		}
		if err := insertManifestVersion(ctx, tx, tenantID, manifest, canonical, digest); err != nil {
			return err
		}
		n, err := tx.Exec(ctx, `UPDATE agent_definition_head SET current_version=$1,revision=revision+1,updated_at=now()
			WHERE tenant_id=$2 AND definition_id=$3 AND revision=$4`, int64(manifest.Version), tenantID, manifest.ID, int64(expectedRevision))
		if err != nil {
			return fmt.Errorf("agentstore: advance manifest head: %w", err)
		}
		if n != 1 {
			return ErrConflict
		}
		nextRevision = expectedRevision + 1
		return nil
	})
	if err != nil {
		return 0, err
	}
	return nextRevision, nil
}

func insertManifestVersion(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, manifest agentmanifest.Manifest, canonical []byte, digest string) error {
	n, err := tx.Exec(ctx, `INSERT INTO agent_definition_version
		(tenant_id,definition_id,version,schema_version,digest,manifest)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb) ON CONFLICT DO NOTHING`,
		tenantID, manifest.ID, int64(manifest.Version), int32(manifest.SchemaVersion), digest, string(canonical))
	if err != nil {
		return fmt.Errorf("agentstore: append manifest version: %w", err)
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}

// CurrentManifest returns the immutable image referenced by the current
// tenant-scoped head and its optimistic concurrency revision.
func (s *Store) CurrentManifest(ctx context.Context, tenantID uuid.UUID, definitionID string) (agentmanifest.Manifest, uint64, error) {
	var manifest agentmanifest.Manifest
	var revision uint64
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var version, rev int64
		var schemaVersion int32
		var digest string
		var raw []byte
		err := tx.QueryRow(ctx, `SELECT v.version,v.schema_version,v.digest,v.manifest,h.revision
			FROM agent_definition_head h
			JOIN agent_definition_version v ON v.tenant_id=h.tenant_id AND v.definition_id=h.definition_id AND v.version=h.current_version
			WHERE h.tenant_id=$1 AND h.definition_id=$2`, tenantID, definitionID).
			Scan(&version, &schemaVersion, &digest, &raw, &rev)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("agentstore: read current manifest: %w", err)
		}
		manifest, err = checkedManifest(raw, digest, definitionID, uint64(version), uint32(schemaVersion))
		if err != nil {
			return err
		}
		revision = uint64(rev)
		return nil
	})
	if err != nil {
		return agentmanifest.Manifest{}, 0, err
	}
	return manifest, revision, nil
}

// ManifestVersion returns one immutable version by tenant, definition and
// version. A missing image is reported as ErrNotFound.
func (s *Store) ManifestVersion(ctx context.Context, tenantID uuid.UUID, definitionID string, version uint64) (agentmanifest.Manifest, error) {
	if tenantID == uuid.Nil || version == 0 || version > math.MaxInt64 {
		return agentmanifest.Manifest{}, fmt.Errorf("%w: invalid tenant or version", ErrInvalidConfig)
	}
	var manifest agentmanifest.Manifest
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var schemaVersion int32
		var digest string
		var raw []byte
		err := tx.QueryRow(ctx, `SELECT schema_version,digest,manifest FROM agent_definition_version
			WHERE tenant_id=$1 AND definition_id=$2 AND version=$3`, tenantID, definitionID, int64(version)).
			Scan(&schemaVersion, &digest, &raw)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("agentstore: read manifest version: %w", err)
		}
		manifest, err = checkedManifest(raw, digest, definitionID, version, uint32(schemaVersion))
		return err
	})
	if err != nil {
		return agentmanifest.Manifest{}, err
	}
	return manifest, nil
}

func checkedManifest(raw []byte, digest, definitionID string, version uint64, schemaVersion uint32) (agentmanifest.Manifest, error) {
	manifest, err := agentmanifest.Parse(raw)
	if err != nil {
		return agentmanifest.Manifest{}, fmt.Errorf("agentstore: stored manifest is invalid: %w", err)
	}
	ref := agentmanifest.ManifestRef{ID: definitionID, Version: version, SchemaVersion: schemaVersion, Digest: digest}
	if err := agentmanifest.Compatible(ref, manifest); err != nil {
		return agentmanifest.Manifest{}, fmt.Errorf("agentstore: stored manifest reference mismatch: %w", err)
	}
	return manifest, nil
}
