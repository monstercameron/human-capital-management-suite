package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/profilephoto"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
)

type demoPeopleReceipt struct {
	Tenant               uuid.UUID `json:"tenant"`
	Company              string    `json:"company"`
	OrganizationUnits    int       `json:"organization_units"`
	OrganizationInserted int       `json:"organization_inserted"`
	OrganizationSkipped  int       `json:"organization_skipped"`
	Planned              int       `json:"planned"`
	Inserted             int       `json:"inserted"`
	Skipped              int       `json:"skipped"`
	Photos               int       `json:"photos"`
	WithoutPhotos        int       `json:"without_photos"`
}

// runDemoPeopleCommand processes every selected source through the same
// bounded original-and-proxy pipeline first, then records the deterministic
// worker population in one tenant-scoped transaction. A receipt is emitted
// only after the database commit succeeds.
func runDemoPeopleCommand(ctx context.Context, db dbport.Beginner, tenant, photoSource, assetDir, originalDir string, out io.Writer) error {
	if tenant == "" || photoSource == "" || assetDir == "" || originalDir == "" {
		return fmt.Errorf("demo people tenant, photo source, asset directory, and original directory are required")
	}
	tenantID := pgstore.TenantID(tenant)
	employees, err := demoworkforce.Plan(tenantID)
	if err != nil {
		return err
	}
	if err := ingestDemoPhotos(ctx, employees, photoSource, assetDir, originalDir); err != nil {
		return err
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin demo workforce transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ensureDemoTenant(ctx, tx, tenantID, tenant); err != nil {
		return err
	}
	organization, err := demoworkforce.SeedOrganization(ctx, tx, tenantID)
	if err != nil {
		return fmt.Errorf("seed HarborCare organization: %w", err)
	}
	summary, err := demoworkforce.Seed(ctx, tx, tenantID)
	if err != nil {
		return fmt.Errorf("seed HarborCare workforce: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit HarborCare workforce: %w", err)
	}
	receipt := demoPeopleReceipt{
		Tenant: tenantID, Company: demoworkforce.HarborCare.Name,
		OrganizationUnits: len(demoworkforce.HarborCare.Units), Planned: summary.Planned,
		OrganizationInserted: organization.UnitsInserted, OrganizationSkipped: organization.UnitsSkipped,
		Inserted: summary.Inserted, Skipped: summary.Skipped, Photos: summary.Photos,
		WithoutPhotos: summary.Planned - summary.Photos,
	}
	if err := json.NewEncoder(out).Encode(receipt); err != nil {
		return fmt.Errorf("write demo people receipt: %w", err)
	}
	return nil
}

func ingestDemoPhotos(ctx context.Context, employees []demoworkforce.Employee, sourceDir, assetDir, originalDir string) error {
	for _, employee := range employees {
		if !employee.HasProfilePhoto {
			continue
		}
		if proxyName := filepath.Base(employee.PhotoProxyRef); proxyName != "" && proxyName != "." {
			switch info, err := os.Stat(filepath.Join(assetDir, proxyName)); {
			case err == nil && !info.IsDir() && info.Size() > 0:
				// Already published by a development seed. The proxy is the
				// authoritative demo fixture; re-deriving it from the
				// source photo and requiring byte-identical JPEG output
				// would make seeding fail whenever the source photo has
				// legitimately drifted by a few pixels (retouch,
				// recompression) without anyone having changed the demo
				// employee roster. The asset store's own publish path
				// still fails closed on a genuine name collision for any
				// proxy that is not already on disk.
				continue
			case err != nil && !os.IsNotExist(err):
				return fmt.Errorf("inspect existing proxy for %s: %w", employee.PhotoSourceName, err)
			}
		}
		sourcePath := filepath.Join(sourceDir, employee.PhotoSourceName)
		file, err := os.Open(sourcePath)
		if err != nil {
			return fmt.Errorf("open generated photo %s: %w", employee.PhotoSourceName, err)
		}
		content, readErr := profilephoto.ReadAllBounded(file)
		closeErr := file.Close()
		if readErr != nil {
			return fmt.Errorf("read generated photo %s: %w", employee.PhotoSourceName, readErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close generated photo %s: %w", employee.PhotoSourceName, closeErr)
		}
		proxyName := filepath.Base(employee.PhotoProxyRef)
		result, err := profilephoto.Upload(ctx, profilephoto.FileStore{Root: assetDir, OriginalRoot: originalDir}, profilephoto.Request{
			WorkerKey: employee.Row.WorkerKey, Content: content, DeclaredMediaType: "image/png",
			OriginalName: employee.PhotoSourceName, ProxyName: proxyName,
		})
		if err != nil {
			return fmt.Errorf("process generated photo %s: %w", employee.PhotoSourceName, err)
		}
		if result.OriginalRef != employee.PhotoOriginalRef || result.ProxyRef != employee.PhotoProxyRef {
			return fmt.Errorf("processed photo %s produced unexpected references %q and %q", employee.PhotoSourceName, result.OriginalRef, result.ProxyRef)
		}
	}
	return nil
}

func ensureDemoTenant(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, tenant string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')
		ON CONFLICT (tenant_id) DO NOTHING`, tenantID, tenant, demoworkforce.HarborCare.Name)
	if err != nil {
		return fmt.Errorf("register HarborCare tenant %q: %w", tenant, err)
	}
	return nil
}
