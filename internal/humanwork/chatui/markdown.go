package chatui

import (
	stdhtml "html"
	"net/url"
	"strings"
	"unicode"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/util"
)

// markdownParser is built once: constructing goldmark's default parser per
// message was 15% of a timeline render's allocations. Parse keeps its state in
// a per-call context, so one parser serves every message.
var markdownParser = goldmark.New(goldmark.WithExtensions(extension.Table, extension.Strikethrough)).Parser()

// markdownMessageBody renders the supported Markdown profile as typed nodes.
// Raw HTML and image syntax never become active HTML or remote requests.
func markdownMessageBody(m Model, body string) []ui.Node {
	// CHATBUG-089: whatever body reaches here, the system's comments are not text.
	body = chatStripInternalMarkers(body)
	if len(body) > 64*1024 {
		return []ui.Node{ui.Text(body)}
	}
	// The browser client parses each distinct body once (chatperf_markdown.go).
	root, source := chatperfMarkdownTreeOf(body)
	return markdownChildren(m, root, source)
}

func markdownChildren(m Model, parent ast.Node, source []byte) []ui.Node {
	var nodes []ui.Node
	var plain strings.Builder
	flush := func() {
		if plain.Len() > 0 {
			nodes = append(nodes, markdownTextNodes(m, plain.String())...)
			plain.Reset()
		}
	}
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		if n, ok := child.(*ast.Text); ok {
			value := string(n.Value(source))
			if !n.IsRaw() {
				value = markdownUnescape(value)
			}
			plain.WriteString(value)
			if n.HardLineBreak() || n.SoftLineBreak() {
				flush()
				nodes = append(nodes, html.Br(html.Props{}))
			}
			continue
		}
		flush()
		nodes = append(nodes, markdownNode(m, child, source)...)
	}
	flush()
	return nodes
}

// markdownUnescape resolves backslash escapes and character references in a
// run of message text. A reference is "&name;", "&#38;" or "&#x26;", with its
// semicolon, as CommonMark has it. The HTML rule that html.UnescapeString
// follows also accepts the old names without one, which turned the address
// "?a=1&copy=2" into "?a=1©=2".
func markdownUnescape(value string) string {
	value = string(util.UnescapePunctuations([]byte(value)))
	if !strings.Contains(value, "&") {
		return value
	}
	var out strings.Builder
	for {
		at := strings.IndexByte(value, '&')
		if at < 0 {
			break
		}
		out.WriteString(value[:at])
		value = value[at:]
		end := markdownReferenceEnd(value)
		if end == 0 {
			out.WriteByte('&')
			value = value[1:]
			continue
		}
		out.WriteString(stdhtml.UnescapeString(value[:end]))
		value = value[end:]
	}
	out.WriteString(value)
	return out.String()
}

// markdownReferenceEnd is the length of the character reference s begins
// with, semicolon included, or 0 when s (which starts with "&") begins with
// none.
func markdownReferenceEnd(s string) int {
	i := 1
	if i < len(s) && s[i] == '#' {
		i++
		if i < len(s) && (s[i] == 'x' || s[i] == 'X') {
			i++
		}
	}
	start := i
	for i < len(s) && i-start < 32 && (s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z') {
		i++
	}
	if i == start || i >= len(s) || s[i] != ';' {
		return 0
	}
	return i + 1
}

