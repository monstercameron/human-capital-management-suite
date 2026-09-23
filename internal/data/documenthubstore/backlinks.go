// Authorized backlinks and stale references for HUB-022: the reverse
// index of who links to a document, showing only jointly readable
// source/target pairs, plus a checker that marks broken or stale targets
// and alerts owners once per transition. Retired sources leave the
// backlink set; renamed anchors read stale.
package documenthubstore

import (
	"context"
	"encoding/json"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Checker-marked link states, extending the extraction states in links.go.
const (
	LinkBroken = "broken"
	LinkStale  = "stale"
)

// LinkStaleAnchor marks a link whose block anchor no longer exists on the
// target's current versions.
const LinkStaleAnchor = "stale-anchor"

// Backlink is one inbound link to a document from a jointly readable source.
type Backlink struct {
	SourceDocID, SourceVersionID, SourceTitle string
	Label, Block, State                       string
}

// Backlinks lists inbound links to a document for a reader who may read
// the target. Each source must be readable too, and retired sources are
// excluded, so no restricted title ever leaks through the reverse index.
func (s *Store) Backlinks(ctx context.Context, tenantID, targetDocID, readerKind, readerID string) ([]Backlink, error) {
	var out []Backlink
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var exists int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document WHERE tenant_id=$1 AND id=$2`,
			tenantID, targetDocID).Scan(&exists); err != nil || exists == 0 {
			return ErrDenied
		}
		if err := authorizeTx(ctx, tx, tenantID, targetDocID, readerKind, readerID, ActionRead); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT l.source_document_id, l.source_version_id, l.label, l.block_id, l.state, v.title, v.status
			FROM document_link l JOIN document_version v
			  ON v.tenant_id=l.tenant_id AND v.id=l.source_version_id
			WHERE l.tenant_id=$1 AND l.target_document_id=$2 AND l.state<>$3
			ORDER BY l.source_document_id, l.source_version_id`,
			tenantID, targetDocID, LinkMalformed)
		if err != nil {
			return err
		}
		type candidate struct {
			source, version, label, block, state, title, status string
		}
		var found []candidate
		for rows.Next() {
			var c candidate
			if err := rows.Scan(&c.source, &c.version, &c.label, &c.block, &c.state, &c.title, &c.status); err != nil {
				rows.Close()
				return err
			}
			found = append(found, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, c := range found {
			if c.status == "retired" {
				continue
			}
			if err := authorizeTx(ctx, tx, tenantID, c.source, readerKind, readerID, ActionRead); err != nil {
				continue
			}
			out = append(out, Backlink{
				SourceDocID: c.source, SourceVersionID: c.version, SourceTitle: c.title,
				Label: c.label, Block: c.block, State: c.state,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CheckLinks re-evaluates every stored link of a version as the checker,
// marks broken or stale states, and alerts the document owner once per
// transition through the outbox.
func (s *Store) CheckLinks(ctx context.Context, tenantID, docID, versionID, actorID string) (LinkReport, error) {
	var report LinkReport
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := authorizeOwnerTx(ctx, tx, tenantID, docID, actorID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id,label,target_document_id,pinned_version_id,block_id,state FROM document_link WHERE tenant_id=$1 AND source_document_id=$2 AND source_version_id=$3 ORDER BY label,target_document_id`,
			tenantID, docID, versionID)
		if err != nil {
			return err
		}
		var links []storedLink
		for rows.Next() {
			var l storedLink
			if err := rows.Scan(&l.id, &l.label, &l.target, &l.pinned, &l.block, &l.state); err != nil {
				rows.Close()
				return err
			}
			links = append(links, l)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		var owner string
		if err := tx.QueryRow(ctx, `SELECT owner_id FROM document WHERE tenant_id=$1 AND id=$2`, tenantID, docID).Scan(&owner); err != nil {
			return ErrDenied
		}
		for _, l := range links {
			code, detail, state := checkLinkTx(ctx, tx, tenantID, docID, versionID, l)
			if code == "" {
				continue
			}
			report.Findings = append(report.Findings, LinkFinding{
				Label: l.label, TargetDocID: l.target, PinnedVersion: l.pinned,
				Block: l.block, Code: code, Severity: SeverityError, Detail: detail,
			})
			if state == "" || state == l.state {
				continue
			}
			if _, err := tx.Exec(ctx, `UPDATE document_link SET state=$1 WHERE tenant_id=$2 AND id=$3`, state, tenantID, l.id); err != nil {
				return err
			}
			event := "link.stale"
			if state == LinkBroken {
				event = "link.broken"
			}
			payload, err := json.Marshal(map[string]string{
				"document_id": docID, "version_id": versionID, "label": l.label,
				"target_document_id": l.target, "code": code, "owner_id": owner,
			})
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO document_outbox(tenant_id,aggregate_id,event_type,payload) VALUES($1,$2,$3,$4)`,
				tenantID, docID, event, string(payload)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return LinkReport{}, err
	}
	return report, nil
}

// storedLink is one persisted canonical link under review.
type storedLink struct {
	id, label, target, pinned, block, state string
}

// checkLinkTx evaluates one stored link, returning the finding code,
// detail and the state to mark, or empty strings when the link resolves.
func checkLinkTx(ctx context.Context, tx dbport.Tx, tenantID, docID, versionID string, l storedLink) (string, string, string) {
	if l.state == LinkMalformed {
		return LinkMalformed, "link target is not a canonical document reference", ""
	}
	var target int
	if err := tx.QueryRow(ctx, `SELECT 1 FROM document WHERE tenant_id=$1 AND id=$2`, tenantID, l.target).Scan(&target); err != nil {
		return LinkUnknownTarget, "target document does not exist", LinkBroken
	}
	if l.pinned != "" {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM document_version WHERE tenant_id=$1 AND id=$2`, tenantID, l.pinned).Scan(&status); err != nil {
			return LinkUnknownVersion, "pinned version does not exist", LinkBroken
		}
		if status == "retired" {
			return LinkRetiredTarget, "pinned version is retired", LinkStale
		}
	}
	var sourceStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM document_version WHERE tenant_id=$1 AND id=$2`, tenantID, versionID).Scan(&sourceStatus); err == nil && sourceStatus == "retired" {
		return LinkRetiredTarget, "source version is retired", LinkStale
	}
	if l.block != "" {
		stale, skip := anchorStaleTx(ctx, tx, tenantID, l.target, l.pinned, l.block)
		if !skip && stale {
			return LinkStaleAnchor, "block anchor no longer exists on the target", LinkStale
		}
	}
	return "", "", ""
}

// anchorStaleTx reports whether a block anchor is missing from the
// versions it should resolve against: the pinned version, or the target's
// currently deployed versions. Versions with no extracted blocks are
// skipped, so links are never flagged for an index that was never built.
func anchorStaleTx(ctx context.Context, tx dbport.Tx, tenantID, targetDocID, pinned, block string) (stale, skip bool) {
	versions := []string{}
	if pinned != "" {
		versions = append(versions, pinned)
	} else {
		rows, err := tx.Query(ctx, `SELECT DISTINCT d.version_id FROM document_active_pointer p
			JOIN document_deployment d ON d.tenant_id=p.tenant_id AND d.id=p.deployment_id
			WHERE p.tenant_id=$1 AND p.document_id=$2`, tenantID, targetDocID)
		if err != nil {
			return false, true
		}
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				rows.Close()
				return false, true
			}
			versions = append(versions, v)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return false, true
		}
	}
	if len(versions) == 0 {
		return false, true
	}
	checked := false
	for _, v := range versions {
		var total int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_block WHERE tenant_id=$1 AND version_id=$2`,
			tenantID, v).Scan(&total); err != nil || total == 0 {
			continue
		}
		checked = true
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_block WHERE tenant_id=$1 AND version_id=$2 AND block_id=$3`,
			tenantID, v, block).Scan(&n); err != nil {
			return false, true
		}
		if n > 0 {
			return false, false
		}
	}
	if !checked {
		return false, true
	}
	return true, false
}
