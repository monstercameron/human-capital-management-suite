package projectactivity

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	domain "github.com/monstercameron/human-capital-management-suite/internal/domains/projectactivity"
	htmlparser "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var ErrInvalidPlainText = errors.New("projectactivity application: text must be bounded safe rich text")

// PlainTextSanitizer accepts a deliberately small HTML formatting subset. It
// drops active content and every attribute except a validated link destination.
type PlainTextSanitizer struct{}

func NewPlainTextSanitizer() PlainTextSanitizer { return PlainTextSanitizer{} }

var _ domain.TextSanitizer = PlainTextSanitizer{}

var allowedFormatting = map[string]string{
	"p": "p", "br": "br", "strong": "strong", "b": "strong", "em": "em", "i": "em",
	"u": "u", "s": "s", "code": "code", "pre": "pre", "blockquote": "blockquote",
	"ul": "ul", "ol": "ol", "li": "li",
}

func (PlainTextSanitizer) Sanitize(_ context.Context, source string) (domain.SafeText, error) {
	if source == "" || len(source) > domain.MaxTextBytes || !utf8.ValidString(source) || strings.TrimSpace(source) == "" {
		return domain.SafeText{}, ErrInvalidPlainText
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(source, "\r\n", "\n"), "\r", "\n")
	for _, r := range normalized {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return domain.SafeText{}, ErrInvalidPlainText
		}
	}
	contextNode := &htmlparser.Node{Type: htmlparser.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := htmlparser.ParseFragment(strings.NewReader(normalized), contextNode)
	if err != nil {
		return domain.SafeText{}, ErrInvalidPlainText
	}
	var out strings.Builder
	handles := make([]string, 0, 4)
	seen := make(map[string]struct{})
	hasBlock := false
	for _, node := range nodes {
		if node.Type == htmlparser.ElementNode {
			switch strings.ToLower(node.Data) {
			case "p", "pre", "blockquote", "ul", "ol":
				hasBlock = true
			}
		}
		if err := renderSafeNode(&out, node, &handles, seen, false); err != nil {
			return domain.SafeText{}, err
		}
	}
	rendered := out.String()
	if strings.TrimSpace(rendered) == "" {
		return domain.SafeText{}, ErrInvalidPlainText
	}
	if !hasBlock {
		rendered = "<p>" + rendered + "</p>"
	}
	if len(rendered) == 0 || len(rendered) > domain.MaxTextBytes {
		return domain.SafeText{}, fmt.Errorf("%w: rendered preview exceeds %d bytes", ErrInvalidPlainText, domain.MaxTextBytes)
	}
	return domain.SafeText{Source: normalized, HTML: rendered, MentionHandles: handles}, nil
}

func renderSafeNode(out *strings.Builder, node *htmlparser.Node, handles *[]string, seen map[string]struct{}, insideLink bool) error {
	switch node.Type {
	case htmlparser.TextNode:
		writeMentionText(out, node.Data, handles, seen, insideLink)
		return nil
	case htmlparser.ElementNode:
		name := strings.ToLower(node.Data)
		// Never retain executable or non-visible subtrees, including their text.
		switch name {
		case "script", "style", "iframe", "object", "embed", "svg", "math", "template", "noscript":
			return nil
		}
		if name == "a" {
			href := safeHref(node)
			if href != "" {
				out.WriteString(`<a href="`)
				out.WriteString(htmlEscapeAttribute(href))
				out.WriteString(`">`)
				for child := node.FirstChild; child != nil; child = child.NextSibling {
					if err := renderSafeNode(out, child, handles, seen, true); err != nil {
						return err
					}
				}
				out.WriteString("</a>")
				return nil
			}
			// An unsafe link is retained only as escaped text.
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if err := renderSafeNode(out, child, handles, seen, insideLink); err != nil {
					return err
				}
			}
			return nil
		}
		tag, ok := allowedFormatting[name]
		if ok {
			out.WriteByte('<')
			out.WriteString(tag)
			out.WriteByte('>')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := renderSafeNode(out, child, handles, seen, insideLink); err != nil {
				return err
			}
		}
		if ok && tag != "br" {
			out.WriteString("</")
			out.WriteString(tag)
			out.WriteByte('>')
		}
	case htmlparser.DocumentNode, htmlparser.DoctypeNode, htmlparser.CommentNode:
		return nil
	default:
		return nil
	}
	return nil
}

func safeHref(node *htmlparser.Node) string {
	for _, attr := range node.Attr {
		if !strings.EqualFold(attr.Key, "href") {
			continue
		}
		value := strings.TrimSpace(attr.Val)
		if value == "" || strings.ContainsAny(value, "\\\r\n\t") {
			return ""
		}
		u, err := url.Parse(value)
		if err != nil || u.User != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || strings.HasPrefix(value, "//") {
			return ""
		}
		return value
	}
	return ""
}

func writeMentionText(out *strings.Builder, text string, handles *[]string, seen map[string]struct{}, insideLink bool) {
	// Escaping is performed by html.EscapeString via the standard serializer.
	// Mention tokens use @handle where handles contain 1..64 safe ASCII chars.
	for i := 0; i < len(text); {
		if text[i] != '@' || (i > 0 && isMentionByte(text[i-1])) {
			r, size := utf8.DecodeRuneInString(text[i:])
			if r == '\n' {
				out.WriteString("<br>")
			} else {
				out.WriteString(escapeText(string(r)))
			}
			i += size
			continue
		}
		j := i + 1
		for j < len(text) && isMentionByte(text[j]) && j-i <= 64 {
			j++
		}
		if j == i+1 || j-i > 65 || (j < len(text) && isMentionByte(text[j])) {
			out.WriteString("@")
			i++
			continue
		}
		handle := strings.ToLower(text[i+1 : j])
		if !insideLink {
			if _, ok := seen[handle]; !ok {
				if len(*handles) >= domain.MaxMentionHandles {
					*handles = append(*handles, handle) // domain boundary rejects before invoking the resolver
					return
				}
				seen[handle] = struct{}{}
				*handles = append(*handles, handle)
			}
		}
		out.WriteString("@")
		out.WriteString(escapeText(text[i+1 : j]))
		i = j
	}
}

func isMentionByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '-' || b == '.'
}

func escapeText(value string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;").Replace(value)
}

func htmlEscapeAttribute(value string) string { return escapeText(value) }
