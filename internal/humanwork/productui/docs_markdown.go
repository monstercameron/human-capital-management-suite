package productui

import (
	"encoding/json"
	stdhtml "html"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/docsdiagram"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// docsMarkdownParser reads the supported profile: CommonMark plus GFM tables,
// strikethrough and task lists. It is built once; parsers are safe to reuse.
var docsMarkdownParser = goldmark.New(goldmark.WithExtensions(extension.Table, extension.Strikethrough, extension.TaskList)).Parser()

var _ parser.Parser = docsMarkdownParser

// docsASTMarkdownNodes renders the supported Markdown profile as typed UI
// nodes. Raw HTML and unsafe links never become active browser content.
func docsASTMarkdownNodes(view View, markdown string) []ui.Node {
	if len(markdown) > 64*1024 {
		return []ui.Node{ui.Text(markdown)}
	}
	source := []byte(markdown)
	root := docsMarkdownParser.Parse(text.NewReader(source))
	return docsMarkdownChildren(view, root, source)
}

func docsMarkdownChildren(view View, parent ast.Node, source []byte) []ui.Node {
	var nodes []ui.Node
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		// A sentence that ends in an attachment ("…is attached: [file](attachment:x).")
		// renders the attachment as a card, and the full stop after it was left
		// alone beside the card or on its own line (D-12). The card ends the
		// sentence, so bare punctuation straight after one is dropped.
		if docsIsAttachmentLink(child) {
			if next, ok := child.NextSibling().(*ast.Text); ok && docsOnlySentencePunctuation(string(next.Value(source))) {
				nodes = append(nodes, docsMarkdownNode(view, child, source)...)
				if next.HardLineBreak() || next.SoftLineBreak() {
					nodes = append(nodes, html.Br(html.Props{}))
				}
				child = next
				continue
			}
		}
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

// docsIsAttachmentLink reports whether node is a non-image attachment link,
// which the reader draws as a card rather than inline text.
func docsIsAttachmentLink(node ast.Node) bool {
	link, ok := node.(*ast.Link)
	if !ok {
		return false
	}
	_, ok = docsAttachmentID(string(link.Destination))
	return ok
}

// docsIsMetadataLead reports whether paragraph is a document's metadata
// line ("**Owner:** … · **Last reviewed:** …"): the first block, or the
// first after the title heading, opening with a bold label that ends in a
// colon. The reader sets it as a muted caption rather than body copy, so
// the first real heading leads the page (D-7).
func docsIsMetadataLead(paragraph *ast.Paragraph, source []byte) bool {
	if previous := paragraph.PreviousSibling(); previous != nil {
		heading, ok := previous.(*ast.Heading)
		if !ok || heading.Level != 1 || heading.PreviousSibling() != nil {
			return false
		}
	}
	label, ok := paragraph.FirstChild().(*ast.Emphasis)
	if !ok || label.Level != 2 {
		return false
	}
	return strings.HasSuffix(strings.TrimSpace(string(label.Text(source))), ":")
}

// docsLeadHidden names the metadata lead's fields the reader already shows
// in its facts row: the owner (the Owner fact, which also resolves "You")
// and the review date (the Reviewed fact, docsLeadReviewed). Repeating them
// in the lead had the page say "Owner: You" and "Owner: Rafael Torres"
// one line apart (r4 D-4).
var docsLeadHidden = map[string]bool{"owner": true, "last reviewed": true, "reviewed": true}

// docsLeadNodes renders the metadata lead without the fields the facts row
// already carries; the rest (a team channel, where to ask) stays as a muted
// caption, and a lead with nothing left renders nothing.
func docsLeadNodes(view View, paragraph *ast.Paragraph, source []byte) []ui.Node {
	type segment struct {
		label string
		nodes []ui.Node
	}
	var segments []segment
	for child := paragraph.FirstChild(); child != nil; child = child.NextSibling() {
		if label, ok := docsLeadLabel(child, source); ok {
			segments = append(segments, segment{label: strings.ToLower(label)})
		}
		if len(segments) == 0 {
			segments = append(segments, segment{})
		}
		current := &segments[len(segments)-1]
		if text, ok := child.(*ast.Text); ok {
			// The " · " between fields is the lead's own separator; it is
			// redrawn between the fields that remain.
			value := string(text.Value(source))
			if !text.IsRaw() {
				value = stdhtml.UnescapeString(string(util.UnescapePunctuations([]byte(value))))
			}
			if trimmed := strings.TrimRight(value, " ·"); strings.Contains(value[len(trimmed):], "·") {
				value = trimmed
			}
			if value != "" {
				current.nodes = append(current.nodes, docsPlainTextNodes(view, value)...)
			}
			continue
		}
		current.nodes = append(current.nodes, docsMarkdownNode(view, child, source)...)
	}
	var kept []ui.Node
	for _, seg := range segments {
		if docsLeadHidden[seg.label] || len(seg.nodes) == 0 {
			continue
		}
		if len(kept) > 0 {
			kept = append(kept, html.Span(html.Props{Class: "docs-lead-sep", Raw: map[string]any{"aria-hidden": "true"}}, ui.Text(" · ")))
		}
		kept = append(kept, html.Span(html.Props{Class: "docs-lead-field"}, seg.nodes...))
	}
	if len(kept) == 0 {
		return nil
	}
	return []ui.Node{html.P(html.Props{Class: "docs-lead", Dir: "auto"}, kept...)}
}

// docsLeadLabel reports a bold "Label:" node and its label without the
// colon.
func docsLeadLabel(node ast.Node, source []byte) (string, bool) {
	emphasis, ok := node.(*ast.Emphasis)
	if !ok || emphasis.Level != 2 {
		return "", false
	}
	label := strings.TrimSpace(string(emphasis.Text(source)))
	if !strings.HasSuffix(label, ":") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimSuffix(label, ":")), true
}

// docsLeadReviewedPattern finds the review date in a metadata lead line.
var docsLeadReviewedPattern = regexp.MustCompile(`(?i)(?:\*\*|__)(?:last reviewed|reviewed):(?:\*\*|__)\s*([^·\n]+)`)

// docsLeadReviewed returns the lead line's review date (RFC 3339 when it
// parses as "January 2, 2006", else the authored text), or "" when the
// document has no metadata lead. markdown is the body without its title
// heading.
func docsLeadReviewed(markdown string) string {
	body := strings.TrimLeft(markdown, " \t\r\n")
	lead, _, _ := strings.Cut(body, "\n\n")
	if !strings.HasPrefix(lead, "**") && !strings.HasPrefix(lead, "__") {
		return ""
	}
	m := docsLeadReviewedPattern.FindStringSubmatch(lead)
	if m == nil {
		return ""
	}
	value := strings.TrimSpace(m[1])
	if at, err := time.Parse("January 2, 2006", value); err == nil {
		// Noon UTC keeps the calendar day in every time zone the reader
		// formats it in.
		return time.Date(at.Year(), at.Month(), at.Day(), 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	}
	return value
}

// docsOnlySentencePunctuation reports whether value is nothing but
// sentence-ending or separating punctuation (and spaces), such as "." or ";".
func docsOnlySentencePunctuation(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, r := range value {
		if !strings.ContainsRune(".,;:!?…。،؛؟", r) {
			return false
		}
	}
	return true
}

func docsMarkdownNode(view View, node ast.Node, source []byte) []ui.Node {
	children := func() []ui.Node { return docsMarkdownChildren(view, node, source) }
	// Authored text keeps its own direction: every block is dir="auto", so
	// an English policy read in the Arabic interface is not laid out (and
	// its punctuation and numbers reordered) as right-to-left text.
	switch n := node.(type) {
	case *ast.Paragraph:
		if docsIsMetadataLead(n, source) {
			return docsLeadNodes(view, n, source)
		}
		if card, ok := docsProjectEmbedParagraph(view, n, source); ok {
			return []ui.Node{card}
		}
		if card, ok := docsJourneyEmbedParagraph(view, n, source); ok {
			return []ui.Node{card}
		}
		return []ui.Node{html.P(html.Props{Dir: "auto"}, children()...)}
	case *ast.TextBlock:
		return children()
	case *ast.Heading:
		// The page title is the h1 and the reader is an h2 section, so a
		// document's own "#" is an h2 beneath it.
		level := n.Level + 1
		if level > 6 {
			level = 6
		}
		// Headings carry a stable id so the outline beside the document
		// can link to them.
		return []ui.Node{html.Tag("h"+string(rune('0'+level)), html.Props{ID: docsHeadingID(string(n.Text(source))), Dir: "auto"}, children()...)}
	case *ast.Emphasis:
		if n.Level == 2 {
			return []ui.Node{html.Strong(html.Props{}, children()...)}
		}
		return []ui.Node{html.Em(html.Props{}, children()...)}
	case *ast.String:
		return docsPlainTextNodes(view, string(n.Value))
	case *ast.CodeSpan:
		return []ui.Node{html.Code(html.Props{}, ui.Text(string(n.Text(source))))}
	case *ast.FencedCodeBlock:
		language := strings.ToLower(strings.TrimSpace(string(n.Language(source))))
		code := docsBlockLines(node, source)
		if language == "mermaid" {
			return []ui.Node{docsDiagramNode(view, code)}
		}
		props := html.Props{}
		if language != "" {
			props.Class = "language-" + docsCodeLanguageClass(language)
		}
		return []ui.Node{html.Pre(html.Props{Dir: "ltr"}, html.Code(props, ui.Text(code)))}
	case *ast.CodeBlock:
		return []ui.Node{html.Pre(html.Props{Dir: "ltr"}, html.Code(html.Props{}, ui.Text(docsBlockLines(node, source))))}
	case *east.Table:
		return []ui.Node{html.Div(html.Props{Class: "docs-table-scroll", Raw: map[string]any{"tabindex": "0", "role": "region", "aria-label": docsText(view.Locale.Resolved, "table_label")}}, html.Tag("table", html.Props{Dir: "auto"}, docsTableSections(view, n, source)...))}
	case *east.Strikethrough:
		return []ui.Node{html.Tag("s", html.Props{}, children()...)}
	case *east.TaskCheckBox:
		return []ui.Node{html.Input(html.Props{Type: "checkbox", Checked: n.IsChecked, Disabled: true, Class: "docs-task"})}
	case *ast.List:
		if n.IsOrdered() {
			props := html.Props{}
			if n.Start > 1 {
				props.Raw = map[string]any{"start": n.Start}
			}
			props.Dir = "auto"
			return []ui.Node{html.Ol(props, children()...)}
		}
		return []ui.Node{html.Ul(html.Props{Dir: "auto"}, children()...)}
	case *ast.ListItem:
		return []ui.Node{html.Li(html.Props{Dir: "auto"}, children()...)}
	case *ast.Blockquote:
		return []ui.Node{html.Blockquote(html.Props{Dir: "auto"}, children()...)}
	case *ast.ThematicBreak:
		return []ui.Node{html.Hr(html.Props{})}
	case *ast.Link:
		target := string(n.Destination)
		label := children()
		if task, ok := docsProjectTaskLinkTarget(view, target, view.Navigate); ok {
			return []ui.Node{task}
		}
		if journey, ok := docsJourneyLinkTarget(view, target); ok {
			return []ui.Node{journey}
		}
		if chip, ok := docsChatLinkNode(view, target, label); ok {
			return []ui.Node{chip}
		}
		if card, ok := docsMediaLinkNode(view, target, string(n.Text(source))); ok {
			return []ui.Node{card}
		}
		if href, ok := docsInternalHref(target); ok {
			return []ui.Node{docsDocLinkNode(view, target, href, string(n.Text(source)), label)}
		}
		if href, ok := docsSafeMarkdownHref(target); ok {
			return []ui.Node{docsMarkdownAnchor(href, label...)}
		}
		return []ui.Node{ui.Text(string(n.Text(source)) + " (" + target + ")")}
	case *ast.AutoLink:
		target := string(n.URL(source))
		if task, ok := docsProjectTaskLinkTarget(view, target, view.Navigate); ok {
			return []ui.Node{task}
		}
		if journey, ok := docsJourneyLinkTarget(view, target); ok {
			return []ui.Node{journey}
		}
		if chip, ok := docsChatLinkNode(view, target, []ui.Node{ui.Text(target)}); ok {
			return []ui.Node{chip}
		}
		if href, ok := docsInternalHref(target); ok {
			return []ui.Node{docsDocLinkNode(view, target, href, target, []ui.Node{ui.Text(target)})}
		}
		if href, ok := docsSafeMarkdownHref(target); ok {
			return []ui.Node{docsMarkdownAnchor(href, ui.Text(target))}
		}
		return []ui.Node{ui.Text(target)}
	case *ast.Image:
		if image, ok := docsMediaImageNode(view, string(n.Destination), string(n.Text(source))); ok {
			return []ui.Node{image}
		}
		return children()
	case *ast.RawHTML:
		return []ui.Node{ui.Text(string(n.Text(source)))}
	case *ast.HTMLBlock:
		return []ui.Node{html.P(html.Props{Dir: "auto"}, ui.Text(string(n.Text(source))))}
	default:
		return children()
	}
}

func docsPlainTextNodes(view View, value string) []ui.Node {
	return docsChatTextNodes(view, value, docsDocTokenNodes)
}

// docsDocTokenNodes links the "doc:<id>" tokens in a run of text.
func docsDocTokenNodes(view View, value string) []ui.Node {
	var nodes []ui.Node
	for len(value) > 0 {
		taskIndex := strings.Index(value, "task:")
		index := strings.Index(value, "doc:")
		if taskIndex >= 0 && (index < 0 || taskIndex < index) {
			if taskIndex > 0 {
				nodes = append(nodes, ui.Text(value[:taskIndex]))
				value = value[taskIndex:]
			}
			end := len("task:")
			for end < len(value) && docsProjectTaskIDChar(value[end]) {
				end++
			}
			if ref, ok := ParseDocsProjectTaskReference(value[:end]); !ok || !docsProjectTaskHasAuthorizedPreview(view, ref) {
				for end > len("task:") && strings.ContainsRune(",.;:!?", rune(value[end-1])) {
					end--
				}
			}
			if end == len("task:") {
				nodes = append(nodes, ui.Text("task:"))
				value = value[len("task:"):]
				continue
			}
			target := value[:end]
			if _, ok := ParseDocsProjectTaskReference(target); ok {
				task, _ := docsProjectTaskLinkTarget(view, target, view.Navigate)
				nodes = append(nodes, task)
				value = value[end:]
				continue
			}
			nodes = append(nodes, ui.Text("task:"))
			value = value[len("task:"):]
			continue
		}
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
			nodes = append(nodes, docsDocLinkNode(view, raw, href, raw, []ui.Node{ui.Text(raw)}))
			value = value[end:]
			continue
		}
		nodes = append(nodes, ui.Text(value[:4]))
		value = value[4:]
	}
	return nodes
}

// encodeDocsLinks and decodeDocsLinks carry the reader's authorized view of
// a version's doc: targets (HUB-035) through docsMarkdownBodyProps, which
// stays comparable by value so the memoized body only re-renders when the
// authorized link states actually change.
func encodeDocsLinks(links []DocumentLinkTarget) string {
	if len(links) == 0 {
		return ""
	}
	data, err := json.Marshal(links)
	if err != nil {
		return ""
	}
	return string(data)
}

func decodeDocsLinks(value string) []DocumentLinkTarget {
	var links []DocumentLinkTarget
	if value != "" {
		_ = json.Unmarshal([]byte(value), &links)
	}
	return links
}

// docsLinkTarget looks up the authorized state of one doc: target already
// resolved by the server for this version.
func docsLinkTarget(view View, id string) (DocumentLinkTarget, bool) {
	if view.Document == nil {
		return DocumentLinkTarget{}, false
	}
	for _, l := range view.Document.Links {
		if l.DocumentID == id {
			return l, true
		}
	}
	return DocumentLinkTarget{}, false
}

// docsDocLinkNode renders one doc: link. A target the server already
// resolved as unreadable is marked so before the reader ever clicks it: it
// keeps its own href (opening it still shows the safe "not available"
// page), but never carries the restricted document's title text beyond
// what the author themselves wrote as the link label.
func docsDocLinkNode(view View, target, href, plainText string, label []ui.Node) ui.Node {
	id := strings.TrimPrefix(target, "doc:")
	locale := view.Locale.Resolved
	state, known := docsLinkTarget(view, id)
	if known && !state.Readable {
		return html.A(html.Props{
			Href: href, Class: "docs-link-unavailable",
			Raw:  map[string]any{"aria-label": plainText + " (" + docsText(locale, "link_unavailable") + ")"},
			Data: map[string]string{"docs-action": "open", "docs-id": href, "docs-link-state": "unavailable"},
		}, append(append([]ui.Node{}, label...), html.Span(html.Props{Class: "docs-link-state", Raw: map[string]any{"aria-hidden": "true"}}, ui.Text(" ("+docsText(locale, "link_unavailable")+")")))...)
	}
	return html.A(html.Props{Href: href, Raw: map[string]any{"aria-label": docsText(locale, "open") + ": " + plainText}, Data: map[string]string{"docs-action": "open", "docs-id": href}}, label...)
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

// docsHeadingID is the anchor for a heading: "sec-" and a lowercase slug of
// its text. Two headings with the same text share an anchor, which lands on
// the first; outlines are short enough that this has not mattered.
func docsHeadingID(text string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
		if b.Len() > 60 {
			break
		}
	}
	return "sec-" + strings.TrimRight(b.String(), "-")
}

type docsHeading struct {
	Level    int
	Text, ID string
}

// docsOutline lists the document's first three heading levels for the
// "On this page" rail.
func docsOutline(markdown string) []docsHeading {
	if len(markdown) > 64*1024 {
		return nil
	}
	source := []byte(markdown)
	root := docsMarkdownParser.Parse(text.NewReader(source))
	var out []docsHeading
	for child := root.FirstChild(); child != nil; child = child.NextSibling() {
		if heading, ok := child.(*ast.Heading); ok && heading.Level <= 3 {
			value := strings.TrimSpace(string(heading.Text(source)))
			if value != "" {
				out = append(out, docsHeading{Level: heading.Level, Text: value, ID: docsHeadingID(value)})
			}
		}
	}
	return out
}

// docsBlockLines returns a code block's text with its line breaks; Text()
// on a block node joins its lines without them.
func docsBlockLines(node ast.Node, source []byte) string {
	var b strings.Builder
	lines := node.Lines()
	for i := 0; i < lines.Len(); i++ {
		segment := lines.At(i)
		b.Write(segment.Value(source))
	}
	return strings.TrimRight(b.String(), "\n")
}

func docsCodeLanguageClass(language string) string {
	var b strings.Builder
	for _, r := range language {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			b.WriteRune(r)
		}
		if b.Len() >= 24 {
			break
		}
	}
	return b.String()
}

