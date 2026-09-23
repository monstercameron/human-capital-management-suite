// Lexical search for HUB-025: title/heading/body terms indexed per version,
// searched only across currently deployed versions. The index never stores
// audience: every query prefilters live read grants in SQL and rechecks
// each hit with the grant authorizer, so revoked and denied readers see
// nothing. Stale index rows are harmless because deployment liveness is
// re-evaluated per query.
package documenthubstore

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// SearchHit is one deployed version matching a lexical query.
type SearchHit struct {
	DocumentID, VersionID, Title string
	Score                        int
	MatchedTerms                 int
}

// lexTokenize lowercases text into alphanumeric terms of length two or
// more, so queries and index terms share one canonical form.
func lexTokenize(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if len(f) >= 2 {
			out = append(out, f)
		}
	}
	return out
}

// IndexDeployedVersion rebuilds the lexical rows for one version, replacing
// prior rows so re-extraction stays idempotent. Title terms weigh 3,
// heading terms 2 and body terms 1 at query time.
func (s *Store) IndexDeployedVersion(ctx context.Context, tenantID, docID, versionID string) error {
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		version, err := loadVersion(ctx, tx, tenantID, docID, versionID)
		if err != nil {
			return err
		}
		return indexTermsTx(ctx, tx, tenantID, docID, version)
	})
}

// indexTermsTx rebuilds the lexical rows of one loaded version with
// replace semantics, shared by direct indexing and the reconciler.
func indexTermsTx(ctx context.Context, tx dbport.Tx, tenantID, docID string, version Version) error {
	if _, err := tx.Exec(ctx, `DELETE FROM document_search_term WHERE tenant_id=$1 AND version_id=$2`, tenantID, version.ID); err != nil {
		return err
	}
	counts := map[[2]string]int{}
	for _, term := range lexTokenize(version.Title) {
		counts[[2]string{term, "title"}]++
	}
	var headings []string
	for _, b := range DeriveBlocks(version.Markdown) {
		headings = append(headings, b.Heading)
	}
	for _, term := range lexTokenize(strings.Join(headings, "\n")) {
		counts[[2]string{term, "heading"}]++
	}
	for _, term := range lexTokenize(version.Markdown) {
		counts[[2]string{term, "body"}]++
	}
	for key, hits := range counts {
		if _, err := tx.Exec(ctx, `INSERT INTO document_search_term(tenant_id,document_id,version_id,term,field,hits) VALUES($1,$2,$3,$4,$5,$6)`,
			tenantID, docID, version.ID, key[0], key[1], hits); err != nil {
			return err
		}
	}
	return nil
}

// SearchLexical returns deployed versions matching the query terms, newest
// score first. Only versions with a live deployment pointer and a
// non-retired status qualify, only documents with a live allow grant and
// no live deny grant for the reader are candidates, and each surviving hit
// is rechecked with the grant authorizer before it is returned.
func (s *Store) SearchLexical(ctx context.Context, tenantID, query, readerKind, readerID string) ([]SearchHit, error) {
	terms := lexTokenize(query)
	if len(terms) == 0 {
		return nil, nil
	}
	var hits []SearchHit
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT t.document_id, t.version_id, v.title,
				sum(t.hits * CASE t.field WHEN 'title' THEN 3 WHEN 'heading' THEN 2 ELSE 1 END),
				count(DISTINCT t.term)
			FROM document_search_term t
			JOIN document_version v ON v.tenant_id=t.tenant_id AND v.id=t.version_id
			WHERE t.tenant_id=$1 AND t.term = ANY($2)
			  AND v.status <> 'retired'
			  AND EXISTS (SELECT 1 FROM document_active_pointer p
					JOIN document_deployment d ON d.tenant_id=p.tenant_id AND d.id=p.deployment_id
					WHERE p.tenant_id=t.tenant_id AND p.document_id=t.document_id AND d.version_id=t.version_id)
			  AND EXISTS (SELECT 1 FROM document doc WHERE doc.tenant_id=t.tenant_id AND doc.id=t.document_id
					AND (doc.home<>'PERSONAL' OR EXISTS (SELECT 1 FROM document_grant shared
					  WHERE shared.tenant_id=doc.tenant_id AND shared.document_id=doc.id
					  AND shared.subject_kind='person' AND shared.subject_id<>doc.owner_id AND shared.action='read'
					  AND shared.effect='allow' AND shared.revoked=false AND (shared.expires_at IS NULL OR shared.expires_at>now())
					  AND NOT EXISTS (SELECT 1 FROM document_grant deny WHERE deny.tenant_id=shared.tenant_id
					    AND deny.document_id=shared.document_id AND deny.subject_kind='person' AND deny.subject_id=shared.subject_id
					    AND deny.action='read' AND deny.effect='deny' AND deny.revoked=false AND (deny.expires_at IS NULL OR deny.expires_at>now())))))
			  AND EXISTS (SELECT 1 FROM document_grant g
					WHERE g.tenant_id=t.tenant_id AND g.document_id=t.document_id
					  AND g.subject_kind=$3 AND g.subject_id=$4 AND g.action='read'
					  AND g.effect='allow' AND g.revoked=false AND (g.expires_at IS NULL OR g.expires_at>now()))
			  AND NOT EXISTS (SELECT 1 FROM document_grant g
					WHERE g.tenant_id=t.tenant_id AND g.document_id=t.document_id
					  AND g.subject_kind=$3 AND g.subject_id=$4 AND g.action='read'
					  AND g.effect='deny' AND g.revoked=false AND (g.expires_at IS NULL OR g.expires_at>now()))
			GROUP BY t.document_id, t.version_id, v.title`,
			tenantID, terms, readerKind, readerID)
		if err != nil {
			return err
		}
		var candidates []SearchHit
		for rows.Next() {
			var h SearchHit
			if err := rows.Scan(&h.DocumentID, &h.VersionID, &h.Title, &h.Score, &h.MatchedTerms); err != nil {
				rows.Close()
				return err
			}
			candidates = append(candidates, h)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, h := range candidates {
			if err := authorizeTx(ctx, tx, tenantID, h.DocumentID, readerKind, readerID, ActionRead); err != nil {
				continue
			}
			hits = append(hits, h)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		if hits[i].DocumentID != hits[j].DocumentID {
			return hits[i].DocumentID < hits[j].DocumentID
		}
		return hits[i].VersionID < hits[j].VersionID
	})
	return hits, nil
}