func markdownNode(m Model, node ast.Node, source []byte) []ui.Node {
	children := func() []ui.Node { return markdownChildren(m, node, source) }
	switch n := node.(type) {
	case *ast.Paragraph:
		return []ui.Node{html.P(markdownBlockProps(m), children()...)}
	case *ast.TextBlock:
		return markdownIsolate(m, children())
	case *ast.Heading:
		level := n.Level + 2
		if level > 6 {
			level = 6
		}
		return []ui.Node{html.Tag("h"+string(rune('0'+level)), markdownBlockProps(m), children()...)}
	case *ast.Emphasis:
		if n.Level == 2 {
			return []ui.Node{html.Strong(html.Props{}, children()...)}
		}
		return []ui.Node{html.Em(html.Props{}, children()...)}
	case *ast.String:
		return modAuthorInline(m, string(n.Value))
	case *ast.CodeSpan:
		return []ui.Node{html.Code(html.Props{}, ui.Text(string(n.Text(source))))}
	case *ast.CodeBlock, *ast.FencedCodeBlock:
		return []ui.Node{html.Pre(html.Props{}, html.Code(html.Props{}, ui.Text(string(node.Text(source)))))}
	case *ast.List:
		if n.IsOrdered() {
			props := html.Props{}
			if n.Start > 1 {
				props.Raw = map[string]any{"start": n.Start}
			}
			return []ui.Node{html.Ol(props, children()...)}
		}
		return []ui.Node{html.Ul(html.Props{}, children()...)}
	case *ast.ListItem:
		return []ui.Node{html.Li(html.Props{}, children()...)}
	case *ast.Blockquote:
		return []ui.Node{html.Blockquote(html.Props{}, children()...)}
	case *ast.ThematicBreak:
		return []ui.Node{html.Hr(html.Props{})}
	case *ast.Link:
		href, ok := safeMarkdownHref(string(n.Destination))
		// An address inside the label of a link is the label, not a second link.
		m.renderInLink = m.renderInLink || ok
		label := children()
		if ok {
			return []ui.Node{html.A(agentUX075LinkProps(html.Props{Href: href}, string(n.Title), markdownUnescape(string(n.Text(source)))), label...)}
		}
		return label
	case *ast.AutoLink:
		label := string(n.Label(source))
		if href, ok := safeMarkdownHref(string(n.URL(source))); ok {
			// CHATBUG-053: "<https://...>" is shortened like an address typed bare.
			if href == label && !m.renderInLink && chatbug053Address.FindString(href) == href {
				return []ui.Node{chatbug053Link(m, href)}
			}
			return []ui.Node{html.A(html.Props{Href: href}, ui.Text(label))}
		}
		return []ui.Node{ui.Text(label)}
	case *ast.Image:
		return children()
	case *ast.RawHTML:
		return []ui.Node{ui.Text(string(n.Text(source)))}
	case *ast.HTMLBlock:
		return []ui.Node{html.P(html.Props{}, ui.Text(string(n.Text(source))))}
	default:
		if nodes, drawn := agentUX075MarkdownNode(m, node, source); drawn {
			return nodes
		}
		return children()
	}
}

func safeMarkdownHref(raw string) (string, bool) {
	if raw == "" || strings.ContainsRune(raw, '\\') {
		return "", false
	}
	for _, r := range raw {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil {
		return "", false
	}
	if u.IsAbs() {
		return raw, (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
	}
	if u.Host != "" || u.Opaque != "" || strings.HasPrefix(raw, "//") {
		return "", false
	}
	if strings.HasPrefix(raw, "#") {
		return raw, true
	}
	if !strings.HasPrefix(raw, "/") || strings.Contains(strings.ToLower(raw), "%2f") || strings.Contains(strings.ToLower(raw), "%5c") {
		return "", false
	}
	return raw, true
}

// markdownTextNodes is a run of plain message text. A flag in it that the
// platform cannot draw becomes a labelled chip (CHATBUG-043); everything else is
// the text it always was.
func markdownTextNodes(m Model, text string) []ui.Node {
	if m.renderHighlight != "" {
		words := m.renderHighlight
		m.renderHighlight = ""
		return chatsearchHighlightNodes(words, markdownTextNodes(m, text))
	}
	if emojiFlagsDrawn() {
		return modAuthorInline(m, text)
	}
	segments := emojiSplitFlags(text)
	if segments == nil {
		return modAuthorInline(m, text)
	}
	var nodes []ui.Node
	for _, segment := range segments {
		if segment.Flag {
			nodes = append(nodes, emojiFlagChip(m, segment.Text))
		} else {
			nodes = append(nodes, modAuthorInline(m, segment.Text)...)
		}
	}
	return nodes
}
