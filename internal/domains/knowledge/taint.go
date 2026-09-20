// Shared instruction-taint detection for the knowledge gates.
//
// Both PublishChunk (KNOW-004) and EvaluateAnswerFreshness (KNOW-006) screen
// content through one injectable detector. The default,
// DetectInstructionTaint, reuses the reviewed agentsecurity contract
// (agentsecurity.DefaultInstructionDetector) on normalized text and extends
// it with the RAG-specific markers the chunk gate always refused, so the two
// gates can no longer disagree about what counts as hostile.
//
// Normalization closes the whitespace, case and obfuscation gaps in code:
// every whitespace, control and format rune (spaces, tabs, newlines,
// zero-width shapes) folds to a single separator, text is lowercased, and
// Latin diacritics fold to ASCII, so "Ignore  previous\ninstructions" and
// "ignorez les instructions précédentes" match exactly like their canonical
// forms. Detection stays substring evidence, never an LLM judgment, and the
// gate is kernel-pure: no database, no clock, no network.
package knowledge

import (
	"strings"
	"unicode"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
)

// InstructionDetector is the injectable instruction-taint contract shared by
// the knowledge gates. It is an alias for the agentsecurity detector shape,
// so any agentsecurity-compatible detector plugs into either gate.
type InstructionDetector = agentsecurity.Detector

// knowledgeMarkers extends the agentsecurity contract with the RAG-specific
// shapes the chunk gate always refused, plus the French marker. Matching
// always runs on normalized text (see NormalizeInstructionText), never on
// raw input.
var knowledgeMarkers = []string{
	"ignore previous instructions",
	"disregard policy",
	"disregard previous",
	"system:",
	"exfiltrate",
	"forward this",
	"send this answer to",
	"bypass review",
	"ignorez les instructions precedentes",
}

// latinFold maps a lowercase Latin letter with diacritics to its ASCII base.
// ToLower runs first, so only lowercase entries are needed.
func latinFold(r rune) (string, bool) {
	switch r {
	case 'à', 'á', 'â', 'ã', 'ä', 'å':
		return "a", true
	case 'æ':
		return "ae", true
	case 'ç':
		return "c", true
	case 'è', 'é', 'ê', 'ë':
		return "e", true
	case 'ì', 'í', 'î', 'ï':
		return "i", true
	case 'ñ':
		return "n", true
	case 'ò', 'ó', 'ô', 'õ', 'ö':
		return "o", true
	case 'œ':
		return "oe", true
	case 'ù', 'ú', 'û', 'ü':
		return "u", true
	case 'ý', 'ÿ':
		return "y", true
	case 'ß':
		return "ss", true
	}
	return "", false
}

// NormalizeInstructionText folds content to its canonical detection form:
// lowercase, diacritics folded to ASCII, and every whitespace, control and
// format rune mapped to a single blank separator. Two inputs that differ
// only in case, spacing, line breaks or zero-width obfuscation normalize
// identically, so a marker hidden behind them still matches.
func NormalizeInstructionText(s string) string {
	lowered := strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(lowered))
	for _, r := range lowered {
		if folded, ok := latinFold(r); ok {
			b.WriteString(folded)
			continue
		}
		if unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// DetectInstructionTaint is the shared default detector for both knowledge
// gates. It normalizes the content, runs the reviewed agentsecurity
// contract over it, then matches the RAG-specific markers over the same
// normalized form. A detector failure from the wrapped contract is
// surfaced, never swallowed: both gates fail closed on it.
func DetectInstructionTaint(content string) (bool, error) {
	normalized := NormalizeInstructionText(content)
	if normalized == "" {
		return false, nil
	}
	hit, err := agentsecurity.DefaultInstructionDetector(normalized)
	if err != nil {
		return false, err
	}
	if hit {
		return true, nil
	}
	for _, marker := range knowledgeMarkers {
		if strings.Contains(normalized, marker) {
			return true, nil
		}
	}
	return false, nil
}

// defaultDetector substitutes the shared default for a nil injectable, so
// the primary entry points keep their signatures while the WithDetector
// variants stay honestly injectable.
func defaultDetector(detect InstructionDetector) InstructionDetector {
	if detect == nil {
		return DetectInstructionTaint
	}
	return detect
}
