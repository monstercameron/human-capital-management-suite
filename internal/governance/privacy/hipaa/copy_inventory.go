package hipaa

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/privacymeta"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// CertifiedCopyInventory is sealed evidence produced only by the tenant
// inventory adapter. Its zero value is invalid and cannot be populated by
// packages outside this package.
type CertifiedCopyInventory struct{ inventory CopyInventory }

// Snapshot returns a detached view for diagnostics and review.
func (c CertifiedCopyInventory) Snapshot() CopyInventory {
	view := c.inventory
	view.Processors = append([]CopyProcessor(nil), c.inventory.Processors...)
	for i := range view.Processors {
		view.Processors[i].Fields = append([]string(nil), c.inventory.Processors[i].Fields...)
	}
	return view
}

// CertifyTenantCopyInventory adapts RECORDS-COPY-001 evidence for HIPAA flow
// approval. It owns a tenant-scoped REPEATABLE READ, read-only transaction so
// certificate rows and adapted field scopes necessarily come from one snapshot.
func CertifyTenantCopyInventory(ctx context.Context, db dbport.Beginner, tenantID, inventoryID uuid.UUID, at time.Time, maxAge time.Duration) (CertifiedCopyInventory, error) {
	if db == nil || tenantID == uuid.Nil || inventoryID == uuid.Nil {
		return CertifiedCopyInventory{}, fmt.Errorf("%w: tenant and inventory identifiers are required", ErrCopyInventoryMismatch)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return CertifiedCopyInventory{}, fmt.Errorf("%w: begin snapshot: %v", ErrCopyInventoryMismatch, err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY"); err != nil {
		return CertifiedCopyInventory{}, fmt.Errorf("%w: establish repeatable read snapshot: %v", ErrCopyInventoryMismatch, err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return CertifiedCopyInventory{}, fmt.Errorf("%w: scope tenant snapshot: %v", ErrCopyInventoryMismatch, err)
	}
	header, err := privacymeta.LoadDataCopyInventory(ctx, tx, tenantID, inventoryID)
	if err != nil {
		return CertifiedCopyInventory{}, fmt.Errorf("%w: load tenant inventory: %v", ErrCopyInventoryMismatch, err)
	}
	certificate, err := privacymeta.CertifyCopyInventory(ctx, tx, tenantID, inventoryID, at, maxAge)
	if err != nil {
		return CertifiedCopyInventory{}, fmt.Errorf("%w: certify tenant inventory: %v", ErrCopyInventoryMismatch, err)
	}
	if header.TenantID != tenantID || header.InventoryID != inventoryID || certificate.TenantID != tenantID || certificate.InventoryID != inventoryID {
		return CertifiedCopyInventory{}, fmt.Errorf("%w: certifier returned evidence for another tenant or inventory", ErrCopyInventoryMismatch)
	}
	if header.Completeness != "COMPLETE" || header.UnknownCount != 0 {
		return CertifiedCopyInventory{}, fmt.Errorf("%w: inventory census is incomplete", ErrCopyInventoryMismatch)
	}
	rows, err := tx.Query(ctx, `SELECT copy_id, processor_ref, data_category, field_scope FROM data_copy WHERE tenant_id=$1 AND inventory_id=$2 ORDER BY copy_id`, tenantID, inventoryID)
	if err != nil {
		return CertifiedCopyInventory{}, fmt.Errorf("%w: read certified copy scopes: %v", ErrCopyInventoryMismatch, err)
	}
	defer rows.Close()
	processors := make([]CopyProcessor, 0, certificate.CopyCount)
	for rows.Next() {
		var copyID uuid.UUID
		var processor, category string
		var raw []byte
		if err := rows.Scan(&copyID, &processor, &category, &raw); err != nil {
			return CertifiedCopyInventory{}, fmt.Errorf("%w: scan certified copy scope: %v", ErrCopyInventoryMismatch, err)
		}
		var scope struct {
			Fields []string `json:"fields"`
		}
		if err := json.Unmarshal(raw, &scope); err != nil || len(scope.Fields) == 0 {
			return CertifiedCopyInventory{}, fmt.Errorf("%w: copy %s has invalid field scope", ErrCopyInventoryMismatch, copyID)
		}
		processors = append(processors, CopyProcessor{CopyID: copyID.String(), Subprocessor: processor, DataCategory: category, Fields: scope.Fields})
	}
	if err := rows.Err(); err != nil {
		return CertifiedCopyInventory{}, fmt.Errorf("%w: iterate certified copy scopes: %v", ErrCopyInventoryMismatch, err)
	}
	if len(processors) != certificate.CopyCount {
		return CertifiedCopyInventory{}, fmt.Errorf("%w: certified copy count changed during snapshot", ErrCopyInventoryMismatch)
	}
	result := CertifiedCopyInventory{inventory: CopyInventory{
		TenantID: tenantID.String(), Version: inventoryID.String(), Digest: certificate.Digest,
		Watermark: header.SourceWatermark, AsOf: header.AsOf, CertifiedAt: certificate.IssuedAt,
		CertifiedCopyCount: certificate.CopyCount, Complete: true, Processors: processors,
	}}
	if err := tx.Commit(ctx); err != nil {
		return CertifiedCopyInventory{}, fmt.Errorf("%w: finish snapshot: %v", ErrCopyInventoryMismatch, err)
	}
	return result, nil
}

// ApproveCertifiedMedicalFlow additionally binds the approval request to the
// tenant whose copy inventory was certified.
func ApproveCertifiedMedicalFlow(program HIPAABusinessAssociateProgram, expectedTenant string, certified CertifiedCopyInventory, subprocessor string, requestedFields []string, at time.Time, maxInventoryAge time.Duration) (FlowApproval, error) {
	inventory := certified.inventory
	if strings.TrimSpace(expectedTenant) == "" || inventory.TenantID != expectedTenant {
		return FlowApproval{}, fmt.Errorf("%w: copy inventory belongs to a different tenant", ErrCopyInventoryMismatch)
	}
	return approveMedicalFlow(program, inventory, subprocessor, requestedFields, at, maxInventoryAge)
}
