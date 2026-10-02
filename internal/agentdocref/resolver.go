package agentdocref

import (
	"context"
	"strings"
	"unicode/utf8"
)

// Invoker identifies the human whose current document access governs a
// resolution. It deliberately carries no agent, author, or system identity.
type Invoker struct {
	TenantID  string
	SubjectID string
}

type ResolvedDocument struct {
	Reference Reference `json:"reference"`
	Version   uint64    `json:"version"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Truncated bool      `json:"truncated"`
}
type Omission struct {
	Label  string `json:"label"`
	Reason string `json:"reason"`
}

const (
	NotReadable  = "NOT_READABLE"
	NotFound     = "NOT_FOUND"
	NotPublished = "NOT_PUBLISHED"
	OverBudget   = "OVER_BUDGET"
)

// Resolver reads document references under the invoking human's current
// document access. Implementations omit inaccessible references rather than
// returning partial content or resolving them under another principal.
type Resolver interface {
	Resolve(context.Context, Invoker, []Reference) (resolved []ResolvedDocument, omitted []Omission, err error)
}

// ApplyBudget returns an independent, ordered copy of the documents whose
// content fits within maxCharacters Unicode code points. When only a prefix of
// a document fits, the prefix ends at a Markdown section boundary and the
// document is marked truncated. A document for which no complete section fits,
// and every document after it, is omitted from the returned slice.
func ApplyBudget(resolved []ResolvedDocument, maxCharacters int) []ResolvedDocument {
	if maxCharacters <= 0 || len(resolved) == 0 {
		return nil
	}
	remaining := maxCharacters
	budgeted := make([]ResolvedDocument, 0, len(resolved))
	for _, document := range resolved {
		contentRunes := utf8.RuneCountInString(document.Content)
		if contentRunes <= remaining {
			document.Content = strings.Clone(document.Content)
			budgeted = append(budgeted, document)
			remaining -= contentRunes
			continue
		}
		prefix := completeSectionPrefix(document.Content, remaining)
		if prefix != "" {
			document.Content = prefix
			document.Truncated = true
			budgeted = append(budgeted, document)
		}
		break
	}
	return budgeted
}

func completeSectionPrefix(content string, budget int) string {
	if budget <= 0 {
		return ""
	}
	sections := markdownSections(content)
	used := 0
	count := 0
	for _, section := range sections {
		length := utf8.RuneCountInString(section)
		if used+length > budget {
			break
		}
		used += length
		count++
	}
	if count == 0 {
		return ""
	}
	return strings.Join(sections[:count], "")
}

// markdownSections treats a heading and everything through the next heading
// as one indivisible budget unit. Heading-free content is one unit, which
// prevents a character cut from silently changing the meaning of a section.
func markdownSections(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.SplitAfter(content, "\n")
	sections := make([]string, 0, len(lines))
	start := 0
	for i, line := range lines {
		if i == 0 || !markdownHeading(line) {
			continue
		}
		sections = append(sections, strings.Join(lines[start:i], ""))
		start = i
	}
	sections = append(sections, strings.Join(lines[start:], ""))
	return sections
}

func markdownHeading(line string) bool {
	trimmed := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	level := 0
	for level < len(trimmed) && level < 6 && trimmed[level] == '#' {
		level++
	}
	return level > 0 && level < len(trimmed) && trimmed[level] == ' '
}
