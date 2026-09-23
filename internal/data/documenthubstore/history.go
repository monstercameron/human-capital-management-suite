// Policy-limited version history for HUB-017: reading history needs the
// live history capability, and each version is judged against the
// document's current classification. Versions more sensitive than current
// policy come back as redacted shells carrying only their id and
// classification, so history never leaks an older title or byte.
package documenthubstore

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// classificationRank orders labels from least to most sensitive. Unknown
// labels rank above every known one, failing closed on both sides of the
// comparison.
func classificationRank(classification string) int {
	switch strings.ToUpper(strings.TrimSpace(classification)) {
	case "PUBLIC":
		return 0
	case "INTERNAL":
		return 1
	case "CONFIDENTIAL":
		return 2
	case "RESTRICTED":
		return 3
	default:
		return 4
	}
}

// HistoryEntry is one version in a history listing. Redacted entries
// carry no title, hash or timestamp-adjacent bytes: only the version id
// and its classification.
type HistoryEntry struct {
	VersionID, Classification, Title, Hash string
	CreatedAt                              time.Time
	Redacted                               bool
}

// ReadHistory lists a document's versions oldest first for a reader
// holding the current history capability. The capability check runs
// first, so unauthorized readers learn nothing, and per-version
// redaction applies the document's current classification after.
func (s *Store) ReadHistory(ctx context.Context, tenantID, docID, readerKind, readerID string) ([]HistoryEntry, error) {
	var out []HistoryEntry
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var exists int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document WHERE tenant_id=$1 AND id=$2`,
			tenantID, docID).Scan(&exists); err != nil || exists == 0 {
			return ErrDenied
		}
		if err := authorizeTx(ctx, tx, tenantID, docID, readerKind, readerID, ActionHistory); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id,classification,title,content_hash,created_at FROM document_version WHERE tenant_id=$1 AND document_id=$2 ORDER BY created_at, id`,
			tenantID, docID)
		if err != nil {
			return err
		}
		type row struct {
			id, classification, title, hash string
			at                              time.Time
		}
		var versions []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.classification, &r.title, &r.hash, &r.at); err != nil {
				rows.Close()
				return err
			}
			versions = append(versions, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		current := ""
		for _, r := range versions {
			current = r.classification
		}
		bar := classificationRank(current)
		for _, r := range versions {
			if classificationRank(r.classification) > bar {
				out = append(out, HistoryEntry{VersionID: r.id, Classification: r.classification, Redacted: true})
				continue
			}
			out = append(out, HistoryEntry{VersionID: r.id, Classification: r.classification, Title: r.title, Hash: r.hash, CreatedAt: r.at})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// historyManifest renders history entries as canonical evidence rows for
// golden pins.
func historyManifest(entries []HistoryEntry) []map[string]any {
	rows := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, map[string]any{
			"classification": e.Classification, "hash": e.Hash, "redacted": e.Redacted,
			"title": e.Title, "version": e.VersionID,
		})
	}
	return rows
}