// docsTableSections renders a GFM table: the header row in <thead>, the rest
// in <tbody>, with column alignment as a class (style attributes are not
// allowed by the product's content security policy).
func docsTableSections(view View, table *east.Table, source []byte) []ui.Node {
	var head, body []ui.Node
	for row := table.FirstChild(); row != nil; row = row.NextSibling() {
		cells := []ui.Node{}
		header := false
		if _, ok := row.(*east.TableHeader); ok {
			header = true
		}
		column := 0
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			props := html.Props{Dir: "auto"}
			if column < len(table.Alignments) {
				switch table.Alignments[column] {
				case east.AlignLeft:
					props.Class = "docs-align-start"
				case east.AlignRight:
					props.Class = "docs-align-end"
				case east.AlignCenter:
					props.Class = "docs-align-center"
				}
			}
			content := docsMarkdownChildren(view, cell, source)
			if header {
				props.Raw = map[string]any{"scope": "col"}
				cells = append(cells, html.Tag("th", props, content...))
			} else {
				cells = append(cells, html.Tag("td", props, content...))
			}
			column++
		}
		if header {
			head = append(head, html.Tag("tr", html.Props{}, cells...))
		} else {
			body = append(body, html.Tag("tr", html.Props{}, cells...))
		}
	}
	out := []ui.Node{}
	if len(head) > 0 {
		out = append(out, html.Tag("thead", html.Props{}, head...))
	}
	if len(body) > 0 {
		out = append(out, html.Tag("tbody", html.Props{}, body...))
	}
	return out
}

