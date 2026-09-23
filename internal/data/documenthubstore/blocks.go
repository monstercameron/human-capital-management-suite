package documenthubstore

import (
	"context"
	"strings"
	"unicode"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// BlockAnchor is a stable, heading-derived address for a content block.
// IDs derive from heading text alone, so body edits and redeploys never
// move an anchor; only an explicit heading edit creates or retires one.
type BlockAnchor struct {
	ID      string
	Heading string
	Level   int
	Ordinal int
}

// BlockHit is one version carrying the given anchor.
type BlockHit struct {
	VersionID string
	Heading   string
	Level     int
}

// DeriveBlocks extracts heading-derived block anchors from markdown. IDs
// come from SplitSections, so anchors and section vectors always agree;
// body edits never move an anchor and only a heading edit retires one.
func DeriveBlocks(markdown string) []BlockAnchor {
	sections := SplitSections(markdown)
	out := make([]BlockAnchor, 0, len(sections))
	for i, sec := range sections {
		out = append(out, BlockAnchor{ID: sec.BlockID, Heading: sec.Heading, Level: sec.Level, Ordinal: i})
	}
	return out
}

// blockManifest renders anchors as canonical evidence rows for golden pins.
func blockManifest(blocks []BlockAnchor) []map[string]any {
	rows := make([]map[string]any, 0, len(blocks))
	for _, b := range blocks {
		rows = append(rows, map[string]any{
			"id": b.ID, "heading": b.Heading, "level": b.Level, "ordinal": b.Ordinal,
		})
	}
	return rows
}

func slugifyHeading(text string) string {
	var b strings.Builder
	prevHyphen := true
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevHyphen = false
			continue
		}
		if !prevHyphen {
			b.WriteByte('-')
			prevHyphen = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "section"
	}
	return slug
}

// StoreBlocks records the derived anchors for one version, replacing any
// prior rows for that version so re-extraction stays idempotent.
func (s *Store) StoreBlocks(ctx context.Context, tenantID, docID, versionID string, blocks []BlockAnchor) error {
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return storeBlocksTx(ctx, tx, tenantID, docID, versionID, blocks)
	})
}

// storeBlocksTx rebuilds the block rows of one version with replace
// semantics, shared by direct extraction and the reconciler.
func storeBlocksTx(ctx context.Context, tx dbport.Tx, tenantID, docID, versionID string, blocks []BlockAnchor) error {
	if _, err := tx.Exec(ctx,
		`DELETE FROM document_block WHERE tenant_id=$1 AND version_id=$2`, tenantID, versionID); err != nil {
		return err
	}
	for _, b := range blocks {
		if _, err := tx.Exec(ctx,
			`INSERT INTO document_block(tenant_id, document_id, version_id, block_id, heading, level, ordinal)
			 VALUES($1,$2,$3,$4,$5,$6,$7)`,
			tenantID, docID, versionID, b.ID, b.Heading, b.Level, b.Ordinal); err != nil {
			return err
		}
	}
	return nil
}

// ResolveBlock returns every version of the document carrying the anchor,
// oldest first. An anchor that survives a redeploy appears once per
// version; an anchor created by a heading rename appears only from the
// renaming version on.
func (s *Store) ResolveBlock(ctx context.Context, tenantID, docID, blockID string) ([]BlockHit, error) {
	var out []BlockHit
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT b.version_id, b.heading, b.level
			   FROM document_block b
			   JOIN document_version v ON v.tenant_id=b.tenant_id AND v.id=b.version_id
			  WHERE b.tenant_id=$1 AND b.document_id=$2 AND b.block_id=$3
			  ORDER BY v.created_at, b.version_id`,
			tenantID, docID, blockID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var h BlockHit
			if err := rows.Scan(&h.VersionID, &h.Heading, &h.Level); err != nil {
				return err
			}
			out = append(out, h)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
