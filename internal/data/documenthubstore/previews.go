package documenthubstore

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// MaxPreviewIDs bounds one DocumentPreviews batch.
const MaxPreviewIDs = 50

// previewSnippetRunes is how much plain text a preview carries.
const previewSnippetRunes = 180

// Preview is one document as a chat unfurl shows it. Title, OwnerID,
// UpdatedAt and Snippet are set only when Readable.
type Preview struct {
	DocumentID, Title, OwnerID, Snippet string
	UpdatedAt                           time.Time
	Readable                            bool
}

// DocumentPreviews resolves up to MaxPreviewIDs distinct document IDs for
// actorID in one statement, in request order. The readable rule is the
// ReadPersonalDocument rule (the listing selection), so a preview never
// shows a document its reader could not open; a document the actor cannot
// read, or that does not exist, reports only its ID.
func (s *Store) DocumentPreviews(ctx context.Context, tenantID, actorID string, ids []string) ([]Preview, error) {
	var wanted []string
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] || len(id) > 128 {
			continue
		}
		seen[id] = true
		wanted = append(wanted, id)
		if len(wanted) == MaxPreviewIDs {
			break
		}
	}
	out := make([]Preview, 0, len(wanted))
	if len(wanted) == 0 {
		return out, nil
	}
	options, _, err := normalizeListOptions(actorID, ListOptions{})
	if err != nil {
		return nil, err
	}
	found := map[string]Preview{}
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT d.id, d.owner_id, v.title, COALESCE(v.normalized_markdown,''), v.created_at `+listSelectionSQL+` AND v.id IS NOT NULL AND d.id=ANY($9::text[])`,
			append(listSelectionArgs(tenantID, actorID, options, nil), wanted)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p Preview
			var markdown string
			if err := rows.Scan(&p.DocumentID, &p.OwnerID, &p.Title, &markdown, &p.UpdatedAt); err != nil {
				return err
			}
			p.Readable, p.Snippet = true, previewSnippet(markdown, p.Title)
			found[p.DocumentID] = p
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	for _, id := range wanted {
		if p, ok := found[id]; ok {
			out = append(out, p)
			continue
		}
		out = append(out, Preview{DocumentID: id})
	}
	return out, nil
}

// previewSnippet is the opening of a document's plain text, without a
// leading repeat of its title, cut at a word boundary.
func previewSnippet(markdown, title string) string {
	text := strings.Join(strings.Fields(PlainText(markdown)), " ")
	if t := strings.TrimSpace(title); t != "" && strings.HasPrefix(strings.ToLower(text), strings.ToLower(t)) {
		text = strings.TrimSpace(text[len(t):])
	}
	if utf8.RuneCountInString(text) <= previewSnippetRunes {
		return text
	}
	runes := []rune(text)[:previewSnippetRunes]
	cut := string(runes)
	if i := strings.LastIndexByte(cut, ' '); i > previewSnippetRunes/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:") + "…"
}