// docsDiagramNode draws a Mermaid block as inline SVG in Go. A diagram the
// renderer cannot draw stays readable as its source, with a short note.
func docsDiagramNode(view View, code string) ui.Node {
	locale := "en"
	switch view.Locale.Resolved {
	case "de-DE":
		locale = "de"
	case "ar":
		locale = "ar"
	}
	if node, err := docsdiagram.Render(code, docsdiagram.Options{Locale: locale}); err == nil {
		return node
	}
	return html.Figure(html.Props{Class: "docs-diagram docs-diagram-fallback"},
		html.Pre(html.Props{}, html.Code(html.Props{Class: "language-mermaid"}, ui.Text(code))),
		html.Tag("figcaption", html.Props{Class: "docs-diagram-note"}, ui.Text(docsText(view.Locale.Resolved, "diagram_failed"))))
}

// docsMarkdownAnchor keeps every link in a document from reloading the app:
// "#section" links scroll the reader, product addresses go through the
// software router, and other sites open in a new tab.
func docsMarkdownAnchor(href string, children ...ui.Node) ui.Node {
	switch {
	case strings.HasPrefix(href, "#"):
		return html.A(html.Props{Href: href, Data: map[string]string{"docs-action": "jump", "docs-id": strings.TrimPrefix(href, "#")}}, children...)
	case strings.HasPrefix(href, "/workspace/app/"):
		return html.A(html.Props{Href: href, Data: map[string]string{"docs-action": "open", "docs-id": href}}, children...)
	case strings.HasPrefix(href, "/"):
		return html.A(html.Props{Href: href}, children...)
	}
	return html.A(html.Props{Href: href, Target: "_blank", Rel: "noopener noreferrer"}, children...)
}
