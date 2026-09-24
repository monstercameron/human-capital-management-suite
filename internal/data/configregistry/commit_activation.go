package configregistry

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	platformconfig "github.com/monstercameron/human-capital-management-suite/internal/platform/configregistry"
)

var _ platformconfig.AtomicActivationStore = (*Store)(nil)

// CommitObjectActivation writes the immutable object and its activation row
// in the same tenant-scoped transaction. Compilation happens before this
// method is called, so failed candidates leave neither row behind.
func (s *Store) CommitObjectActivation(o platformconfig.ConfigurationObject, evidence platformconfig.ActivationEvidence) (platformconfig.ActivationRecord, error) {
	if s == nil || s.db == nil {
		return platformconfig.ActivationRecord{}, fmt.Errorf("configregistry: no database store supplied")
	}
	if err := o.Verify(); err != nil {
		return platformconfig.ActivationRecord{}, err
	}
	if evidence.ActivatedBy == "" {
		return platformconfig.ActivationRecord{}, fmt.Errorf("configregistry: activation has no activating principal")
	}
	if evidence.ActivatedAt.IsZero() {
		return platformconfig.ActivationRecord{}, fmt.Errorf("configregistry: activation has no activation time")
	}
	ctx := context.Background()
	record := platformconfig.ActivationRecord{
		Scope: o.Scope, Kind: o.Kind, ID: o.ID, Revision: o.Revision,
		ActivatedBy: evidence.ActivatedBy, Authority: evidence.Authority,
		Reason: evidence.Reason, ActivatedAt: evidence.ActivatedAt, ObjectDigest: o.Digest(),
	}
	err := s.withTenant(ctx, o.Scope.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO config_object (
			tenant_id, cell_id, kind, object_id, revision, canonical_body_digest, body, record_digest,
			schema_ref, publisher_principal, published_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT DO NOTHING`,
			o.Scope.TenantID, o.Scope.CellID, string(o.Kind), o.ID, int64(o.Revision),
			o.CanonicalBodyDigest, o.Body, o.Digest(), o.SchemaRef, o.PublisherPrincipal, o.PublishedAt)
		if err != nil {
			return err
		}
		var storedDigest string
		if err := tx.QueryRow(ctx, `SELECT record_digest FROM config_object WHERE tenant_id=$1 AND cell_id=$2 AND kind=$3 AND object_id=$4 AND revision=$5`,
			o.Scope.TenantID, o.Scope.CellID, string(o.Kind), o.ID, int64(o.Revision)).Scan(&storedDigest); err != nil {
			return err
		}
		if storedDigest != o.Digest() {
			return fmt.Errorf("configregistry: revision %d is already published with different content", o.Revision)
		}
		var currentMax int64
		maxErr := tx.QueryRow(ctx, `SELECT activation_sequence FROM config_object_activation
			WHERE tenant_id=$1 AND cell_id=$2 AND kind=$3 AND object_id=$4
			ORDER BY activation_sequence DESC LIMIT 1`, o.Scope.TenantID, o.Scope.CellID, string(o.Kind), o.ID).Scan(&currentMax)
		if errors.Is(maxErr, dbport.ErrNoRows) {
			currentMax = 0
		} else if maxErr != nil {
			return maxErr
		}
		_, err = tx.Exec(ctx, `INSERT INTO config_object_activation (
			tenant_id, activation_id, cell_id, kind, object_id, revision, activation_sequence,
			activated_by, authority, reason, activated_at, object_digest
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			o.Scope.TenantID, uuid.New(), o.Scope.CellID, string(o.Kind), o.ID, int64(o.Revision), currentMax+1,
			evidence.ActivatedBy, evidence.Authority, evidence.Reason, evidence.ActivatedAt, o.Digest())
		return err
	})
	if err != nil {
		return platformconfig.ActivationRecord{}, err
	}
	return record, nil
}
