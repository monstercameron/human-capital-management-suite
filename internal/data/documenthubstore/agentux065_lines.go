package documenthubstore

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/semver"
)

// LineLabels names what one reference reads and what its document latest
// publishes (AGENTUX-065).
type LineLabels struct {
	Reading string
	Latest  string
}

// ResolveLineLabels resolves what a reference reads under its own mode and
// the document's newest published label. It reports false when the reader
// may not read the document or the line has no published label.
func (s *Store) ResolveLineLabels(ctx context.Context, tenantID, docID, readerKind, readerID string, ref agentdocref.Reference) (LineLabels, bool, error) {
	var out LineLabels
	var found bool
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := authorizeTx(ctx, tx, tenantID, docID, readerKind, readerID, ActionRead); err != nil {
			return nil
		}
		reading, latest, ok, err := resolveLineLabelsTx(ctx, tx, tenantID, ref)
		if err != nil {
			return err
		}
		out, found = LineLabels{Reading: reading, Latest: latest}, ok
		return nil
	})
	if err != nil {
		return LineLabels{}, false, err
	}
	return out, found, nil
}

func resolveLineLabelsTx(ctx context.Context, tx dbport.Tx, tenantID string, ref agentdocref.Reference) (reading, latest string, ok bool, err error) {
	rows, err := tx.Query(ctx, `SELECT semantic_version FROM document_version WHERE tenant_id=$1 AND document_id=$2 AND status<>'retired' AND semantic_version<>'' AND EXISTS (SELECT 1 FROM document_deployment d WHERE d.tenant_id=$1 AND d.document_id=$2 AND d.version_id=document_version.id)`, tenantID, ref.DocumentID)
	if err != nil {
		return "", "", false, err
	}
	var labels []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return "", "", false, err
		}
		labels = append(labels, raw)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", "", false, err
	}
	var newest *semver.Version
	for _, raw := range labels {
		parsed, err := semver.Parse(raw)
		if err != nil {
			continue
		}
		if newest == nil || newest.Less(parsed) {
			newest = &parsed
		}
	}
	if newest == nil {
		return "", "", false, nil
	}
	switch ref.VersionMode {
	case agentdocref.ModeLatestPublished:
		return newest.String(), newest.String(), true, nil
	case agentdocref.ModePinnedLabel:
		pinned, err := semver.Parse(ref.PinnedLabel)
		if err != nil {
			return "", "", false, nil
		}
		for _, raw := range labels {
			if raw == pinned.String() {
				return pinned.String(), newest.String(), true, nil
			}
		}
		return "", "", false, nil
	case agentdocref.ModeCompatible:
		floor, err := semver.Parse(ref.CompatibleBase)
		if err != nil {
			return "", "", false, nil
		}
		var best *semver.Version
		for _, raw := range labels {
			parsed, err := semver.Parse(raw)
			if err != nil || !parsed.SatisfiesCaret(floor) {
				continue
			}
			if best == nil || best.Less(parsed) {
				best = &parsed
			}
		}
		if best == nil {
			return "", "", false, nil
		}
		return best.String(), newest.String(), true, nil
	default:
		var raw string
		err := tx.QueryRow(ctx, `SELECT ordered.semantic_version FROM (SELECT semantic_version,row_number() OVER (ORDER BY created_at,id) AS number FROM document_version WHERE tenant_id=$1 AND document_id=$2) ordered WHERE ordered.number=$3`, tenantID, ref.DocumentID, ref.PinnedVersion).Scan(&raw)
		if errors.Is(err, dbport.ErrNoRows) {
			return "", "", false, nil
		}
		if err != nil {
			return "", "", false, err
		}
		pinned, err := semver.Parse(raw)
		if err != nil {
			return "", "", false, nil
		}
		return pinned.String(), newest.String(), true, nil
	}
}
