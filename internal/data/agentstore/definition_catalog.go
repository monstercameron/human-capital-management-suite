package agentstore

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ListCurrentManifests returns current definitions, including unpublished
// definitions, inside the normal tenant transaction and integrity checks.
func (s *Store) ListCurrentManifests(ctx context.Context, tenantID uuid.UUID) ([]agentmanifest.Manifest, error) {
	manifests := []agentmanifest.Manifest{}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT v.definition_id,v.version,v.schema_version,v.digest,v.manifest
			FROM agent_definition_head h
			JOIN agent_definition_version v ON v.tenant_id=h.tenant_id AND v.definition_id=h.definition_id AND v.version=h.current_version
			WHERE h.tenant_id=$1 ORDER BY h.definition_id`, tenantID)
		if err != nil {
			return fmt.Errorf("agentstore: list current definitions: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id, digest string
			var version int64
			var schema int32
			var raw []byte
			if err := rows.Scan(&id, &version, &schema, &digest, &raw); err != nil {
				return err
			}
			manifest, err := checkedManifest(raw, digest, id, uint64(version), uint32(schema))
			if err != nil {
				return err
			}
			manifests = append(manifests, manifest)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return manifests, nil
}
