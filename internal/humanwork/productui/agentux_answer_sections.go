package productui

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Section addresses match the heading-derived anchors returned by Documents,
// including duplicate headings and headings longer than the outline label.
func agentAnswerSectionAnchors(markdown string) []string {
	seen := map[string]int{}
	var anchors []string
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		level := len(trimmed) - len(strings.TrimLeft(trimmed, "#"))
		if level < 1 || level > 6 || len(trimmed) <= level || trimmed[level] != ' ' {
			continue
		}
		heading := strings.TrimSpace(trimmed[level:])
		if heading == "" {
			continue
		}
		var slug strings.Builder
		dash := true
		for _, char := range strings.ToLower(heading) {
			if unicode.IsLetter(char) || unicode.IsDigit(char) {
				slug.WriteRune(char)
				dash = false
			} else if !dash {
				slug.WriteByte('-')
				dash = true
			}
		}
		base := strings.Trim(slug.String(), "-")
		if base == "" {
			base = "section"
		}
		seen[base]++
		anchor := base
		if seen[base] > 1 {
			anchor += "-" + strconv.Itoa(seen[base])
		}
		anchors = append(anchors, anchor)
	}
	return anchors
}

func agentAnswerBindDocumentSections(root ast.Node, source []byte, original string) {
	prefix := ""
	if offset := strings.LastIndex(original, string(source)); offset >= 0 {
		prefix = original[:offset]
	}
	_ = ast.Walk(root, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if heading, ok := node.(*ast.Heading); ok && entering && heading.Lines().Len() > 0 {
			anchors := agentAnswerSectionAnchors(prefix + string(source[:heading.Lines().At(0).Stop]))
			if len(anchors) > 0 {
				heading.SetAttributeString("agentux-section", anchors[len(anchors)-1])
			}
		}
		return ast.WalkContinue, nil
	})
}

func agentAnswerDocumentHasSection(markdown, anchor string) bool {
	if len(markdown) > 64*1024 {
		return false
	}
	source := []byte(markdown)
	root := docsMarkdownParser.Parse(text.NewReader(source))
	agentAnswerBindDocumentSections(root, source, markdown)
	found := false
	_ = ast.Walk(root, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if heading, ok := node.(*ast.Heading); ok && entering {
			section, _ := heading.AttributeString("agentux-section")
			if section == strings.TrimPrefix(anchor, "sec-") {
				found = true
				return ast.WalkStop, nil
			}
		}
		return ast.WalkContinue, nil
	})
	return found
}
