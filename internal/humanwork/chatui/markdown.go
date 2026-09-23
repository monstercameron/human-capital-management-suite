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
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// markdownMessageBody renders the supported Markdown profile as typed nodes.
// Raw HTML and image syntax never become active HTML or remote requests.
func markdownMessageBody(m Model, body string) []ui.Node {
	if len(body) > 64*1024 {
		return []ui.Node{ui.Text(body)}
	}
	source := []byte(body)
	root := goldmark.New().Parser().Parse(text.NewReader(source))
	return markdownChildren(m, root, source)
}

func markdownChildren(m Model, parent ast.Node, source []byte) []ui.Node {
	var nodes []ui.Node
	var plain strings.Builder
	flush := func() {
		if plain.Len() > 0 {
			nodes = append(nodes, channelReferenceBody(m, plain.String())...)
			plain.Reset()
		}
	}
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		if n, ok := child.(*ast.Text); ok {
			value := string(n.Value(source))
			if !n.IsRaw() {
				value = stdhtml.UnescapeString(string(util.UnescapePunctuations([]byte(value))))
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

func markdownNode(m Model, node ast.Node, source []byte) []ui.Node {
	children := func() []ui.Node { return markdownChildren(m, node, source) }
	switch n := node.(type) {
	case *ast.Paragraph:
		return []ui.Node{html.P(html.Props{}, children()...)}
	case *ast.TextBlock:
		return children()
	case *ast.Heading:
		level := n.Level + 2
		if level > 6 {
			level = 6
		}
		return []ui.Node{html.Tag("h"+string(rune('0'+level)), html.Props{}, children()...)}
	case *ast.Emphasis:
		if n.Level == 2 {
			return []ui.Node{html.Strong(html.Props{}, children()...)}
		}
		return []ui.Node{html.Em(html.Props{}, children()...)}
	case *ast.String:
		return channelReferenceBody(m, string(n.Value))
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
		label := children()
		if href, ok := safeMarkdownHref(string(n.Destination)); ok {
			return []ui.Node{html.A(html.Props{Href: href}, label...)}
		}
		return label
	case *ast.AutoLink:
		label := string(n.Label(source))
		if href, ok := safeMarkdownHref(string(n.URL(source))); ok {
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
