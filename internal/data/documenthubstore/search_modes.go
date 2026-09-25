// Library search modes. A query runs over the caller's authorized
// candidate set only: the documents ListPersonalDocumentsPage would list
// for the same folder, star, owner and collection filters, each reduced to
// the one version the caller may read (the owner's latest candidate,
// anyone else's active deployment). Matching, fuzzy scoring, vector
// ranking, fusion and snippets all work on that set in Go, so no mode can
// see text, vectors or documents the plain listing would not show. At the
// few thousand documents one person can read this is a bounded scan;
// fuzzy scoring mirrors pg_trgm's trigram similarity without needing the
// extension.
package documenthubstore

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Search modes.
const (
	SearchSmart    = "smart"
	SearchContains = "contains"
	SearchFuzzy    = "fuzzy"
	SearchMeaning  = "meaning"
)

// Match reasons, strongest first.
const (
	MatchTitle   = "title"
	MatchText    = "text"
	MatchFuzzy   = "fuzzy"
	MatchMeaning = "meaning"
)

// Search bounds and thresholds.
const (
	// MaxSearchResults caps one query's matches and so its total count.
	MaxSearchResults = 1000
	// fuzzyThreshold is the similarity every query word needs. A word's
	// similarity is the larger of its pg_trgm trigram similarity and its
	// edit similarity (one minus the Damerau-Levenshtein distance over the
	// longer length, counted only within fuzzyMaxEdits). Trigrams alone at
	// pg_trgm's 0.3 default matched every "-ording" word for "onbaording"
	// and missed short transpositions such as "clsoe".
	fuzzyThreshold = 0.45
	// fuzzyTitleBonus lifts a document whose title holds the near match.
	fuzzyTitleBonus = 0.5
	// Meaning mode keeps up to meaningLimit sections at or above
	// meaningFloor; smart fusion takes only the strongest few.
	meaningFloor      = 0.25
	meaningLimit      = 50
	smartMeaningFloor = 0.35
	smartMeaningLimit = 10
	// Cosine scores of one embedding model bunch together, so an absolute
	// floor alone let a query about "time away from work" fill a whole page.
	// A section must also come within this share of the best section's
	// score: meaningRelative in meaning mode, smartMeaningRelative when
	// meaning only adds to smart's word matches. When the word legs found
	// something, smart adds at most smartMeaningLexicalLimit meaning-only
	// documents, so a multi-word title query is not trailed by look-alikes.
	meaningRelative          = 0.8
	smartMeaningRelative     = 0.9
	smartMeaningLexicalLimit = 3
	// rrfK is the reciprocal-rank-fusion constant.
	rrfK = 60
	// snippetWindow is the target snippet length; snippets never exceed
	// snippetMax.
	snippetWindow = 200
	snippetMax    = 240
)

// SearchResult is one page of matches with the selection's total and the
// mode that actually ran.
type SearchResult struct {
	Rows  []DocumentSummary
	Total int
	Mode  string
}

type searchCandidate struct {
	id, versionID, title, markdown string
	owner                          string
	updated                        time.Time
	*versionText
}

// versionText is the derived plain text and vocabulary of one immutable
// version, cached by tenant and version ID because versions never change.
type versionText struct {
	plain, lowerPlain, lowerTitle string
	titleWords, bodyWords         []string
	// display and lowerDisplay are the snippet text (snippetText): matching
	// runs on plain, excerpts are cut from display.
	display, lowerDisplay string
}

// versionTextCacheMax bounds the cache; when full it starts over.
const versionTextCacheMax = 20000

type versionTextCache struct {
	mu sync.Mutex
	m  map[string]*versionText
}

func (c *versionTextCache) get(key, title, markdown string) *versionText {
	if c != nil {
		c.mu.Lock()
		hit, ok := c.m[key]
		c.mu.Unlock()
		if ok {
			return hit
		}
	}
	plain := plainBody(title, markdown)
	display := snippetText(title, markdown)
	v := &versionText{plain: plain, lowerPlain: strings.ToLower(plain), lowerTitle: strings.ToLower(title), titleWords: uniqueWords(title), bodyWords: uniqueWords(plain), display: display, lowerDisplay: strings.ToLower(display)}
	if c != nil {
		c.mu.Lock()
		if c.m == nil || len(c.m) >= versionTextCacheMax {
			c.m = map[string]*versionText{}
		}
		c.m[key] = v
		c.mu.Unlock()
	}
	return v
}

