package productui

import "strings"

// uxblindOSearchField is the small common vocabulary used by the two shell
// search surfaces. Keeping the scoring here prevents global search and the
// action launcher from disagreeing about whether a name is more useful than
// an incidental substring in a description.
type uxblindOSearchField struct {
	value  string
	weight int
}

func uxblindOSearchScore(fields []uxblindOSearchField, tokens []string, label string) int {
	if len(fields) == 0 || len(tokens) == 0 {
		return 0
	}
	total := 0
	for _, token := range tokens {
		best := 0
		for _, field := range fields {
			if score := uxblindOSearchFieldScore(field.value, token); score > 0 && score+field.weight > best {
				best = score + field.weight
			}
		}
		if best == 0 {
			return 0
		}
		total += best
	}
	// An exact label is the strongest possible intent: it must beat a result
	// that only happens to contain the query in another field, regardless of
	// input ordering or the item's kind.
	if normalizeNavigationSearch(label) == strings.Join(tokens, " ") {
		total += 1_000_000
	}
	return total
}

func uxblindOSearchFieldScore(value, token string) int {
	value = normalizeNavigationSearch(value)
	token = normalizeNavigationSearch(token)
	if value == "" || token == "" {
		return 0
	}
	if value == token {
		return 10_000
	}
	for _, word := range strings.Fields(value) {
		if word == token {
			return 9_000
		}
		if len([]rune(token)) >= 2 && strings.HasPrefix(word, token) {
			return 8_000 - minInt(len([]rune(word))-len([]rune(token)), 100)
		}
	}
	if strings.HasPrefix(value, token) {
		return 7_000
	}
	if len([]rune(token)) >= 3 && strings.Contains(value, token) {
		return 3_000
	}
	if fuzzy := fuzzyNormalizedFieldScore(value, token); fuzzy > 0 {
		return 100 + fuzzy
	}
	return 0
}
