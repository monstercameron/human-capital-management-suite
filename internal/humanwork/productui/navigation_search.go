package productui

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// navigationSearchScore ranks a menu item against business vocabulary from
// its label, stable route identity, localized description, and registry-owned
// aliases. Zero means the item is not relevant enough to show.
func navigationSearchScore(item NavItem, query string) int {
	tokens := strings.Fields(normalizeNavigationSearch(query))
	if len(tokens) == 0 {
		return 1
	}
	fields := []struct {
		text       string
		weight     int
		shortQuery bool
	}{
		{text: item.Label, weight: 32, shortQuery: true},
		{text: string(item.Page), weight: 20, shortQuery: true},
		{text: item.LabelKey, weight: 18},
		{text: item.Description, weight: 4},
	}
	// The registry supplies stable, non-user-authored metadata that remains
	// available even when a projection localizes the visible label. Searching
	// route/title vocabulary makes deep pages discoverable without treating a
	// fuzzy match as authorization.
	if definition, ok := LookupPage(item.Page); ok {
		for _, value := range []string{definition.Route, definition.Label, definition.Title, definition.LabelKey, definition.TitleKey, definition.SubtitleKey} {
			fields = append(fields, struct {
				text       string
				weight     int
				shortQuery bool
			}{text: value, weight: 16})
		}
	}
	for _, keyword := range item.Keywords {
		fields = append(fields, struct {
			text       string
			weight     int
			shortQuery bool
		}{text: keyword, weight: 24})
	}
	total := 0
	for _, token := range tokens {
		best := 0
		for _, field := range fields {
			if utf8.RuneCountInString(token) < 3 && !field.shortQuery {
				continue
			}
			if score := fuzzyFieldScore(field.text, token); score > 0 && score+field.weight > best {
				best = score + field.weight
			}
		}
		if best == 0 {
			return 0
		}
		total += best
	}
	return total
}

func fuzzyFieldScore(value, token string) int {
	value = normalizeNavigationSearch(value)
	if value == "" || token == "" {
		return 0
	}
	if value == token {
		return 140
	}
	best := 0
	for _, word := range strings.Fields(value) {
		score := fuzzyWordScore(word, token)
		if score > best {
			best = score
		}
	}
	if utf8.RuneCountInString(token) >= 3 && strings.Contains(value, token) && best < 88 {
		best = 88
	}
	return best
}

func fuzzyWordScore(word, token string) int {
	wordLength, tokenLength := utf8.RuneCountInString(word), utf8.RuneCountInString(token)
	if word == token {
		return 132
	}
	if tokenLength >= 2 && strings.HasPrefix(word, token) {
		return 120 - minInt(wordLength-tokenLength, 18)
	}
	// Very short fragments create noisy matches ("pe" in "appearance").
	// Require at least three characters before typo and subsequence matching.
	if tokenLength < 3 {
		return 0
	}
	if strings.Contains(word, token) {
		return 94 - minInt(strings.Index(word, token), 12)
	}
	allowedDistance := 1
	if tokenLength >= 6 {
		allowedDistance = 2
	}
	if distance := navigationEditDistance(word, token); distance <= allowedDistance {
		return 91 - distance*14
	}
	if gaps, ok := navigationSubsequenceGaps(word, token); ok {
		return 72 - minInt(gaps, 24)
	}
	return 0
}

func normalizeNavigationSearch(value string) string {
	var result strings.Builder
	space := true
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			result.WriteRune(r)
			space = false
			continue
		}
		if !space {
			result.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(result.String())
}

func navigationSubsequenceGaps(word, token string) (int, bool) {
	wordRunes, tokenRunes := []rune(word), []rune(token)
	matched, first, last := 0, -1, -1
	for index := 0; index < len(wordRunes) && matched < len(tokenRunes); index++ {
		if wordRunes[index] != tokenRunes[matched] {
			continue
		}
		if first < 0 {
			first = index
		}
		last = index
		matched++
	}
	if matched != len(tokenRunes) {
		return 0, false
	}
	return (last - first + 1) - len(tokenRunes) + first, true
}

func navigationEditDistance(left, right string) int {
	leftRunes, rightRunes := []rune(left), []rune(right)
	previous := make([]int, len(rightRunes)+1)
	for index := range previous {
		previous[index] = index
	}
	for leftIndex := 1; leftIndex <= len(leftRunes); leftIndex++ {
		current := make([]int, len(rightRunes)+1)
		current[0] = leftIndex
		for rightIndex := 1; rightIndex <= len(rightRunes); rightIndex++ {
			cost := 0
			if leftRunes[leftIndex-1] != rightRunes[rightIndex-1] {
				cost = 1
			}
			current[rightIndex] = minInt(
				minInt(current[rightIndex-1]+1, previous[rightIndex]+1),
				previous[rightIndex-1]+cost,
			)
		}
		previous = current
	}
	return previous[len(rightRunes)]
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