type searchHit struct {
	id, match, snippet string
	score              float64
}

// SearchPersonalDocuments runs options.Query in options.Mode over the
// caller's authorized candidates and returns one page. Meaning runs only
// with options.QueryVector and options.VectorModel; without them a
// meaning request runs smart and says so. Sort defaults to relevance.
func (s *Store) SearchPersonalDocuments(ctx context.Context, tenantID, actorID string, options ListOptions) (SearchResult, error) {
	options, terms, err := normalizeListOptions(actorID, options)
	if err != nil {
		return SearchResult{}, err
	}
	mode := options.Mode
	switch mode {
	case SearchContains, SearchFuzzy, SearchMeaning:
	default:
		mode = SearchSmart
	}
	semantic := len(options.QueryVector) > 0 && options.VectorModel != ""
	if mode == SearchMeaning && !semantic {
		mode = SearchSmart
	}
	result := SearchResult{Mode: mode, Rows: []DocumentSummary{}}
	query := options.Query
	if query == "" {
		return result, nil
	}
	selection := options
	selection.Query = ""
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		candidates, err := loadSearchCandidates(ctx, tx, tenantID, actorID, selection, s.texts)
		if err != nil {
			return err
		}
		var vectors map[string]sectionMatch
		if semantic && (mode == SearchMeaning || mode == SearchSmart) {
			vectors, err = bestSections(ctx, tx, tenantID, options.VectorModel, options.QueryVector, candidates)
			if err != nil {
				return err
			}
		}
		hits := rankSearch(mode, query, terms, candidates, vectors)
		byID := make(map[string]*searchCandidate, len(candidates))
		for i := range candidates {
			byID[candidates[i].id] = &candidates[i]
		}
		sortHits(hits, options.Sort, byID, options.OwnerNames)
		if len(hits) > MaxSearchResults {
			hits = hits[:MaxSearchResults]
		}
		result.Total = len(hits)
		if options.Offset >= len(hits) {
			return nil
		}
		page := hits[options.Offset:min(len(hits), options.Offset+options.Limit)]
		rows, err := projectRows(ctx, tx, tenantID, actorID, selection, page)
		if err != nil {
			return err
		}
		result.Rows = rows
		return nil
	})
	if err != nil {
		return SearchResult{}, err
	}
	return result, nil
}

