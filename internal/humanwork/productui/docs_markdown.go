package productui

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

// docsASTMarkdownNodes renders the supported Markdown profile as typed UI
// nodes. Raw HTML and unsafe links never become active browser content.
func docsASTMarkdownNodes(view View, markdown string) []ui.Node {
	if len(markdown) > 64*1024 {
		return []ui.Node{ui.Text(markdown)}
	}
	source := []byte(markdown)
	root := goldmark.New().Parser().Parse(text.NewReader(source))
	return docsMarkdownChildren(view, root, source)
}

func docsMarkdownChildren(view View, parent ast.Node, source []byte) []ui.Node {
	var nodes []ui.Node
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		if node, ok := child.(*ast.Text); ok {
			value := string(node.Value(source))
			if !node.IsRaw() {
				value = stdhtml.UnescapeString(string(util.UnescapePunctuations([]byte(value))))
			}
			nodes = append(nodes, docsPlainTextNodes(view, value)...)
			if node.HardLineBreak() || node.SoftLineBreak() {
				nodes = append(nodes, html.Br(html.Props{}))
			}
			continue
		}
		nodes = append(nodes, docsMarkdownNode(view, child, source)...)
	}
	return nodes
}

func docsMarkdownNode(view View, node ast.Node, source []byte) []ui.Node {
	children := func() []ui.Node { return docsMarkdownChildren(view, node, source) }
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
		return docsPlainTextNodes(view, string(n.Value))
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
		target := string(n.Destination)
		label := children()
		if href, ok := docsInternalHref(target); ok {
			return []ui.Node{appLink(view, html.Props{Raw: map[string]any{"aria-label": docsText(view.Locale.Resolved, "open") + ": " + string(n.Text(source))}}, href, label...)}
		}
		if href, ok := docsSafeMarkdownHref(target); ok {
			return []ui.Node{html.A(html.Props{Href: href}, label...)}
		}
		return []ui.Node{ui.Text(string(n.Text(source)) + " (" + target + ")")}
	case *ast.AutoLink:
		target := string(n.URL(source))
		if href, ok := docsInternalHref(target); ok {
			return []ui.Node{appLink(view, html.Props{Raw: map[string]any{"aria-label": docsText(view.Locale.Resolved, "open") + ": " + target}}, href, ui.Text(target))}
		}
		if href, ok := docsSafeMarkdownHref(target); ok {
			return []ui.Node{html.A(html.Props{Href: href}, ui.Text(target))}
		}
		return []ui.Node{ui.Text(target)}
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

func docsPlainTextNodes(view View, value string) []ui.Node {
	var nodes []ui.Node
	for len(value) > 0 {
		index := strings.Index(value, "doc:")
		if index < 0 {
			nodes = append(nodes, ui.Text(value))
			break
		}
		if index > 0 {
			nodes = append(nodes, ui.Text(value[:index]))
			value = value[index:]
		}
		end := 4
		for end < len(value) && isDocTargetChar(value[end]) {
			end++
		}
		raw := value[:end]
		if href, ok := docsInternalHref(raw); ok {
			nodes = append(nodes, appLink(view, html.Props{Raw: map[string]any{"aria-label": docsText(view.Locale.Resolved, "open") + ": " + raw}}, href, ui.Text(raw)))
			value = value[end:]
			continue
		}
		nodes = append(nodes, ui.Text(value[:4]))
		value = value[4:]
	}
	return nodes
}

func docsSafeMarkdownHref(raw string) (string, bool) {
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
