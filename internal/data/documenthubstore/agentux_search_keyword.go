package documenthubstore

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// workspaceKeywordStopwords are dropped from a question before matching, so
// "which company holidays are coming up in the rest of 2026" is matched on
// company, holiday, coming, rest and 2026.
var workspaceKeywordStopwords = map[string]bool{
	"a": true, "an": true, "and": true, "any": true, "are": true, "at": true, "be": true, "by": true, "can": true, "do": true,
	"does": true, "for": true, "from": true, "get": true, "give": true, "has": true, "have": true, "how": true, "in": true,
	"is": true, "it": true, "list": true, "me": true, "my": true, "of": true, "on": true, "or": true, "our": true, "please": true,
	"tell": true, "that": true, "the": true, "their": true, "there": true, "this": true, "to": true, "up": true, "us": true,
	"was": true, "we": true, "what": true, "when": true, "where": true, "which": true, "who": true, "will": true, "with": true,
	"you": true, "your": true,
}

// keywordStem folds a plural so "holidays" matches "holiday".
func keywordStem(word string) string {
	if len(word) > 4 && strings.HasSuffix(word, "ies") {
		return word[:len(word)-3] + "y"
	}
	if len(word) > 3 && strings.HasSuffix(word, "s") && !strings.HasSuffix(word, "ss") {
		return word[:len(word)-1]
	}
	return word
}

func keywordTerms(query string) []string {
	seen := map[string]bool{}
	var out []string
	for _, word := range lexTokenize(query) {
		if workspaceKeywordStopwords[word] {
			continue
		}
		if stem := keywordStem(word); !seen[stem] {
			seen[stem] = true
			out = append(out, stem)
		}
	}
	return out
}

func keywordMatches(terms []string, text string) map[string]bool {
	words := map[string]bool{}
	for _, word := range lexTokenize(text) {
		words[keywordStem(word)] = true
	}
	matched := map[string]bool{}
	for _, term := range terms {
		if words[term] {
			matched[term] = true
		}
	}
	return matched
}

// SearchWorkspaceKeyword is the workspace search used when no embedding model
// is available. It reads the same grant-filtered published candidate set as
// SearchWorkspaceMeaning, so every document it can return is one every member
// of the workspace and the asker may read; only the ranking differs: a section
// scores by how many distinct words of the question its heading, the document
// title and its text contain. Nothing here reads a vector or leaves the store.
func (s *Store) SearchWorkspaceKeyword(ctx context.Context, tenant, actor, query string, members []string) ([]WorkspaceSectionHit, error) {
	if !validWorkspaceMembers(members) || actor == "" {
		return nil, ErrDenied
	}
	terms := keywordTerms(query)
	if len(terms) == 0 {
		return nil, nil
	}
	need := max(1, (len(terms)+2)/3)
	var out []WorkspaceSectionHit
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT d.id,d.owner_id,v.id,v.title,v.normalized_markdown,v.created_at`+workspaceSelectionSQL+`
		AND EXISTS (SELECT 1 FROM document_grant g WHERE g.tenant_id=d.tenant_id AND g.document_id=d.id AND g.subject_kind='person' AND g.subject_id=$3 AND g.action='read' AND g.effect='allow' AND NOT g.revoked AND (g.expires_at IS NULL OR g.expires_at>now()))
		AND NOT EXISTS (SELECT 1 FROM document_grant g WHERE g.tenant_id=d.tenant_id AND g.document_id=d.id AND g.subject_kind='person' AND g.subject_id=$3 AND g.action='read' AND g.effect='deny' AND NOT g.revoked AND (g.expires_at IS NULL OR g.expires_at>now()))`, tenant, members, actor)
		if err != nil {
			return err
		}
		var candidates []searchCandidate
		for rows.Next() {
			var c searchCandidate
			if err := rows.Scan(&c.id, &c.owner, &c.versionID, &c.title, &c.markdown, &c.updated); err != nil {
				rows.Close()
				return err
			}
			candidates = append(candidates, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		type scored struct {
			hit     WorkspaceSectionHit
			matched int
			title   int
			updated int64
		}
		var found []scored
		for _, c := range candidates {
			titleMatches := keywordMatches(terms, c.title)
			sections := SplitSections(c.markdown)
			if len(sections) == 0 {
				sections = []Section{{Text: c.markdown}}
			}
			var best *scored
			for _, section := range sections {
				matched := keywordMatches(terms, c.title+"\n"+section.Text)
				if len(matched) < need {
					continue
				}
				if best == nil || len(matched) > best.matched {
					best = &scored{hit: WorkspaceSectionHit{DocumentID: c.id, VersionID: c.versionID, Title: c.title, OwnerID: c.owner, SectionAnchor: section.BlockID, Text: section.Text, Score: float64(len(matched)) / float64(len(terms))}, matched: len(matched), title: len(titleMatches), updated: c.updated.UnixNano()}
				}
			}
			if best != nil {
				found = append(found, *best)
			}
		}
		sort.SliceStable(found, func(i, j int) bool {
			a, b := found[i], found[j]
			if a.matched != b.matched {
				return a.matched > b.matched
			}
			if a.title != b.title {
				return a.title > b.title
			}
			if a.updated != b.updated {
				return a.updated > b.updated
			}
			return a.hit.DocumentID < b.hit.DocumentID
		})
		for _, f := range found[:min(len(found), 5)] {
			// The index is never an access path: every returned document is checked
			// again through the hub's own read decision.
			if err := authorizeTx(ctx, tx, tenant, f.hit.DocumentID, "person", actor, ActionRead); err != nil {
				if errors.Is(err, ErrDenied) {
					continue
				}
				return err
			}
			out = append(out, f.hit)
		}
		return nil
	})
	return out, err
}
