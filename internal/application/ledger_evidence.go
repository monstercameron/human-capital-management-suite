package application

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/checkpoint"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	dataledger "github.com/monstercameron/human-capital-management-suite/internal/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/admin"
)

// NewLedgerEvidenceExport returns the application-owned evidence export port
// over the same read handle the serve composition uses for its ledger.
func NewLedgerEvidenceExport(q dbport.Querier) admin.LedgerEvidenceExport {
	exporter := dataledger.NewEvidenceExporter()
	return func(ctx context.Context, tenant string, from, to time.Time) (map[string][]byte, error) {
		if q == nil {
			return nil, fmt.Errorf("ledger evidence export: database is not configured")
		}
		tenantID := pgstore.TenantID(tenant)
		var release, digest string
		err := q.QueryRow(ctx, `
			SELECT r.release_version, r.artifact_digest
			FROM migration_journal j
			JOIN schema_release r ON r.release_id = j.release_id
			WHERE j.direction = 'UP' AND j.status = 'APPLIED' AND j.rolled_back_at IS NULL
			ORDER BY j.migration_version DESC
			LIMIT 1`).Scan(&release, &digest)
		if err != nil {
			return nil, fmt.Errorf("ledger evidence export: resolve applied schema release: %w", err)
		}
		versionText, ok := strings.CutPrefix(release, "p1a-")
		if !ok {
			return nil, fmt.Errorf("ledger evidence export: unsupported schema release %q", release)
		}
		versionText, _, ok = strings.Cut(versionText, "-")
		if !ok {
			return nil, fmt.Errorf("ledger evidence export: malformed schema release %q", release)
		}
		version, err := strconv.ParseInt(versionText, 10, 64)
		if err != nil || version <= 0 || digest == "" {
			return nil, fmt.Errorf("ledger evidence export: malformed schema release %q", release)
		}
		pkg, err := exporter.Export(ctx, q, dataledger.EvidenceRequest{
			Tenant: tenantID,
			From:   from,
			To:     to,
			Schema: checkpoint.SchemaRelease{Version: version, Digest: digest},
		})
		if err != nil {
			return nil, fmt.Errorf("ledger evidence export: %w", err)
		}
		return pkg.Files(), nil
	}
}
