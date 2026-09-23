// Index reconciliation for HUB-029: an idempotent document outbox
// consumer that rebuilds derived chunks for live versions and deletes the
// chunks of versions with no live deployment. A per-document watermark
// records the consumed outbox id, so replay is a no-op and late events
// are picked up incrementally. Link checker events are not
// index-bearing and pass through. Grant revocation needs no reindex at
// all: search evaluates grants per query. Vector rebuilds need a provider
// call supplied by the caller; model retirement purges vectors by model.
package documenthubstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ReconcileOptions tunes one reconcile run: an optional provider for
// vector rebuilds with its approved policy and model, and vector models
// to purge outright.
type ReconcileOptions struct {
	Embed        func(string) ([]float32, error)
	Policy       EmbeddingPolicy
	ModelID      string
	RetireModels []string
}

// ReconcileReport proves one consumer run: events seen, live versions
// rebuilt, dead chunks deleted, and the resulting watermark.
type ReconcileReport struct {
	Events           int64
	Rebuilt, Deleted int64
	Watermark        int64
}

// ReconcileDocument consumes the document's outbox events since its
// watermark, rebuilds live versions, deletes dead chunks, purges retired
// models and advances the watermark. The caller needs owner or manager
// authority; unknown documents fail as denied.
func (s *Store) ReconcileDocument(ctx context.Context, tenantID, docID, actorID string, opts ReconcileOptions) (ReconcileReport, error) {
	var out ReconcileReport
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := authorizeOwnerTx(ctx, tx, tenantID, docID, actorID); err != nil {
			return err
		}
		var watermark int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(watermark),0) FROM document_index_watermark WHERE tenant_id=$1 AND document_id=$2`,
			tenantID, docID).Scan(&watermark); err != nil {
			return err
		}
		var maxSeen int64
		var events int64
		rows, err := tx.Query(ctx, `SELECT id FROM document_outbox WHERE tenant_id=$1 AND aggregate_id=$2 AND id>$3 ORDER BY id`,
			tenantID, docID, watermark)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			events++
			maxSeen = id
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		live, err := liveVersionsTx(ctx, tx, tenantID, docID)
		if err != nil {
			return err
		}
		for _, versionID := range live {
			version, err := loadVersion(ctx, tx, tenantID, docID, versionID)
			if err != nil {
				return err
			}
			if err := indexVersionTx(ctx, tx, tenantID, docID, version); err != nil {
				return err
			}
			out.Rebuilt++
			if opts.Embed != nil && opts.ModelID != "" {
				model, err := opts.Policy.resolve(opts.ModelID, version.Classification)
				if err != nil {
					return err
				}
				if _, err := embedSectionsTx(ctx, tx, tenantID, docID, version, model, opts.Embed); err != nil {
					return err
				}
			}
		}
		deleted, err := deleteDeadChunksTx(ctx, tx, tenantID, docID, live)
		if err != nil {
			return err
		}
		out.Deleted = deleted
		for _, model := range opts.RetireModels {
			n, err := tx.Exec(ctx, `DELETE FROM document_section_vector WHERE tenant_id=$1 AND document_id=$2 AND model_id=$3`,
				tenantID, docID, model)
			if err != nil {
				return err
			}
			out.Deleted += n
		}
		out.Events = events
		out.Watermark = watermark
		if events > 0 {
			out.Watermark = maxSeen
		}
		if _, err := tx.Exec(ctx, `INSERT INTO document_index_watermark(tenant_id,document_id,watermark) VALUES($1,$2,$3)
			ON CONFLICT (tenant_id,document_id) DO UPDATE SET watermark=EXCLUDED.watermark, updated_at=now()`,
			tenantID, docID, out.Watermark); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return ReconcileReport{}, err
	}
	return out, nil
}

// liveVersionsTx returns the distinct versions with a live pointer.
func liveVersionsTx(ctx context.Context, tx dbport.Tx, tenantID, docID string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT d.version_id FROM document_active_pointer p
		JOIN document_deployment d ON d.tenant_id=p.tenant_id AND d.id=p.deployment_id
		WHERE p.tenant_id=$1 AND p.document_id=$2 ORDER BY d.version_id`, tenantID, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// indexVersionTx rebuilds every derivative of one loaded version through
// the shared replace-semantics helpers, so reruns converge.
func indexVersionTx(ctx context.Context, tx dbport.Tx, tenantID, docID string, version Version) error {
	if err := indexTermsTx(ctx, tx, tenantID, docID, version); err != nil {
		return err
	}
	if err := storeBlocksTx(ctx, tx, tenantID, docID, version.ID, DeriveBlocks(version.Markdown)); err != nil {
		return err
	}
	return storeLinksTx(ctx, tx, tenantID, docID, version.ID, ExtractLinks(version.Markdown))
}

// deleteDeadChunksTx removes derived rows of versions with no live
// deployment and returns the removed row count.
func deleteDeadChunksTx(ctx context.Context, tx dbport.Tx, tenantID, docID string, live []string) (int64, error) {
	alive := map[string]bool{}
	for _, v := range live {
		alive[v] = true
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT version_id FROM document_search_term WHERE tenant_id=$1 AND document_id=$2`, tenantID, docID)
	if err != nil {
		return 0, err
	}
	var versions []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return 0, err
		}
		versions = append(versions, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	var deleted int64
	sweep := []string{
		`DELETE FROM document_search_term WHERE tenant_id=$1 AND document_id=$2 AND version_id=$3`,
		`DELETE FROM document_block WHERE tenant_id=$1 AND document_id=$2 AND version_id=$3`,
		`DELETE FROM document_link WHERE tenant_id=$1 AND source_document_id=$2 AND source_version_id=$3`,
		`DELETE FROM document_section_vector WHERE tenant_id=$1 AND document_id=$2 AND version_id=$3`,
	}
	for _, v := range versions {
		if alive[v] {
			continue
		}
		for _, q := range sweep {
			n, err := tx.Exec(ctx, q, tenantID, docID, v)
			if err != nil {
				return 0, err
			}
			deleted += n
		}
	}
	return deleted, nil
}
