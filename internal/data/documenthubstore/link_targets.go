package documenthubstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// LinkTarget is one doc: link target as the actor would find it now:
// Readable when ReadPersonalDocument would open it, and Title only then.
type LinkTarget struct {
	DocumentID, Title string
	Readable          bool
}

// LinkTargets resolves the distinct valid doc: targets of markdown for
// actorID in one query, in first-appearance order. A target the actor
// cannot open reports only its ID, so links never reveal titles.
func (s *Store) LinkTargets(ctx context.Context, tenantID, actorID, markdown string) ([]LinkTarget, error) {
	var ids []string
	seen := map[string]bool{}
	for _, l := range ExtractLinks(markdown) {
		if l.State != LinkValid || seen[l.TargetDocID] {
			continue
		}
		seen[l.TargetDocID] = true
		ids = append(ids, l.TargetDocID)
	}
	out := make([]LinkTarget, 0, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	options, _, err := normalizeListOptions(actorID, ListOptions{})
	if err != nil {
		return nil, err
	}
	var titles map[string]string
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		titles, err = readableTitlesTx(ctx, tx, tenantID, actorID, options, ids)
		return err
	})
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		title, ok := titles[id]
		out = append(out, LinkTarget{DocumentID: id, Title: title, Readable: ok})
	}
	return out, nil
}

// readableTitlesTx returns the current title of every id the actor may
// open, in one query: the listing selection is the ReadPersonalDocument
// rule (readable, with a non-retired version the actor may see).
func readableTitlesTx(ctx context.Context, tx dbport.Tx, tenantID, actorID string, options ListOptions, ids []string) (map[string]string, error) {
	rows, err := tx.Query(ctx, `SELECT d.id, v.title `+listSelectionSQL+` AND v.id IS NOT NULL AND d.id=ANY($9::text[])`,
		append(listSelectionArgs(tenantID, actorID, options, nil), ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	titles := map[string]string{}
	for rows.Next() {
		var id, title string
		if err := rows.Scan(&id, &title); err != nil {
			return nil, err
		}
		titles[id] = title
	}
	return titles, rows.Err()
}