func loadSearchCandidates(ctx context.Context, tx dbport.Tx, tenantID, actorID string, selection ListOptions, texts *versionTextCache) ([]searchCandidate, error) {
	rows, err := tx.Query(ctx, `SELECT d.id, d.owner_id, v.id, v.title, v.normalized_markdown, v.created_at `+listSelectionSQL+` AND v.id IS NOT NULL`,
		listSelectionArgs(tenantID, actorID, selection, nil)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []searchCandidate
	for rows.Next() {
		var c searchCandidate
		if err := rows.Scan(&c.id, &c.owner, &c.versionID, &c.title, &c.markdown, &c.updated); err != nil {
			return nil, err
		}
		c.versionText = texts.get(tenantID+"\x00"+c.versionID, c.title, c.markdown)
		out = append(out, c)
	}
	return out, rows.Err()
}

// sectionMatch is one document's best section for the query vector.
type sectionMatch struct {
	blockID string
	cosine  float64
}

// bestSections ranks the stored section vectors of exactly the candidate
// versions, so meaning search inherits the candidate set's access rule.
func bestSections(ctx context.Context, tx dbport.Tx, tenantID, modelID string, qvec []float32, candidates []searchCandidate) (map[string]sectionMatch, error) {
	versionDoc := make(map[string]string, len(candidates))
	ids := make([]string, 0, len(candidates))
	for _, c := range candidates {
		versionDoc[c.versionID] = c.id
		ids = append(ids, c.versionID)
	}
	out := map[string]sectionMatch{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT version_id, block_id, vector FROM document_section_vector WHERE tenant_id=$1 AND model_id=$2 AND version_id=ANY($3::text[])`, tenantID, modelID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var versionID, blockID string
		var raw []byte
		if err := rows.Scan(&versionID, &blockID, &raw); err != nil {
			return nil, err
		}
		doc, ok := versionDoc[versionID]
		if !ok {
			continue
		}
		c := cosine(qvec, SectionVector{raw: raw}.Dimensions())
		if best, seen := out[doc]; !seen || c > best.cosine || (c == best.cosine && blockID < best.blockID) {
			out[doc] = sectionMatch{blockID: blockID, cosine: c}
		}
	}
	return out, rows.Err()
}

// rankSearch returns matches in relevance order.
func rankSearch(mode, query string, terms []string, candidates []searchCandidate, vectors map[string]sectionMatch) []searchHit {
	byUpdated := func(hits []searchHit, index map[string]*searchCandidate) {
		sort.SliceStable(hits, func(i, j int) bool {
			if hits[i].score != hits[j].score {
				return hits[i].score > hits[j].score
			}
			a, b := index[hits[i].id], index[hits[j].id]
			if !a.updated.Equal(b.updated) {
				return a.updated.After(b.updated)
			}
			return a.id < b.id
		})
	}
	index := make(map[string]*searchCandidate, len(candidates))
	for i := range candidates {
		index[candidates[i].id] = &candidates[i]
	}
	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	var exact, contains, fuzzy, meaning []searchHit
	if mode == SearchSmart {
		exact = exactHits(terms, candidates)
	}
	if mode == SearchSmart || mode == SearchContains {
		contains = containsHits(lowerQuery, candidates)
	}
	if mode == SearchSmart || mode == SearchFuzzy {
		fuzzy = fuzzyHits(terms, candidates, mode == SearchSmart)
	}
	if mode == SearchSmart || mode == SearchMeaning {
		floor, relative, limit := meaningFloor, meaningRelative, meaningLimit
		if mode == SearchSmart {
			floor, relative, limit = smartMeaningFloor, smartMeaningRelative, smartMeaningLimit
			if len(exact) > 0 || len(contains) > 0 {
				limit = smartMeaningLexicalLimit
			}
		}
		meaning = meaningHits(vectors, candidates, floor)
		byUpdated(meaning, index)
		meaning = meaningWithinTop(meaning, relative)
		if len(meaning) > limit {
			meaning = meaning[:limit]
		}
	}
	for _, leg := range [][]searchHit{exact, contains, fuzzy} {
		byUpdated(leg, index)
	}
	switch mode {
	case SearchContains:
		return contains
	case SearchFuzzy:
		return fuzzy
	case SearchMeaning:
		return meaning
	}
	fused := fuseReciprocalRank([][]searchHit{exact, contains, fuzzy, meaning})
	byUpdated(fused, index)
	return fused
}

// matchStrength orders reasons: a title match is stronger than text, text
// than a near spelling, and a near spelling than meaning alone.
var matchStrength = map[string]int{MatchTitle: 4, MatchText: 3, MatchFuzzy: 2, MatchMeaning: 1}

// fuseReciprocalRank merges ranked legs with reciprocal-rank fusion; each
// document keeps the strongest reason and that reason's snippet.
func fuseReciprocalRank(legs [][]searchHit) []searchHit {
	fused := map[string]*searchHit{}
	var order []string
	for _, leg := range legs {
		for rank, h := range leg {
			f, ok := fused[h.id]
			if !ok {
				f = &searchHit{id: h.id, match: h.match, snippet: h.snippet}
				fused[h.id] = f
				order = append(order, h.id)
			}
			f.score += 1 / float64(rrfK+rank+1)
			if matchStrength[h.match] > matchStrength[f.match] {
				f.match, f.snippet = h.match, h.snippet
			}
		}
	}
	out := make([]searchHit, 0, len(order))
	for _, id := range order {
		out = append(out, *fused[id])
	}
	return out
}

// exactHits matches documents where every query term is a word, or the
// start of a word, of the readable version; title matches rank first.
func exactHits(terms []string, candidates []searchCandidate) []searchHit {
	if len(terms) == 0 {
		return nil
	}
	var out []searchHit
	for _, c := range candidates {
		inTitle, inAny := true, true
		for _, term := range terms {
			t := hasPrefixWord(c.titleWords, term)
			inTitle = inTitle && t
			if !t && !hasPrefixWord(c.bodyWords, term) {
				inAny = false
				break
			}
		}
		if !inAny {
			continue
		}
		h := searchHit{id: c.id, match: MatchText, score: 1}
		if inTitle {
			h.match, h.score = MatchTitle, 2
		}
		h.snippet = snippetAround(c.display, wordPosition(c.lowerDisplay, terms[0]))
		out = append(out, h)
	}
	return out
}

// containsHits matches the query as a case-insensitive substring of the
// title or the plain text; earlier occurrences rank higher.
func containsHits(lowerQuery string, candidates []searchCandidate) []searchHit {
	if lowerQuery == "" {
		return nil
	}
	var out []searchHit
	for _, c := range candidates {
		bodyPos := indexLower(c.plain, c.lowerPlain, lowerQuery)
		snippet := snippetAround(c.display, indexLower(c.display, c.lowerDisplay, lowerQuery))
		if strings.Contains(c.lowerTitle, lowerQuery) {
			out = append(out, searchHit{id: c.id, match: MatchTitle, score: 2, snippet: snippet})
			continue
		}
		if bodyPos >= 0 {
			out = append(out, searchHit{id: c.id, match: MatchText, score: 1 - float64(bodyPos)/float64(len(c.plain)+1), snippet: snippet})
		}
	}
	return out
}

// fuzzyHits scores each document by the mean, over query words, of the
// best trigram similarity to any of its words; every query word must reach
// fuzzyThreshold. In smart mode a query word that is already the start of
// some candidate word only matches those words, so correctly spelled
// queries gain no near-miss noise and typos still find their word.
func fuzzyHits(terms []string, candidates []searchCandidate, strict bool) []searchHit {
	if len(terms) == 0 {
		return nil
	}
	exists := map[string]bool{}
	if strict {
		for _, term := range terms {
			for _, c := range candidates {
				if hasPrefixWord(c.titleWords, term) || hasPrefixWord(c.bodyWords, term) {
					exists[term] = true
					break
				}
			}
		}
	}
	grams := map[string]map[string]struct{}{}
	gramsOf := func(w string) map[string]struct{} {
		g, ok := grams[w]
		if !ok {
			g = trigrams(w)
			grams[w] = g
		}
		return g
	}
	type memoKey struct{ term, word string }
	memo := map[memoKey]float64{}
	sim := func(term, word string) float64 {
		if exists[term] {
			if strings.HasPrefix(word, term) {
				return 1
			}
			return 0
		}
		k := memoKey{term, word}
		v, ok := memo[k]
		if !ok {
			v = max(trigramSimilarity(gramsOf(term), gramsOf(word)), editSimilarity(term, word))
			if strings.HasPrefix(word, term) {
				v = 1
			}
			memo[k] = v
		}
		return v
	}
	var out []searchHit
	for _, c := range candidates {
		total, ok := 0.0, true
		bestWord := ""
		for i, term := range terms {
			best, word := 0.0, ""
			for _, w := range c.titleWords {
				if v := sim(term, w); v > best {
					best, word = v, w
				}
			}
			titleBest := best
			for _, w := range c.bodyWords {
				if v := sim(term, w); v > best {
					best, word = v, w
				}
			}
			// A near match in the title outranks the same match in the text.
			if titleBest >= fuzzyThreshold && titleBest >= best {
				total += fuzzyTitleBonus
			}
			if best < fuzzyThreshold {
				ok = false
				break
			}
			if i == 0 {
				bestWord = word
			}
			total += best
		}
		if !ok {
			continue
		}
		out = append(out, searchHit{id: c.id, match: MatchFuzzy, score: total / float64(len(terms)), snippet: snippetAround(c.display, wordPosition(c.lowerDisplay, bestWord))})
	}
	return out
}

// meaningWithinTop keeps the hits, sorted best first, whose score is at
// least relative times the best score.
func meaningWithinTop(hits []searchHit, relative float64) []searchHit {
	if len(hits) == 0 {
		return hits
	}
	cut := hits[0].score * relative
	for i, h := range hits {
		if h.score < cut {
			return hits[:i]
		}
	}
	return hits
}

// meaningHits keeps documents whose best section reaches floor; the
// snippet is that section's opening text.
func meaningHits(vectors map[string]sectionMatch, candidates []searchCandidate, floor float64) []searchHit {
	var out []searchHit
	for _, c := range candidates {
		best, ok := vectors[c.id]
		if !ok || best.cosine < floor {
			continue
		}
		snippet := ""
		for _, sec := range SplitSections(c.markdown) {
			if sec.BlockID == best.blockID {
				text := sec.Text
				if i := strings.IndexByte(text, '\n'); i >= 0 {
					text = text[i+1:]
				}
				snippet = snippetAround(snippetText("", text), 0)
				break
			}
		}
		if snippet == "" {
			snippet = snippetAround(c.display, 0)
		}
		out = append(out, searchHit{id: c.id, match: MatchMeaning, score: best.cosine, snippet: snippet})
	}
	return out
}

// sortHits applies a non-relevance sort to the matches; relevance keeps
// the ranked order. Every order ends with the document ID so pages are
// stable.
func sortHits(hits []searchHit, order string, index map[string]*searchCandidate, names map[string]string) {
	ownerKey := func(c *searchCandidate) string {
		if name, ok := names[c.owner]; ok && name != "" {
			return strings.ToLower(name)
		}
		return strings.ToLower(c.owner)
	}
	byOwner := func(dir int) func(a, b *searchCandidate) int {
		return func(a, b *searchCandidate) int {
			if c := strings.Compare(ownerKey(a), ownerKey(b)) * dir; c != 0 {
				return c
			}
			return b.updated.Compare(a.updated)
		}
	}
	less := map[string]func(a, b *searchCandidate) int{
		SortOwner:      byOwner(1),
		SortOwnerDesc:  byOwner(-1),
		SortUpdated:    func(a, b *searchCandidate) int { return b.updated.Compare(a.updated) },
		SortUpdatedAsc: func(a, b *searchCandidate) int { return a.updated.Compare(b.updated) },
		SortTitle:      func(a, b *searchCandidate) int { return strings.Compare(a.lowerTitle, b.lowerTitle) },
		SortTitleDesc:  func(a, b *searchCandidate) int { return strings.Compare(b.lowerTitle, a.lowerTitle) },
	}[order]
	if less == nil {
		return
	}
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := index[hits[i].id], index[hits[j].id]
		if c := less(a, b); c != 0 {
			return c < 0
		}
		return a.id < b.id
	})
}

// projectRows loads the listing projection for one page of hits, in hit
// order, re-applying the full selection so a row that stopped being
// readable mid-request is dropped rather than shown.
func projectRows(ctx context.Context, tx dbport.Tx, tenantID, actorID string, selection ListOptions, page []searchHit) ([]DocumentSummary, error) {
	ids := make([]string, 0, len(page))
	for _, h := range page {
		ids = append(ids, h.id)
	}
	args := append(listSelectionArgs(tenantID, actorID, selection, nil), ids)
	rows, err := tx.Query(ctx, listProjectionSQL+listSelectionSQL+` AND d.id=ANY($9::text[])`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := map[string]DocumentSummary{}
	for rows.Next() {
		d, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		found[d.ID] = d
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]DocumentSummary, 0, len(page))
	for _, h := range page {
		d, ok := found[h.id]
		if !ok {
			continue
		}
		d.Match, d.Snippet = h.match, h.snippet
		out = append(out, d)
	}
	return out, nil
}

// trigrams returns pg_trgm's trigram set for one lower-case word: the
// word padded with two spaces in front and one behind.
func trigrams(word string) map[string]struct{} {
	r := []rune("  " + word + " ")
	out := make(map[string]struct{}, len(r))
	for i := 0; i+3 <= len(r); i++ {
		out[string(r[i:i+3])] = struct{}{}
	}
	return out
}

// fuzzyMaxEdits allows one edit in words of four to seven characters and
// two from eight; shorter words must match by trigrams.
func fuzzyMaxEdits(n int) int {
	switch {
	case n >= 8:
		return 2
	case n >= 4:
		return 1
	}
	return 0
}

// editSimilarity is one minus the optimal-string-alignment distance over
// the longer word's length, or zero beyond fuzzyMaxEdits.
func editSimilarity(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	longest := max(len(ra), len(rb))
	limit := fuzzyMaxEdits(min(len(ra), len(rb)))
	if limit == 0 || longest-min(len(ra), len(rb)) > limit {
		return 0
	}
	prev2 := make([]int, len(rb)+1)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		rowMin := cur[0]
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				cur[j] = min(cur[j], prev2[j-2]+1)
			}
			rowMin = min(rowMin, cur[j])
		}
		if rowMin > limit {
			return 0
		}
		prev2, prev, cur = prev, cur, prev2
	}
	if d := prev[len(rb)]; d <= limit {
		return 1 - float64(d)/float64(longest)
	}
	return 0
}

// trigramSimilarity is pg_trgm's similarity: shared trigrams over all
// distinct trigrams.
func trigramSimilarity(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	shared := 0
	for g := range a {
		if _, ok := b[g]; ok {
			shared++
		}
	}
	return float64(shared) / float64(len(a)+len(b)-shared)
}

func uniqueWords(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, w := range lexTokenize(text) {
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}

func hasPrefixWord(words []string, term string) bool {
	for _, w := range words {
		if strings.HasPrefix(w, term) {
			return true
		}
	}
	return false
}

// wordPosition finds the first occurrence of word at a word start in the
// lower-cased text, or -1.
func wordPosition(lower, word string) int {
	if word == "" {
		return -1
	}
	for from := 0; from < len(lower); {
		i := strings.Index(lower[from:], word)
		if i < 0 {
			return -1
		}
		at := from + i
		if at == 0 || !isWordByte(lower[at-1]) {
			return at
		}
		from = at + len(word)
	}
	return -1
}

func isWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b >= 0x80
}

// indexLower finds lowerQuery in text through its lower-cased copy, and
// only trusts the offset when lower-casing kept byte lengths.
func indexLower(text, lower, lowerQuery string) int {
	i := strings.Index(lower, lowerQuery)
	if i >= 0 && len(lower) != len(text) {
		return 0
	}
	return i
}

var (
	mdImage     = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	mdLink      = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	mdStrong    = regexp.MustCompile(`(\*\*|__)(.+?)(\*\*|__)`)
	mdEmphasis  = regexp.MustCompile(`(^|[^\w*])[*_]([^*_\n]+)[*_]`)
	mdCode      = regexp.MustCompile("`([^`]*)`")
	mdLineStart = regexp.MustCompile(`^\s*(#{1,6}\s+|>\s*|[-*+]\s+|\d+[.)]\s+)`)
	mdRule      = regexp.MustCompile(`^\s*(\|?\s*:?-{3,}:?\s*)+\|?\s*$|^\s*([-*_]\s*){3,}$|^\s*` + "```")
	mdSpaces    = regexp.MustCompile(`\s+`)
)

// stripMarkdown turns Markdown into one line of plain text: headings, list
// and quote markers, rules and table pipes go, emphasis and code marks go,
// links and images keep only their label, and Mermaid diagram blocks are
// dropped.
func stripMarkdown(markdown string) string {
	var parts []string
	inDiagram := false
	for _, line := range strings.Split(markdown, "\n") {
		// Diagram sources (```mermaid fences) render as pictures, not text,
		// so they are neither searchable nor quotable.
		if fence := strings.TrimSpace(line); strings.HasPrefix(fence, "```") {
			inDiagram = !inDiagram && strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(fence, "```")), "mermaid")
			continue
		}
		if inDiagram || mdRule.MatchString(line) {
			continue
		}
		for {
			next := mdLineStart.ReplaceAllString(line, "")
			if next == line {
				break
			}
			line = next
		}
		line = strings.ReplaceAll(line, "|", " ")
		line = mdImage.ReplaceAllString(line, "$1")
		line = mdLink.ReplaceAllString(line, "$1")
		line = mdStrong.ReplaceAllString(line, "$2")
		line = mdEmphasis.ReplaceAllString(line, "$1$2")
		line = mdCode.ReplaceAllString(line, "$1")
		if strings.TrimSpace(line) != "" {
			parts = append(parts, line)
		}
	}
	return strings.TrimSpace(mdSpaces.ReplaceAllString(strings.Join(parts, " "), " "))
}

// plainBody is the version's plain text without its leading title heading.
func plainBody(title, markdown string) string {
	plain := stripMarkdown(markdown)
	if t := strings.TrimSpace(title); t != "" && strings.HasPrefix(plain, t) {
		plain = strings.TrimSpace(plain[len(t):])
	}
	return plain
}

// mdLeadLabel matches a metadata lead line ("**Owner:** … · **Last
// reviewed:** …"): a paragraph that opens with a bold label ending in a
// colon.
var mdLeadLabel = regexp.MustCompile(`^\s*(\*\*|__)[^*_\n]+:\s*(\*\*|__)`)

// mdHeading matches an ATX heading line.
var mdHeading = regexp.MustCompile(`^\s*#{1,6}\s+`)

// snippetText is the display text that search snippets and link previews
// are cut from (C-5). It is deliberately separate from stripMarkdown and
// PlainText, whose output is the coordinate space of stored comment
// anchors and must not change. Unlike them it keeps block boundaries (a
// heading, list item, table row or paragraph that ends without sentence
// punctuation gets a full stop, so "Scope" and "This policy applies…" no
// longer run together), and it drops the document's own title heading and
// the metadata lead line that follows it.
func snippetText(title, markdown string) string {
	var blocks, para []string
	opening := true // no content block emitted yet
	title = strings.TrimSpace(title)
	flush := func() {
		if len(para) == 0 {
			return
		}
		lead := opening && mdLeadLabel.MatchString(para[0])
		text := ""
		for _, line := range para {
			text += " " + snippetInline(line)
		}
		para = para[:0]
		text = strings.TrimSpace(mdSpaces.ReplaceAllString(text, " "))
		if text == "" || lead {
			return
		}
		blocks = append(blocks, snippetSentence(text))
		opening = false
	}
	inFence, inDiagram := false, false
	for _, line := range strings.Split(markdown, "\n") {
		if fence := strings.TrimSpace(line); strings.HasPrefix(fence, "```") {
			flush()
			if inFence {
				inFence, inDiagram = false, false
			} else {
				inFence = true
				inDiagram = strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(fence, "```")), "mermaid")
			}
			continue
		}
		if inDiagram {
			continue
		}
		if strings.TrimSpace(line) == "" || mdRule.MatchString(line) {
			flush()
			continue
		}
		if mdHeading.MatchString(line) {
			flush()
			text := strings.TrimSpace(snippetInline(line))
			if opening && len(blocks) == 0 && title != "" && strings.EqualFold(text, title) {
				continue
			}
			if text != "" {
				blocks = append(blocks, snippetSentence(text))
				opening = false
			}
			continue
		}
		if inFence || mdLineStart.MatchString(line) || strings.HasPrefix(strings.TrimSpace(line), "|") {
			// Code lines, list items, quotes and table rows are blocks of
			// their own.
			flush()
			para = append(para, line)
			flush()
			continue
		}
		para = append(para, line)
	}
	flush()
	text := strings.Join(blocks, " ")
	if title != "" && strings.HasPrefix(strings.ToLower(text), strings.ToLower(title)) {
		text = strings.TrimSpace(text[len(title):])
	}
	return text
}

