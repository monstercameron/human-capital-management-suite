// Canonical document-link extraction for HUB-019: the parser reads
// doc:-scheme targets from Markdown versions and stores the stable target
// ID with optional pinned version and block, plus the source version and a
// validation state. Titles and slugs are never resolved, so renames cannot
// break stored links; forged targets are stored as malformed, never valid.
package documenthubstore

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Link validation states.
const (
	LinkValid     = "valid"
	LinkMalformed = "malformed"
)

// DocLink is one extracted document link.
type DocLink struct {
	Label, TargetDocID, PinnedVersion, Block, State string
}

func cleanIDPart(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// ExtractLinks parses canonical doc: links from Markdown source. Images
// with doc: targets count as links; external targets and title-style
// [[...]] references are not document links and are skipped.
func ExtractLinks(src string) []DocLink {
	var links []DocLink
	i := 0
	for i < len(src) {
		open := i
		if src[i] == '!' && i+1 < len(src) && src[i+1] == '[' {
			open = i + 1
		} else if src[i] != '[' {
			i++
			continue
		}
		label, target, next, ok := parseBracket(src, open)
		if !ok {
			i++
			continue
		}
		i = next
		scheme, rest, found := strings.Cut(target, ":")
		if !found || !strings.EqualFold(scheme, "doc") {
			continue
		}
		link := DocLink{Label: label, State: LinkMalformed}
		body, block, _ := strings.Cut(rest, "#")
		id, pinned, hasPin := strings.Cut(body, "@")
		link.TargetDocID, link.PinnedVersion, link.Block = id, "", block
		if hasPin {
			link.PinnedVersion = pinned
		}
		valid := cleanIDPart(id)
		if block != "" {
			valid = valid && cleanIDPart(block)
		}
		if hasPin {
			lower := strings.ToLower(pinned)
			if !strings.HasPrefix(lower, "docv-") {
				valid = false
			} else {
				valid = valid && cleanIDPart(strings.TrimPrefix(lower, "docv-"))
			}
		}
		if valid {
			link.State = LinkValid
		}
		links = append(links, link)
	}
	return links
}

// StoreLinks rebuilds the stored link set for one version: previous rows
// for that version are replaced, so the table always mirrors the current
// Markdown.
func (s *Store) StoreLinks(ctx context.Context, tenantID, docID, versionID string, links []DocLink) error {
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return storeLinksTx(ctx, tx, tenantID, docID, versionID, links)
	})
}

// storeLinksTx rebuilds the stored link set of one version with replace
// semantics, shared by direct extraction and the reconciler.
func storeLinksTx(ctx context.Context, tx dbport.Tx, tenantID, docID, versionID string, links []DocLink) error {
	if _, err := tx.Exec(ctx, `DELETE FROM document_link WHERE tenant_id=$1 AND source_document_id=$2 AND source_version_id=$3`, tenantID, docID, versionID); err != nil {
		return err
	}
	for _, l := range links {
		if _, err := tx.Exec(ctx, `INSERT INTO document_link(id,tenant_id,source_document_id,source_version_id,label,target_document_id,pinned_version_id,block_id,state) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			"docl-"+uuid.NewString(), tenantID, docID, versionID, l.Label, l.TargetDocID, l.PinnedVersion, l.Block, l.State); err != nil {
			return err
		}
	}
	return nil
}

// linkManifest renders links as canonical evidence rows for golden pins.
func linkManifest(links []DocLink) []map[string]string {
	rows := make([]map[string]string, 0, len(links))
	for _, l := range links {
		rows = append(rows, map[string]string{
			"block": l.Block, "label": l.Label, "pinned_version": l.PinnedVersion,
			"state": l.State, "target": l.TargetDocID,
		})
	}
	return rows
}
