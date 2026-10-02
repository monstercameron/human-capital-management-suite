package chatrewrite

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Longer protected spans take precedence over tokens nested inside them.
func protectedPattern() *regexp.Regexp {
	return regexp.MustCompile("(?m)```[\\s\\S]*?```|~~~[\\s\\S]*?~~~|``[^`]+``|`[^`\\n]+`|^>[ \\t]?.*$|\\\"[^\\\"\\n]+\\\"|(?:^|[^\\p{L}\\p{N}])'[^'\\n]+'|“[^”\\n]+”|‘[^’\\n]+’|\\[[^]\\n]*\\]\\([^ )\\n]+\\)|(?:https?://|mailto:|www\\.)[^\\s<>]+|<@[^>]+>|@[\\p{L}\\p{N}_][\\p{L}\\p{N}_.-]*|[$€£]?[-+]?[\\p{N}]+(?:[-/:.,][\\p{N}]+)*(?:%|[ \\t]?(?:USD|EUR|GBP))?")
}

type protectedDraft struct {
	text           string
	tokens, values []string
}

func protect(draft string) (protectedDraft, error) {
	if strings.Contains(draft, "⟦HCM:") || strings.Contains(draft, "⟧") {
		return protectedDraft{}, ErrInvalid
	}
	// The nonce is letters only. A hex nonce put long runs of digits in front of
	// the outbound verifier, which now and then read one as a card number and
	// refused a draft that held nothing sensitive (about one in three hundred).
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return protectedDraft{}, ErrUnavailable
	}
	nonce := make([]byte, len(random))
	for i, b := range random {
		nonce[i] = 'a' + b%26
	}
	prefix := "⟦HCM:" + string(nonce) + ":"
	p := protectedDraft{}
	p.text = protectedPattern().ReplaceAllStringFunc(draft, func(value string) string {
		token := fmt.Sprintf("%s%d⟧", prefix, len(p.values))
		p.values = append(p.values, value)
		p.tokens = append(p.tokens, token)
		return token
	})
	return p, nil
}
func (p protectedDraft) restore(output string) (string, error) {
	for _, token := range p.tokens {
		if strings.Count(output, token) != 1 {
			return "", ErrPreservation
		}
	}
	for i, token := range p.tokens {
		output = strings.Replace(output, token, p.values[i], 1)
	}
	if strings.Contains(output, "⟦HCM:") || strings.Contains(output, "⟧") {
		return "", ErrPreservation
	}
	// Refuse added or changed literal tokens as well as lost placeholders.
	counts := map[string]int{}
	for _, value := range p.values {
		counts[value]++
	}
	for _, value := range protectedPattern().FindAllString(output, -1) {
		counts[value]--
	}
	for _, count := range counts {
		if count != 0 {
			return "", ErrPreservation
		}
	}
	return output, nil
}

func boundedContext(messages []string) []string {
	if len(messages) > MaxContextMessages {
		messages = messages[len(messages)-MaxContextMessages:]
	}
	result := make([]string, 0, len(messages))
	remaining := MaxContextBytes
	for _, text := range messages {
		if remaining <= 0 {
			break
		}
		limit := 512
		if remaining < limit {
			limit = remaining
		}
		text = truncateUTF8(text, limit)
		result = append(result, text)
		remaining -= len(text)
	}
	return result
}
func truncateUTF8(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	end := 0
	for pos := range s {
		if pos > limit {
			break
		}
		end = pos
	}
	return s[:end]
}
func promptData(draft string, context []string) string {
	// JSON escaping prevents user text from closing the data delimiter, including injected roles.
	data, _ := json.Marshal(struct {
		Draft           string   `json:"draft"`
		RegisterContext []string `json:"register_context"`
	}{draft, boundedContext(context)})
	return "<untrusted_data>\n" + string(data) + "\n</untrusted_data>"
}