// snippetInline strips one line's block markers and inline Markdown, the
// same way stripMarkdown does.
func snippetInline(line string) string {
	for {
		next := mdLineStart.ReplaceAllString(line, "")
		if next == line {
			break
		}
		line = next
	}
	line = strings.ReplaceAll(line, "|", " ")
	line = mdImage.ReplaceAllString(line, "$1")
	line = mdLink.ReplaceAllString(line, "$1")
	line = mdStrong.ReplaceAllString(line, "$2")
	line = mdEmphasis.ReplaceAllString(line, "$1$2")
	return mdCode.ReplaceAllString(line, "$1")
}

// snippetSentence ends a block with a full stop unless it already ends in
// sentence or separating punctuation.
func snippetSentence(text string) string {
	last, _ := utf8.DecodeLastRuneInString(text)
	if strings.ContainsRune(".!?…:;。؟؛\"'”’", last) {
		return text
	}
	return text + "."
}

// snippetAround returns about snippetWindow characters of text centered on
// byte offset pos (or from the start when pos is negative), cut at word
// boundaries and marked with ellipses where text was dropped.
func snippetAround(text string, pos int) string {
	if text == "" {
		return ""
	}
	if pos < 0 || pos > len(text) {
		pos = 0
	}
	start := max(0, pos-snippetWindow/2)
	end := min(len(text), start+snippetWindow)
	start = max(0, min(start, end-snippetWindow))
	for start > 0 && !utf8.RuneStart(text[start]) {
		start--
	}
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end++
	}
	if start > 0 {
		if i := strings.IndexByte(text[start:end], ' '); i >= 0 && start+i < pos {
			start += i + 1
		}
	}
	if end < len(text) {
		if i := strings.LastIndexByte(text[start:end], ' '); i > 0 && start+i > pos {
			end = start + i
		}
	}
	out := strings.TrimSpace(text[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(text) {
		// "employees.…" reads as a typo: the ellipsis replaces trailing
		// punctuation rather than following it (C-5).
		if trimmed := strings.TrimRight(out, " ,.;:"); trimmed != "" {
			out = trimmed
		}
		out += "…"
	}
	for len(out) > snippetMax {
		_, size := utf8.DecodeLastRuneInString(strings.TrimSuffix(out, "…"))
		out = strings.TrimSuffix(out, "…")
		out = out[:len(out)-size] + "…"
	}
	return out
}
