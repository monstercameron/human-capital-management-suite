// Typed search filters for HUB-030: team, channel, status, owner, locale
// and date are applied in the same statement as the reader's grant checks,
// so a facet can only narrow an already authorized result and never widen
// or reveal one. Every filter and every authorization check runs before a
// title, snippet, count or citation is built, so a restricted document
// cannot surface through any facet.
package documenthubstore

import (
	"context"
	"sort"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// SearchFilters narrows a lexical search to typed facets. An empty string
// or zero time skips that filter. TeamID and ChannelID require a live read
// grant to that team or channel on the document itself (the filter never
// substitutes for the reader's own authorization, which is always
// rechecked separately). Status matches the exact document_version status
// (deployed, stale, retired, ...); DateFrom/DateTo bound the deployment's
// effective time.
type SearchFilters struct {
	TeamID    string
	ChannelID string
	Status    string
	OwnerID   string
	Locale    string
	DateFrom  time.Time
	DateTo    time.Time
}

// FilteredSearchHit is one deployed version that matched the query, every
// requested filter, and the reader's own current authorization. Status,
// Locale, OwnerID and DeployedAt double as the result's citation: exact
// version, status and effective date, never a candidate or stale guess.
type FilteredSearchHit struct {
	DocumentID, VersionID, Title string
	Status, Locale, OwnerID      string
	DeployedAt                   time.Time
	Score, MatchedTerms          int
}

// FilteredSearchResult is one bounded page plus the total count of
// authorized, filtered matches. Total is computed from the same
// authorized and filtered set as Hits, so it never discloses the
// existence of a document the reader may not see.
type FilteredSearchResult struct {
	Hits  []FilteredSearchHit
	Total int
}

// filteredSearchRow is one row of searchFilteredSQL before the per-hit
// authorization recheck.
type filteredSearchRow struct {
	documentID, versionID, title, status, locale, ownerID string
	deployedAt                                            time.Time
	score, matchedTerms                                   int
}

// searchFilteredSQL selects deployed, non-retired versions matching the
// lexical term set, joined to the document's owner and its live default
// deployment, with every typed filter applied in the WHERE clause
// alongside the reader's own grant checks. Parameters: $1 tenant, $2
// terms, $3 reader kind, $4 reader id, $5 status, $6 owner, $7 locale,
// $8 date-from, $9 date-to, $10 team id, $11 channel id.
const searchFilteredSQL = `SELECT t.document_id, t.version_id, v.title, v.status, v.locale, doc.owner_id, dep.effective_at,
		sum(t.hits * CASE t.field WHEN 'title' THEN 3 WHEN 'heading' THEN 2 ELSE 1 END),
		count(DISTINCT t.term)
	FROM document_search_term t
	JOIN document_version v ON v.tenant_id=t.tenant_id AND v.id=t.version_id
	JOIN document doc ON doc.tenant_id=t.tenant_id AND doc.id=t.document_id
	JOIN document_active_pointer p ON p.tenant_id=t.tenant_id AND p.document_id=t.document_id
		AND p.scope_kind='default' AND p.scope_id='' AND p.version_id=t.version_id
	JOIN document_deployment dep ON dep.tenant_id=p.tenant_id AND dep.id=p.deployment_id
	WHERE t.tenant_id=$1 AND t.term = ANY($2)
	  AND v.status <> 'retired'
	  AND ($5::text='' OR v.status=$5::text)
	  AND ($6::text='' OR doc.owner_id=$6::text)
	  AND ($7::text='' OR v.locale=$7::text)
	  AND ($8::timestamptz IS NULL OR dep.effective_at>=$8::timestamptz)
	  AND ($9::timestamptz IS NULL OR dep.effective_at<=$9::timestamptz)
	  AND ($10::text='' OR EXISTS (SELECT 1 FROM document_grant tg WHERE tg.tenant_id=doc.tenant_id AND tg.document_id=doc.id
			AND tg.subject_kind='team' AND tg.subject_id=$10::text AND tg.action='read' AND tg.effect='allow' AND tg.revoked=false
			AND (tg.expires_at IS NULL OR tg.expires_at>now())
			AND NOT EXISTS (SELECT 1 FROM document_grant td WHERE td.tenant_id=tg.tenant_id AND td.document_id=tg.document_id
				AND td.subject_kind='team' AND td.subject_id=tg.subject_id AND td.action='read' AND td.effect='deny' AND td.revoked=false
				AND (td.expires_at IS NULL OR td.expires_at>now()))))
	  AND ($11::text='' OR EXISTS (SELECT 1 FROM document_grant cg WHERE cg.tenant_id=doc.tenant_id AND cg.document_id=doc.id
			AND cg.subject_kind='channel' AND cg.subject_id=$11::text AND cg.action='read' AND cg.effect='allow' AND cg.revoked=false
			AND (cg.expires_at IS NULL OR cg.expires_at>now())
			AND NOT EXISTS (SELECT 1 FROM document_grant cd WHERE cd.tenant_id=cg.tenant_id AND cd.document_id=cg.document_id
				AND cd.subject_kind='channel' AND cd.subject_id=cg.subject_id AND cd.action='read' AND cd.effect='deny' AND cd.revoked=false
				AND (cd.expires_at IS NULL OR cd.expires_at>now()))))
	  AND (doc.home<>'PERSONAL' OR EXISTS (SELECT 1 FROM document_grant shared
			WHERE shared.tenant_id=doc.tenant_id AND shared.document_id=doc.id
			AND shared.subject_kind='person' AND shared.subject_id<>doc.owner_id AND shared.action='read'
			AND shared.effect='allow' AND shared.revoked=false AND (shared.expires_at IS NULL OR shared.expires_at>now())
			AND NOT EXISTS (SELECT 1 FROM document_grant deny WHERE deny.tenant_id=shared.tenant_id
				AND deny.document_id=shared.document_id AND deny.subject_kind='person' AND deny.subject_id=shared.subject_id
				AND deny.action='read' AND deny.effect='deny' AND deny.revoked=false AND (deny.expires_at IS NULL OR deny.expires_at>now()))))
	  AND EXISTS (SELECT 1 FROM document_grant g
			WHERE g.tenant_id=t.tenant_id AND g.document_id=t.document_id
			  AND g.subject_kind=$3 AND g.subject_id=$4 AND g.action='read'
			  AND g.effect='allow' AND g.revoked=false AND (g.expires_at IS NULL OR g.expires_at>now()))
	  AND NOT EXISTS (SELECT 1 FROM document_grant g
			WHERE g.tenant_id=t.tenant_id AND g.document_id=t.document_id
			  AND g.subject_kind=$3 AND g.subject_id=$4 AND g.action='read'
			  AND g.effect='deny' AND g.revoked=false AND (g.expires_at IS NULL OR g.expires_at>now()))
	GROUP BY t.document_id, t.version_id, v.title, v.status, v.locale, doc.owner_id, dep.effective_at`

// SearchLexicalFiltered runs a lexical query with typed team, channel,
// status, owner, locale and date filters applied server-side in the same
// statement as the reader's grant checks, then rechecks every surviving
// hit with the grant authorizer exactly like SearchLexical. No facet can
// widen or reveal access: a team/channel filter only narrows an already
// authorized set, so a title, owner, status or count for a document the
// reader may not read is never returned.
func (s *Store) SearchLexicalFiltered(ctx context.Context, tenantID, query, readerKind, readerID string, filters SearchFilters) (FilteredSearchResult, error) {
	terms := lexTokenize(query)
	if len(terms) == 0 {
		return FilteredSearchResult{}, nil
	}
	var hits []FilteredSearchHit
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, searchFilteredSQL,
			tenantID, terms, readerKind, readerID,
			filters.Status, filters.OwnerID, filters.Locale,
			nullableTime(filters.DateFrom), nullableTime(filters.DateTo),
			filters.TeamID, filters.ChannelID)
		if err != nil {
			return err
		}
		var candidates []filteredSearchRow
		for rows.Next() {
			var r filteredSearchRow
			if err := rows.Scan(&r.documentID, &r.versionID, &r.title, &r.status, &r.locale, &r.ownerID, &r.deployedAt, &r.score, &r.matchedTerms); err != nil {
				rows.Close()
				return err
			}
			candidates = append(candidates, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, r := range candidates {
			if err := authorizeTx(ctx, tx, tenantID, r.documentID, readerKind, readerID, ActionRead); err != nil {
				continue
			}
			hits = append(hits, FilteredSearchHit{
				DocumentID: r.documentID, VersionID: r.versionID, Title: r.title,
				Status: r.status, Locale: r.locale, OwnerID: r.ownerID, DeployedAt: r.deployedAt,
				Score: r.score, MatchedTerms: r.matchedTerms,
			})
		}
		return nil
	})
	if err != nil {
		return FilteredSearchResult{}, err
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
	if len(hits) > MaxSearchResults {
		hits = hits[:MaxSearchResults]
	}
	return FilteredSearchResult{Hits: hits, Total: len(hits)}, nil
}
