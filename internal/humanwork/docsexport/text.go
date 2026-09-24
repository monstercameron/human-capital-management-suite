// Package docsexport turns a document's Markdown into the files the Docs
// "Export" menu offers: plain text, Markdown with its attachment
// references made absolute, and PDF. It is pure: no storage, no network.
package docsexport

import (
	stdhtml "html"
	"regexp"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// markdownParser reads the same profile as the Docs reader: CommonMark plus
// GFM tables, strikethrough and task lists.
var markdownParser parser.Parser = goldmark.New(goldmark.WithExtensions(extension.Table, extension.Strikethrough, extension.TaskList)).Parser()

// AttachmentScheme prefixes an attachment reference in document Markdown.
const AttachmentScheme = "attachment:"

var attachmentRef = regexp.MustCompile(`(\]\(\s*<?)` + AttachmentScheme + `([A-Za-z0-9_-]{1,80})`)

// Markdown returns the source with every attachment reference in a link or
// image destination rewritten by resolve (to an absolute download address).
func Markdown(markdown string, resolve func(id string) string) string {
	if resolve == nil {
		return markdown
	}
	return attachmentRef.ReplaceAllStringFunc(markdown, func(match string) string {
		parts := attachmentRef.FindStringSubmatch(match)
		return parts[1] + resolve(parts[2])
	})
}

// AttachmentIDs lists the attachment ids a document references, in order,
// without repeats.
func AttachmentIDs(markdown string) []string {
	seen := map[string]bool{}
	var out []string
	for _, match := range attachmentRef.FindAllStringSubmatch(markdown, -1) {
		if !seen[match[2]] {
			seen[match[2]] = true
			out = append(out, match[2])
		}
	}
	return out
}

// PlainText strips the Markdown: headings and paragraphs become lines of
// text separated by blank lines, lists keep a "-" or "1." marker, quotes a
// "> ", code blocks an indent, tables tab-separated cells, links their
// text with an outside address in parentheses, and images their alt text.
func PlainText(markdown string) string {
	source := []byte(markdown)
	root := markdownParser.Parse(text.NewReader(source))
	var b strings.Builder
	writeBlocks(&b, root, source, "")
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func writeBlocks(b *strings.Builder, parent ast.Node, source []byte, prefix string) {
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		writeBlock(b, child, source, prefix)
	}
}

func writeLines(b *strings.Builder, prefix, value string) {
	for _, line := range strings.Split(value, "\n") {
		b.WriteString(strings.TrimRight(prefix+line, " "))
		b.WriteString("\n")
	}
}

func writeBlock(b *strings.Builder, node ast.Node, source []byte, prefix string) {
	switch n := node.(type) {
	case *ast.Heading, *ast.Paragraph:
		writeLines(b, prefix, plainInline(n, source))
		b.WriteString(strings.TrimRight(prefix, " ") + "\n")
	case *ast.TextBlock:
		writeLines(b, prefix, plainInline(n, source))
	case *ast.List:
		number := n.Start
		if number == 0 {
			number = 1
		}
		for item := n.FirstChild(); item != nil; item = item.NextSibling() {
			marker := "- "
			if n.IsOrdered() {
				marker = strconv.Itoa(number) + ". "
				number++
			}
			var inner strings.Builder
			writeBlocks(&inner, item, source, "")
			lines := strings.Split(strings.TrimRight(inner.String(), "\n"), "\n")
			for i, line := range lines {
				if i == 0 {
					b.WriteString(prefix + marker + line + "\n")
				} else if strings.TrimSpace(line) == "" {
					continue
				} else {
					b.WriteString(prefix + strings.Repeat(" ", len(marker)) + line + "\n")
				}
			}
		}
		b.WriteString(strings.TrimRight(prefix, " ") + "\n")
	case *ast.Blockquote:
		var inner strings.Builder
		writeBlocks(&inner, n, source, prefix+"> ")
		// Drop the quote's own trailing blank line so it ends like any
		// other block, with one empty line after it.
		marker := strings.TrimRight(prefix+"> ", " ")
		quoted := strings.TrimRight(inner.String(), "\n")
		for strings.HasSuffix(quoted, "\n"+marker) {
			quoted = strings.TrimRight(strings.TrimSuffix(quoted, marker), "\n")
		}
		b.WriteString(quoted + "\n" + strings.TrimRight(prefix, " ") + "\n")
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		writeLines(b, prefix+"    ", blockLines(n, source))
		b.WriteString(strings.TrimRight(prefix, " ") + "\n")
	case *east.Table:
		for row := n.FirstChild(); row != nil; row = row.NextSibling() {
			var cells []string
			for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
				cells = append(cells, plainInline(cell, source))
			}
			b.WriteString(prefix + strings.Join(cells, "\t") + "\n")
		}
		b.WriteString(strings.TrimRight(prefix, " ") + "\n")
	case *ast.ThematicBreak:
		b.WriteString(prefix + "----\n\n")
	case *ast.HTMLBlock:
		writeLines(b, prefix, strings.TrimRight(blockLines(n, source), "\n"))
		b.WriteString("\n")
	default:
		writeBlocks(b, n, source, prefix)
	}
}

// plainInline is a block's inline text with its formatting removed.
func plainInline(parent ast.Node, source []byte) string {
	var b strings.Builder
	var walk func(n ast.Node)
	walk = func(n ast.Node) {
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			switch c := child.(type) {
			case *ast.Text:
				b.WriteString(unescape(c, source))
				if c.HardLineBreak() {
					b.WriteString("\n")
				} else if c.SoftLineBreak() {
					b.WriteString(" ")
				}
			case *ast.String:
				b.WriteString(string(c.Value))
			case *ast.CodeSpan:
				b.WriteString(inlineText(c, source))
			case *ast.Link:
				label := inlineText(c, source)
				walk(c)
				dest := string(c.Destination)
				if (strings.HasPrefix(dest, "http://") || strings.HasPrefix(dest, "https://")) && dest != label {
					b.WriteString(" (" + dest + ")")
				}
			case *ast.Image:
				alt := inlineText(c, source)
				if alt == "" {
					alt = "image"
				}
				b.WriteString("[" + alt + "]")
			case *ast.AutoLink:
				b.WriteString(string(c.URL(source)))
			case *east.TaskCheckBox:
				if c.IsChecked {
					b.WriteString("[x] ")
				} else {
					b.WriteString("[ ] ")
				}
			case *ast.RawHTML:
				b.WriteString(string(c.Segments.Value(source)))
			default:
				walk(c)
			}
		}
	}
	walk(parent)
	return strings.TrimSpace(b.String())
}

func unescape(node *ast.Text, source []byte) string {
	value := string(node.Value(source))
	if node.IsRaw() {
		return value
	}
	return stdhtml.UnescapeString(string(util.UnescapePunctuations([]byte(value))))
}

// inlineText is the plain text under a node.
func inlineText(node ast.Node, source []byte) string {
	var b strings.Builder
	_ = ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch c := n.(type) {
		case *ast.Text:
			b.WriteString(unescape(c, source))
			if c.SoftLineBreak() || c.HardLineBreak() {
				b.WriteString(" ")
			}
		case *ast.String:
			b.WriteString(string(c.Value))
		}
		return ast.WalkContinue, nil
	})
	return strings.TrimSpace(b.String())
}

// blockLines is a code or HTML block's text with its line breaks.
func blockLines(node ast.Node, source []byte) string {
	var b strings.Builder
	lines := node.Lines()
	for i := 0; i < lines.Len(); i++ {
		segment := lines.At(i)
		b.Write(segment.Value(source))
	}
	return strings.TrimRight(b.String(), "\n")
}
