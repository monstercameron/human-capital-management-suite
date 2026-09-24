package productui

import (
	stdhtml "html"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// The split editor keeps one Markdown text and two views of it: the raw
// source and a formatted, editable rendering. This file is the pure half of
// that loop, testable without a browser: Markdown to safe HTML for the
// formatted pane, a small element tree the browser's DOM is copied into,
// the serializer from that tree back to Markdown, and the toolbar's text
// transforms. docs_editor_wasm.go only moves strings and trees across.

// docsEditorMarkStart and docsEditorMarkEnd bracket a selection while it
// travels through a render or a serialization. They are private-use
// characters, so no document is expected to hold them, and they are never
// escaped, so they survive both directions.
const (
	docsEditorMarkStart = rune(0xE000)
	docsEditorMarkEnd   = rune(0xE001)
)

// docsEditorNode is an element or text node: the formatted pane's DOM as
// the serializer sees it. Tag is a lower-case element name, or "" for text.
type docsEditorNode struct {
	Tag      string
	Text     string
	Attrs    map[string]string
	Children []*docsEditorNode
}

func (n *docsEditorNode) attr(name string) string {
	if n == nil || n.Attrs == nil {
		return ""
	}
	return n.Attrs[name]
}

func (n *docsEditorNode) hasClass(class string) bool {
	for _, value := range strings.Fields(n.attr("class")) {
		if value == class {
			return true
		}
	}
	return false
}

// textContent is the node's text with no Markdown meaning attached.
func (n *docsEditorNode) textContent() string {
	if n == nil {
		return ""
	}
	if n.Tag == "" {
		return n.Text
	}
	var b strings.Builder
	for _, child := range n.Children {
		b.WriteString(child.textContent())
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Markdown → HTML for the formatted pane
// ---------------------------------------------------------------------------

// docsEditorHTML renders Markdown as the formatted pane's HTML. It walks the
// same parser the reader uses, but writes every piece itself: text and
// attributes are escaped, raw HTML is shown as literal text, links keep
// only http(s), site-relative, #fragment and doc: targets, and nothing
// carries a style attribute (the content security policy forbids them).
// Headings keep their natural level so they map back exactly.
func docsEditorHTML(locale, markdown string) string {
	source := []byte(markdown)
	root := docsMarkdownParser.Parse(text.NewReader(source))
	r := docsEditorRenderer{source: source, locale: locale}
	r.children(root)
	return r.b.String()
}

type docsEditorRenderer struct {
	b      strings.Builder
	source []byte
	locale string
}

func (r *docsEditorRenderer) text(value string) { r.b.WriteString(stdhtml.EscapeString(value)) }

// attrValue drops selection marks from attributes: a mark belongs in text,
// and one left in an address would travel back into the document.
func (r *docsEditorRenderer) attrValue(value string) string {
	return stdhtml.EscapeString(docsEditorStripMarks(value))
}

func (r *docsEditorRenderer) children(parent ast.Node) {
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		r.node(child)
	}
}

func (r *docsEditorRenderer) node(node ast.Node) {
	switch n := node.(type) {
	case *ast.Text:
		value := string(n.Value(r.source))
		if !n.IsRaw() {
			value = docsEditorUnescape(value)
		}
		r.text(value)
		if n.HardLineBreak() || n.SoftLineBreak() {
			r.b.WriteString("<br>")
		}
	case *ast.String:
		r.text(string(n.Value))
	case *ast.Paragraph:
		r.b.WriteString(`<p dir="auto">`)
		r.children(n)
		r.b.WriteString("</p>")
	case *ast.TextBlock:
		r.children(n)
	case *ast.Heading:
		level := strconv.Itoa(min(max(n.Level, 1), 6))
		r.b.WriteString(`<h` + level + ` dir="auto">`)
		r.children(n)
		r.b.WriteString(`</h` + level + `>`)
	case *ast.ThematicBreak:
		r.b.WriteString("<hr>")
	case *ast.Blockquote:
		r.b.WriteString("<blockquote>")
		r.children(n)
		r.b.WriteString("</blockquote>")
	case *ast.List:
		if n.IsOrdered() {
			if n.Start != 1 {
				r.b.WriteString(`<ol start="` + strconv.Itoa(n.Start) + `">`)
			} else {
				r.b.WriteString("<ol>")
			}
			r.children(n)
			r.b.WriteString("</ol>")
			return
		}
		r.b.WriteString("<ul>")
		r.children(n)
		r.b.WriteString("</ul>")
	case *ast.ListItem:
		if docsEditorIsTaskItem(n) {
			r.b.WriteString(`<li class="docs-editor-task" dir="auto">`)
		} else {
			r.b.WriteString(`<li dir="auto">`)
		}
		r.children(n)
		r.b.WriteString("</li>")
	case *east.TaskCheckBox:
		if n.IsChecked {
			r.b.WriteString(`<input type="checkbox" checked>`)
		} else {
			r.b.WriteString(`<input type="checkbox">`)
		}
	case *ast.Emphasis:
		tag := "em"
		if n.Level == 2 {
			tag = "strong"
		}
		r.b.WriteString("<" + tag + ">")
		r.children(n)
		r.b.WriteString("</" + tag + ">")
	case *east.Strikethrough:
		r.b.WriteString("<s>")
		r.children(n)
		r.b.WriteString("</s>")
	case *ast.CodeSpan:
		r.b.WriteString("<code>")
		r.text(string(n.Text(r.source)))
		r.b.WriteString("</code>")
	case *ast.FencedCodeBlock:
		language := strings.ToLower(strings.TrimSpace(string(n.Language(r.source))))
		code := docsBlockLines(n, r.source)
		if language == "mermaid" {
			r.b.WriteString(`<pre class="docs-editor-diagram" dir="ltr" data-label="` + r.attrValue(docsText(r.locale, "editor_diagram")) + `"><code class="language-mermaid">`)
		} else if class := docsCodeLanguageClass(docsEditorStripMarks(language)); class != "" {
			r.b.WriteString(`<pre dir="ltr"><code class="language-` + class + `">`)
		} else {
			r.b.WriteString(`<pre dir="ltr"><code>`)
		}
		r.text(code)
		r.b.WriteString("</code></pre>")
	case *ast.CodeBlock:
		r.b.WriteString(`<pre dir="ltr"><code>`)
		r.text(docsBlockLines(n, r.source))
		r.b.WriteString("</code></pre>")
	case *ast.HTMLBlock:
		var raw strings.Builder
		lines := n.Lines()
		for i := 0; i < lines.Len(); i++ {
			segment := lines.At(i)
			raw.Write(segment.Value(r.source))
		}
		if n.HasClosure() {
			raw.Write(n.ClosureLine.Value(r.source))
		}
		r.b.WriteString(`<pre class="docs-editor-raw" dir="ltr" data-md-raw="block">`)
		r.text(strings.TrimRight(raw.String(), "\n"))
		r.b.WriteString("</pre>")
	case *ast.RawHTML:
		var raw strings.Builder
		for i := 0; i < n.Segments.Len(); i++ {
			segment := n.Segments.At(i)
			raw.Write(segment.Value(r.source))
		}
		r.b.WriteString(`<code class="docs-editor-raw" data-md-raw="inline">`)
		r.text(raw.String())
		r.b.WriteString("</code>")
	case *ast.Link:
		target := string(n.Destination)
		if !docsEditorSafeHref(docsEditorStripMarks(target)) {
			r.children(n)
			return
		}
		r.b.WriteString(`<a href="` + r.attrValue(target) + `"`)
		if len(n.Title) > 0 {
			r.b.WriteString(` title="` + r.attrValue(string(n.Title)) + `"`)
		}
		r.b.WriteString(">")
		r.children(n)
		r.b.WriteString("</a>")
	case *ast.AutoLink:
		target := string(n.URL(r.source))
		if n.AutoLinkType != ast.AutoLinkURL || !docsEditorSafeHref(target) {
			r.text(string(n.Label(r.source)))
			return
		}
		r.b.WriteString(`<a href="` + r.attrValue(target) + `">`)
		r.text(string(n.Label(r.source)))
		r.b.WriteString("</a>")
	case *ast.Image:
		// Images are kept as a non-editable token rather than an <img>, so
		// editing never fetches anything and the address survives intact.
		r.b.WriteString(`<span class="docs-editor-image" contenteditable="false" data-md-image="` + r.attrValue(string(n.Destination)) + `"`)
		if len(n.Title) > 0 {
			r.b.WriteString(` data-md-title="` + r.attrValue(string(n.Title)) + `"`)
		}
		r.b.WriteString(">")
		r.text(docsEditorPlainText(n, r.source))
		r.b.WriteString("</span>")
	case *east.Table:
		r.table(n)
	default:
		r.children(node)
	}
}

func (r *docsEditorRenderer) table(table *east.Table) {
	r.b.WriteString("<table>")
	inBody := false
	for row := table.FirstChild(); row != nil; row = row.NextSibling() {
		_, header := row.(*east.TableHeader)
		if header {
			r.b.WriteString("<thead><tr>")
		} else {
			if !inBody {
				r.b.WriteString("<tbody>")
				inBody = true
			}
			r.b.WriteString("<tr>")
		}
		column := 0
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			tag := "td"
			if header {
				tag = "th"
			}
			class := ""
			if column < len(table.Alignments) {
				switch table.Alignments[column] {
				case east.AlignLeft:
					class = "docs-align-start"
				case east.AlignRight:
					class = "docs-align-end"
				case east.AlignCenter:
					class = "docs-align-center"
				}
			}
			if class != "" {
				r.b.WriteString("<" + tag + ` class="` + class + `" dir="auto">`)
			} else {
				r.b.WriteString("<" + tag + ` dir="auto">`)
			}
			r.children(cell)
			r.b.WriteString("</" + tag + ">")
			column++
		}
		if header {
			r.b.WriteString("</tr></thead>")
		} else {
			r.b.WriteString("</tr>")
		}
	}
	if inBody {
		r.b.WriteString("</tbody>")
	}
	r.b.WriteString("</table>")
}

func docsEditorIsTaskItem(item *ast.ListItem) bool {
	first := item.FirstChild()
	if first == nil {
		return false
	}
	_, ok := first.FirstChild().(*east.TaskCheckBox)
	return ok
}

func docsEditorPlainText(node ast.Node, source []byte) string {
	var b strings.Builder
	_ = ast.Walk(node, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := child.(type) {
		case *ast.Text:
			b.WriteString(docsEditorUnescape(string(n.Value(source))))
		case *ast.String:
			b.WriteString(string(n.Value))
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

// docsEditorSafeHref keeps the link targets the reader will follow: web
// addresses, site paths, in-page fragments, doc: references, chat
// references and attachment:<id> files.
func docsEditorSafeHref(target string) bool {
	if _, ok := docsInternalHref(target); ok || docsChatRefTarget(target) {
		return true
	}
	if _, ok := docsAttachmentID(target); ok {
		return true
	}
	_, ok := docsSafeMarkdownHref(target)
	return ok
}

// ---------------------------------------------------------------------------
// Tree → Markdown
// ---------------------------------------------------------------------------

var docsEditorBlockTags = map[string]bool{
	"p": true, "div": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"ul": true, "ol": true, "li": true, "blockquote": true, "pre": true, "hr": true, "table": true,
	"section": true, "article": true, "header": true, "footer": true, "main": true, "aside": true, "nav": true,
	"figure": true, "figcaption": true, "details": true, "summary": true, "dl": true, "dt": true, "dd": true,
	"address": true, "center": true, "form": true, "fieldset": true, "body": true, "html": true,
}

// docsEditorSkipTags never carry document text.
var docsEditorSkipTags = map[string]bool{
	"script": true, "style": true, "template": true, "head": true, "title": true, "meta": true, "link": true,
	"noscript": true, "iframe": true, "object": true, "embed": true, "svg": true, "math": true, "button": true,
	"select": true, "textarea": true, "canvas": true, "video": true, "audio": true, "colgroup": true, "col": true,
}

// docsEditorMarkdown serializes the formatted pane back to Markdown. Blocks
// are separated by one blank line; the text ends with a single newline.
func docsEditorMarkdown(root *docsEditorNode) string {
	if root == nil {
		return ""
	}
	blocks := docsEditorBlocks(root.Children)
	out := strings.Join(blocks, "\n\n")
	if out == "" {
		return ""
	}
	return out + "\n"
}

// docsEditorBlocks turns a mixed run of nodes into Markdown blocks. Loose
// inline content between blocks (bare text in the editor, a pasted <span>)
// becomes a paragraph of its own.
func docsEditorBlocks(nodes []*docsEditorNode) []string {
	var blocks []string
	var run []*docsEditorNode
	flush := func() {
		if len(run) == 0 {
			return
		}
		blocks = append(blocks, docsEditorParagraphs(run)...)
		run = nil
	}
	// Two lists of the same kind in a row would read back as one list, so
	// the second takes the other marker ("*" after "-", ")" after ".").
	lastList, lastAlternate, listEnd := "", false, -1
	for _, node := range nodes {
		if node.Tag == "" || !docsEditorBlockTags[node.Tag] {
			if docsEditorSkipTags[node.Tag] {
				continue
			}
			run = append(run, node)
			continue
		}
		flush()
		if node.Tag == "ul" || node.Tag == "ol" {
			alternate := lastList == node.Tag && listEnd == len(blocks) && !lastAlternate
			if list := docsEditorList(node, alternate); list != "" {
				blocks = append(blocks, list)
				lastList, lastAlternate, listEnd = node.Tag, alternate, len(blocks)
			}
			continue
		}
		blocks = append(blocks, docsEditorBlock(node)...)
	}
	flush()
	return blocks
}

func docsEditorBlock(node *docsEditorNode) []string {
	switch node.Tag {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		content := docsEditorInline(node.Children, docsEditorInlineOptions{singleLine: true})
		if strings.TrimSpace(docsEditorStripMarks(content)) == "" {
			if strings.ContainsAny(content, string([]rune{docsEditorMarkStart, docsEditorMarkEnd})) {
				return []string{content}
			}
			return nil
		}
		if strings.HasSuffix(content, "#") {
			content = content[:len(content)-1] + `\#`
		}
		return []string{strings.Repeat("#", int(node.Tag[1]-'0')) + " " + content}
	case "hr":
		return []string{"---"}
	case "pre":
		if node.attr("data-md-raw") == "block" {
			return []string{strings.TrimRight(docsEditorPreText(node), "\n")}
		}
		return []string{docsEditorFence(node)}
	case "blockquote":
		inner := strings.Join(docsEditorBlocks(node.Children), "\n\n")
		if inner == "" {
			return nil
		}
		lines := strings.Split(inner, "\n")
		for i, line := range lines {
			if line == "" {
				lines[i] = ">"
			} else {
				lines[i] = "> " + line
			}
		}
		return []string{strings.Join(lines, "\n")}
	case "ul", "ol":
		if list := docsEditorList(node, false); list != "" {
			return []string{list}
		}
		return nil
	case "table":
		if table := docsEditorTable(node); table != "" {
			return []string{table}
		}
		return nil
	default:
		// p, div, li outside a list, section and the like: their content,
		// which may itself mix inline runs and blocks.
		return docsEditorBlocks(node.Children)
	}
}

// docsEditorParagraphs writes an inline run; an empty line inside it (two
// line breaks in a row) starts a new paragraph.
func docsEditorParagraphs(run []*docsEditorNode) []string {
	content := docsEditorInline(run, docsEditorInlineOptions{})
	var out []string
	for _, part := range strings.Split(content, "\n\n") {
		part = strings.Trim(part, "\n")
		if strings.TrimSpace(part) == "" {
			if strings.ContainsAny(part, string([]rune{docsEditorMarkStart, docsEditorMarkEnd})) {
				out = append(out, strings.TrimSpace(part))
			}
			continue
		}
		out = append(out, part)
	}
	return out
}

func docsEditorPreText(node *docsEditorNode) string {
	var b strings.Builder
	var walk func(n *docsEditorNode)
	walk = func(n *docsEditorNode) {
		switch {
		case n.Tag == "":
			b.WriteString(strings.ReplaceAll(n.Text, string(docsEditorNBSP), " "))
		case n.Tag == "br":
			b.WriteByte('\n')
		case n.Tag == "div" || n.Tag == "p":
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
				b.WriteByte('\n')
			}
			for _, child := range n.Children {
				walk(child)
			}
			if !strings.HasSuffix(b.String(), "\n") {
				b.WriteByte('\n')
			}
		default:
			for _, child := range n.Children {
				walk(child)
			}
		}
	}
	for _, child := range node.Children {
		walk(child)
	}
	return b.String()
}

func docsEditorFence(pre *docsEditorNode) string {
	language := ""
	classes := pre.attr("class")
	for _, child := range pre.Children {
		if child.Tag == "code" {
			classes += " " + child.attr("class")
		}
	}
	for _, class := range strings.Fields(classes) {
		if value, ok := strings.CutPrefix(class, "language-"); ok {
			language = docsCodeLanguageClass(value)
			break
		}
	}
	if pre.hasClass("docs-editor-diagram") {
		language = "mermaid"
	}
	code := strings.TrimSuffix(docsEditorPreText(pre), "\n")
	longest, run := 0, 0
	for _, r := range code {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", max(3, longest+1))
	return fence + language + "\n" + code + "\n" + fence
}

func docsEditorList(list *docsEditorNode, alternate bool) string {
	ordered := list.Tag == "ol"
	number := 1
	if start, err := strconv.Atoi(strings.TrimSpace(list.attr("start"))); err == nil && start >= 0 {
		number = start
	}
	type item struct {
		node  *docsEditorNode
		extra []*docsEditorNode
	}
	var items []*item
	loose := false
	for _, child := range list.Children {
		switch {
		case child.Tag == "li":
			items = append(items, &item{node: child})
			for _, grandchild := range child.Children {
				if grandchild.Tag == "p" {
					loose = true
				}
			}
		case child.Tag == "ul" || child.Tag == "ol":
			// Browsers indent by nesting a list beside the item, not in it.
			if len(items) == 0 {
				items = append(items, &item{node: &docsEditorNode{Tag: "li"}})
			}
			items[len(items)-1].extra = append(items[len(items)-1].extra, child)
		case child.Tag == "" && strings.TrimSpace(child.Text) == "":
		default:
			items = append(items, &item{node: &docsEditorNode{Tag: "li", Children: []*docsEditorNode{child}}})
		}
	}
	var out []string
	for _, it := range items {
		marker := "- "
		if alternate {
			marker = "* "
		}
		if ordered {
			marker = strconv.Itoa(number) + ". "
			if alternate {
				marker = strconv.Itoa(number) + ") "
			}
			number++
		}
		task, checked := docsEditorTaskBox(it.node)
		children := append(append([]*docsEditorNode{}, it.node.Children...), it.extra...)
		blocks := docsEditorBlocks(children)
		joiner := "\n"
		if loose {
			joiner = "\n\n"
		}
		body := strings.Join(blocks, joiner)
		if task {
			box := "[ ] "
			if checked {
				box = "[x] "
			}
			if body == "" || strings.HasPrefix(body, "\n") || docsEditorIsListLine(body) {
				body = strings.TrimSuffix(box, " ") + prefixNewline(body)
			} else {
				body = box + body
			}
		} else if docsEditorIsListLine(body) || strings.HasPrefix(body, "#") {
			// An item that opens with a nested list or heading must not
			// glue its marker onto the nested one.
			body = "\n" + body
		}
		indent := strings.Repeat(" ", len(marker))
		lines := strings.Split(body, "\n")
		for i := 1; i < len(lines); i++ {
			if lines[i] != "" {
				lines[i] = indent + lines[i]
			}
		}
		first := marker + lines[0]
		if lines[0] == "" {
			first = strings.TrimRight(marker, " ")
		}
		lines[0] = first
		out = append(out, strings.Join(lines, "\n"))
	}
	joiner := "\n"
	if loose {
		joiner = "\n\n"
	}
	return strings.Join(out, joiner)
}

func prefixNewline(body string) string {
	if body == "" || strings.HasPrefix(body, "\n") {
		return body
	}
	return "\n" + body
}

func docsEditorIsListLine(line string) bool {
	line = strings.TrimLeft(docsEditorStripMarks(line), " ")
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "+ ") || line == "-" {
		return true
	}
	digits := 0
	for digits < len(line) && line[digits] >= '0' && line[digits] <= '9' {
		digits++
	}
	return digits > 0 && digits < len(line) && (line[digits] == '.' || line[digits] == ')')
}

// docsEditorTaskBox finds a checkbox that opens a list item, looking
// through the paragraph a loose list wraps the item's text in.
func docsEditorTaskBox(li *docsEditorNode) (bool, bool) {
	for _, child := range li.Children {
		switch {
		case child.Tag == "" && strings.TrimSpace(docsEditorStripMarks(child.Text)) == "":
			continue
		case child.Tag == "input" && strings.EqualFold(child.attr("type"), "checkbox"):
			_, checked := child.Attrs["checked"]
			child.Tag = "#consumed"
			return true, checked
		case child.Tag == "p" || child.Tag == "div" || child.Tag == "span" || child.Tag == "label":
			return docsEditorTaskBox(child)
		default:
			return false, false
		}
	}
	return false, false
}

func docsEditorTable(table *docsEditorNode) string {
	var rows [][]*docsEditorNode
	var collect func(n *docsEditorNode)
	collect = func(n *docsEditorNode) {
		for _, child := range n.Children {
			switch child.Tag {
			case "thead", "tbody", "tfoot":
				collect(child)
			case "tr":
				var cells []*docsEditorNode
				for _, cell := range child.Children {
					if cell.Tag == "th" || cell.Tag == "td" {
						cells = append(cells, cell)
					}
				}
				rows = append(rows, cells)
			}
		}
	}
	collect(table)
	if len(rows) == 0 {
		return ""
	}
	columns := 0
	for _, row := range rows {
		columns = max(columns, len(row))
	}
	if columns == 0 {
		return ""
	}
	cell := func(row []*docsEditorNode, i int) string {
		if i >= len(row) {
			return ""
		}
		return docsEditorInline(row[i].Children, docsEditorInlineOptions{singleLine: true, table: true})
	}
	line := func(row []*docsEditorNode) string {
		parts := make([]string, columns)
		for i := range parts {
			parts[i] = cell(row, i)
		}
		return "| " + strings.Join(parts, " | ") + " |"
	}
	aligns := make([]string, columns)
	for i := range aligns {
		aligns[i] = "---"
		if i < len(rows[0]) {
			c := rows[0][i]
			switch {
			case c.hasClass("docs-align-start") || strings.EqualFold(c.attr("align"), "left"):
				aligns[i] = ":---"
			case c.hasClass("docs-align-end") || strings.EqualFold(c.attr("align"), "right"):
				aligns[i] = "---:"
			case c.hasClass("docs-align-center") || strings.EqualFold(c.attr("align"), "center"):
				aligns[i] = ":---:"
			}
		}
	}
	out := []string{line(rows[0]), "| " + strings.Join(aligns, " | ") + " |"}
	for _, row := range rows[1:] {
		out = append(out, line(row))
	}
	return strings.Join(out, "\n")
}

// ---------------------------------------------------------------------------
// Inline serialization
// ---------------------------------------------------------------------------

type docsEditorInlineOptions struct {
	singleLine bool // headings and table cells: a line break becomes a space
	table      bool // table cells: escape the column separator
}

// docsEditorWriter writes inline Markdown with collapsed whitespace. A
// space is held back until the next visible character, so runs collapse
// and nothing trails a line; lineStart drives the escapes that only matter
// at the start of a line.
type docsEditorWriter struct {
	b          strings.Builder
	opts       docsEditorInlineOptions
	pending    bool
	lead       bool
	leadBreak  bool
	lineStart  bool
	hasContent bool
	last       rune
	// escapeIn counts the visible runes still to write before the one
	// that must be escaped ("1986. A year" escapes the dot, not the 1).
	escapeIn int
}

func (w *docsEditorWriter) space() {
	if !w.hasContent {
		w.lead = true
		return
	}
	if !w.lineStart {
		w.pending = true
		w.last = ' '
	}
}

func (w *docsEditorWriter) newline() {
	if w.opts.singleLine {
		w.space()
		return
	}
	if !w.hasContent {
		w.leadBreak = true
		return
	}
	w.pending = false
	w.b.WriteByte('\n')
	w.lineStart = true
	w.last = '\n'
}

func (w *docsEditorWriter) flush() {
	if w.pending {
		w.b.WriteByte(' ')
		w.last = ' '
		w.pending = false
	}
}

// raw writes Markdown syntax (a delimiter, a finished span) as is.
func (w *docsEditorWriter) raw(value string) {
	if value == "" {
		return
	}
	w.flush()
	w.b.WriteString(value)
	w.hasContent = true
	w.lineStart = false
	w.last, _ = utf8.DecodeLastRuneInString(value)
}

func (w *docsEditorWriter) mark(r rune) {
	w.flush()
	w.b.WriteRune(r)
	w.hasContent = true
}

// text writes document text, collapsing whitespace and escaping what
// Markdown would otherwise read as syntax.
func (w *docsEditorWriter) text(value string) {
	runes := []rune(value)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == docsEditorMarkStart || r == docsEditorMarkEnd {
			w.mark(r)
			continue
		}
		if r == docsEditorNBSP || unicode.IsSpace(r) || r == docsEditorZWSP {
			if r != docsEditorZWSP {
				w.space()
			}
			continue
		}
		w.flush()
		next := func(k int) rune {
			for j := i + k; j < len(runes); j++ {
				if runes[j] != docsEditorMarkStart && runes[j] != docsEditorMarkEnd {
					return runes[j]
				}
			}
			return 0
		}
		escape := false
		switch r {
		case '\\', '*', '`', '[', ']', '~':
			escape = true
		case '_':
			escape = !(docsEditorWordRune(w.last) && docsEditorWordRune(next(1)))
		case '<':
			n := next(1)
			escape = unicode.IsLetter(n) || n == '/' || n == '!' || n == '?'
		case '&':
			escape = docsEditorEntityAhead(runes[i+1:])
		case '|':
			escape = w.opts.table
		}
		if w.lineStart && !escape {
			if at := docsEditorLineStartEscape(runes[i:]); at == 0 {
				escape = true
			} else if at > 0 {
				w.escapeIn = at + 1
			}
		}
		if w.escapeIn > 0 {
			w.escapeIn--
			escape = escape || w.escapeIn == 0
		}
		if escape {
			w.b.WriteByte('\\')
		}
		w.b.WriteRune(r)
		w.hasContent = true
		w.lineStart = false
		w.last = r
	}
}

func docsEditorWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func docsEditorEntityAhead(rest []rune) bool {
	for i, r := range rest {
		if r == ';' {
			return i > 0
		}
		if !(r == '#' || unicode.IsLetter(r) || unicode.IsDigit(r)) || i > 32 {
			return false
		}
	}
	return false
}

// docsEditorLineStartEscape reports which visible rune of text starting a
// line must be escaped so it is not read as a block marker (a heading,
// quote, list item or rule), or -1 when none must.
func docsEditorLineStartEscape(rest []rune) int {
	clean := []rune(docsEditorStripMarks(string(rest)))
	if len(clean) == 0 {
		return -1
	}
	yes := func(ok bool) int {
		if ok {
			return 0
		}
		return -1
	}
	at := func(i int) rune {
		if i < len(clean) {
			return clean[i]
		}
		return 0
	}
	blank := func(r rune) bool { return r == 0 || r == ' ' || r == '\t' || r == docsEditorNBSP }
	switch c := clean[0]; {
	case c == '#':
		n := 0
		for at(n) == '#' {
			n++
		}
		return yes(n <= 6 && blank(at(n)))
	case c == '>':
		return 0
	case c == '-' || c == '+' || c == '=':
		if blank(at(1)) && c != '=' {
			return 0
		}
		for _, r := range clean {
			if r != c && r != ' ' {
				return -1
			}
		}
		return 0
	case c >= '0' && c <= '9':
		n := 0
		for at(n) >= '0' && at(n) <= '9' {
			n++
		}
		if n <= 9 && (at(n) == '.' || at(n) == ')') && blank(at(n+1)) {
			return n
		}
	}
	return -1
}

// docsEditorInline serializes an inline run.
func docsEditorInline(nodes []*docsEditorNode, opts docsEditorInlineOptions) string {
	w := &docsEditorWriter{opts: opts, lineStart: true}
	docsEditorInlineNodes(w, nodes)
	return strings.TrimRight(w.b.String(), " \n")
}

func docsEditorInlineNodes(w *docsEditorWriter, nodes []*docsEditorNode) {
	for i, node := range nodes {
		var next *docsEditorNode
		if i+1 < len(nodes) {
			next = nodes[i+1]
		}
		docsEditorInlineNode(w, node, next)
	}
}

func docsEditorInlineNode(w *docsEditorWriter, node, next *docsEditorNode) {
	switch node.Tag {
	case "":
		w.text(node.Text)
	case "#consumed":
	case "br":
		w.newline()
	case "strong", "b":
		docsEditorWrap(w, node, "**")
	case "em", "i", "cite", "var", "dfn":
		delimiter := "_"
		if docsEditorWordRune(w.last) || docsEditorWordRune(docsEditorFirstRune(next)) {
			delimiter = "*"
		}
		docsEditorWrap(w, node, delimiter)
	case "s", "del", "strike":
		docsEditorWrap(w, node, "~~")
	case "code", "kbd", "samp", "tt":
		if node.attr("data-md-raw") == "inline" {
			w.raw(node.textContent())
			return
		}
		w.raw(docsEditorCodeSpan(strings.ReplaceAll(node.textContent(), string(docsEditorNBSP), " ")))
	case "a":
		docsEditorLink(w, node)
	case "span":
		if src := node.attr("data-md-image"); src != "" {
			docsEditorImage(w, node.textContent(), src, node.attr("data-md-title"))
			return
		}
		docsEditorInlineNodes(w, node.Children)
	case "img":
		if src := node.attr("src"); src != "" && docsEditorSafeHref(src) {
			docsEditorImage(w, node.attr("alt"), src, node.attr("title"))
		} else {
			w.text(node.attr("alt"))
		}
	case "input", "wbr":
	default:
		if docsEditorSkipTags[node.Tag] {
			return
		}
		if docsEditorBlockTags[node.Tag] {
			// A block pasted inside an inline element: keep its text on
			// its own line.
			w.newline()
			docsEditorInlineNodes(w, node.Children)
			w.newline()
			return
		}
		docsEditorInlineNodes(w, node.Children)
	}
}

func docsEditorFirstRune(node *docsEditorNode) rune {
	if node == nil {
		return 0
	}
	for _, r := range docsEditorStripMarks(node.textContent()) {
		return r
	}
	return 0
}

// docsEditorWrap writes emphasis. Whitespace at the edges moves outside the
// delimiters, where Markdown needs it; an empty span writes nothing.
func docsEditorWrap(w *docsEditorWriter, node *docsEditorNode, delimiter string) {
	inner := &docsEditorWriter{opts: w.opts, last: rune(delimiter[0])}
	docsEditorInlineNodes(inner, node.Children)
	content := inner.b.String()
	trailBreak := strings.HasSuffix(strings.TrimRight(content, " "), "\n")
	trail := strings.HasSuffix(content, " ") || inner.pending
	content = strings.TrimRight(content, " \n")
	if inner.leadBreak {
		w.newline()
	} else if inner.lead {
		w.space()
	}
	if strings.TrimSpace(docsEditorStripMarks(content)) == "" {
		w.raw(content)
	} else {
		w.raw(delimiter + content + delimiter)
	}
	if trailBreak {
		w.newline()
	} else if trail {
		w.space()
	}
}

func docsEditorCodeSpan(code string) string {
	longest, run := 0, 0
	for _, r := range code {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", longest+1)
	if strings.HasPrefix(code, "`") || strings.HasSuffix(code, "`") || (strings.HasPrefix(code, " ") && strings.HasSuffix(code, " ") && strings.TrimSpace(code) != "") {
		code = " " + code + " "
	}
	if code == "" {
		return ""
	}
	return fence + code + fence
}

func docsEditorLink(w *docsEditorWriter, node *docsEditorNode) {
	href := strings.TrimSpace(docsEditorStripMarks(node.attr("href")))
	if !docsEditorSafeHref(href) {
		docsEditorInlineNodes(w, node.Children)
		return
	}
	inner := &docsEditorWriter{opts: docsEditorInlineOptions{singleLine: true, table: w.opts.table}, last: '['}
	docsEditorInlineNodes(inner, node.Children)
	label := strings.TrimRight(inner.b.String(), " ")
	if inner.lead {
		w.space()
	}
	title := docsEditorStripMarks(node.attr("title"))
	if docsEditorStripMarks(node.textContent()) == href && title == "" && (strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://")) && !strings.ContainsAny(href, "<> ") {
		w.raw("<" + href + ">")
		return
	}
	if strings.TrimSpace(docsEditorStripMarks(label)) == "" {
		label += docsEditorEscapeLinkText(href)
	}
	w.raw("[" + label + "](" + docsEditorDestination(href) + docsEditorTitle(title) + ")")
}

func docsEditorImage(w *docsEditorWriter, alt, src, title string) {
	inner := &docsEditorWriter{opts: docsEditorInlineOptions{singleLine: true}, last: '['}
	inner.text(alt)
	w.raw("![" + strings.TrimSpace(inner.b.String()) + "](" + docsEditorDestination(docsEditorStripMarks(src)) + docsEditorTitle(docsEditorStripMarks(title)) + ")")
}

func docsEditorEscapeLinkText(value string) string {
	inner := &docsEditorWriter{opts: docsEditorInlineOptions{singleLine: true}}
	inner.text(value)
	return inner.b.String()
}

func docsEditorDestination(href string) string {
	if strings.ContainsAny(href, " ()<>") {
		return "<" + strings.NewReplacer("<", `\<`, ">", `\>`).Replace(href) + ">"
	}
	return href
}

func docsEditorTitle(title string) string {
	if title == "" {
		return ""
	}
	return ` "` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(title) + `"`
}

// ---------------------------------------------------------------------------
// Selection marks and offsets
// ---------------------------------------------------------------------------

func docsEditorStripMarks(value string) string {
	if !strings.ContainsAny(value, string([]rune{docsEditorMarkStart, docsEditorMarkEnd})) {
		return value
	}
	return strings.Map(func(r rune) rune {
		if r == docsEditorMarkStart || r == docsEditorMarkEnd {
			return -1
		}
		return r
	}, value)
}

// docsEditorExtractMarks removes the selection marks from serialized
// Markdown and returns where they were, as byte offsets into the clean
// text. A missing mark falls back to the other one, then to the end.
func docsEditorExtractMarks(marked string) (clean string, start, end int) {
	start, end = -1, -1
	var b strings.Builder
	for _, r := range marked {
		switch r {
		case docsEditorMarkStart:
			if start < 0 {
				start = b.Len()
			}
		case docsEditorMarkEnd:
			if end < 0 {
				end = b.Len()
			}
		default:
			b.WriteRune(r)
		}
	}
	clean = b.String()
	switch {
	case start < 0 && end < 0:
		start, end = len(clean), len(clean)
	case start < 0:
		start = end
	case end < 0:
		end = start
	}
	if end < start {
		start, end = end, start
	}
	return clean, start, end
}

// docsEditorInsertMarks places the selection marks into Markdown before it
// is rendered, moving each one past any block syntax at the start of its
// line so a mark never turns "# Title" into a paragraph.
func docsEditorInsertMarks(markdown string, start, end int) string {
	start = docsEditorPastBlockSyntax(markdown, docsEditorClamp(markdown, start))
	end = max(start, docsEditorPastBlockSyntax(markdown, docsEditorClamp(markdown, end)))
	return markdown[:start] + string(docsEditorMarkStart) + markdown[start:end] + string(docsEditorMarkEnd) + markdown[end:]
}

func docsEditorClamp(value string, offset int) int {
	offset = min(max(offset, 0), len(value))
	for offset > 0 && offset < len(value) && !utf8.RuneStart(value[offset]) {
		offset--
	}
	return offset
}

func docsEditorPastBlockSyntax(markdown string, offset int) int {
	lineStart := strings.LastIndexByte(markdown[:offset], '\n') + 1
	lineEnd := strings.IndexByte(markdown[offset:], '\n')
	if lineEnd < 0 {
		lineEnd = len(markdown)
	} else {
		lineEnd += offset
	}
	line := markdown[lineStart:lineEnd]
	prefix := docsEditorLinePrefixLength(line)
	if trimmed := strings.TrimLeft(line, " "); strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
		prefix = len(line)
	}
	if isRule := strings.Trim(strings.ReplaceAll(line, " ", ""), "-*_") == "" && len(strings.TrimSpace(line)) >= 3; isRule {
		prefix = len(line)
	}
	if offset-lineStart < prefix {
		return lineStart + prefix
	}
	return offset
}

// docsEditorLinePrefixLength measures the block syntax opening a line:
// indentation, quote markers, a list marker with an optional task box,
// and a heading marker.
func docsEditorLinePrefixLength(line string) int {
	i := 0
	for {
		for i < len(line) && line[i] == ' ' {
			i++
		}
		if i < len(line) && line[i] == '>' {
			i++
			if i < len(line) && line[i] == ' ' {
				i++
			}
			continue
		}
		break
	}
	if n := docsEditorListMarkerLength(line[i:]); n > 0 {
		i += n
		if strings.HasPrefix(line[i:], "[ ] ") || strings.HasPrefix(line[i:], "[x] ") || strings.HasPrefix(line[i:], "[X] ") {
			i += 4
		}
	}
	if n := docsEditorHeadingMarkerLength(line[i:]); n > 0 {
		i += n
	}
	return i
}

func docsEditorListMarkerLength(line string) int {
	if len(line) >= 2 && (line[0] == '-' || line[0] == '*' || line[0] == '+') && line[1] == ' ' {
		return 2
	}
	digits := 0
	for digits < len(line) && digits < 9 && line[digits] >= '0' && line[digits] <= '9' {
		digits++
	}
	if digits > 0 && digits+1 < len(line) && (line[digits] == '.' || line[digits] == ')') && line[digits+1] == ' ' {
		return digits + 2
	}
	return 0
}

func docsEditorHeadingMarkerLength(line string) int {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n == 0 || n > 6 {
		return 0
	}
	if n == len(line) {
		return n
	}
	if line[n] == ' ' {
		return n + 1
	}
	return 0
}

// docsEditorUTF16ToByte converts a textarea offset (UTF-16 code units) to
// a byte offset into value.
func docsEditorUTF16ToByte(value string, units int) int {
	count := 0
	for index, r := range value {
		if count >= units {
			return index
		}
		if r >= 0x10000 {
			count += 2
		} else {
			count++
		}
	}
	return len(value)
}

func docsEditorByteToUTF16(value string, offset int) int {
	count := 0
	for index, r := range value {
		if index >= offset {
			break
		}
		if r >= 0x10000 {
			count += 2
		} else {
			count++
		}
	}
	return count
}

// docsEditorFirstDifference is where two texts start to differ, as a byte
// offset into next, clamped to a rune boundary. Undo puts the caret there.
func docsEditorFirstDifference(previous, next string) int {
	i := 0
	for i < len(previous) && i < len(next) && previous[i] == next[i] {
		i++
	}
	return docsEditorClamp(next, i)
}

// ---------------------------------------------------------------------------
// Plain text, counts and formats
// ---------------------------------------------------------------------------

// docsEditorPlainMarkdown writes pasted plain text as Markdown that reads
// back as the same text: blank lines separate paragraphs, single line
// breaks stay line breaks, and syntax characters are escaped.
func docsEditorPlainMarkdown(value string) string {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	root := &docsEditorNode{Tag: "div"}
	for _, paragraph := range strings.Split(value, "\n\n") {
		p := &docsEditorNode{Tag: "p"}
		for i, line := range strings.Split(paragraph, "\n") {
			if i > 0 {
				p.Children = append(p.Children, &docsEditorNode{Tag: "br"})
			}
			p.Children = append(p.Children, &docsEditorNode{Text: line})
		}
		root.Children = append(root.Children, p)
	}
	return strings.TrimSuffix(docsEditorMarkdown(root), "\n")
}

type docsEditorStats struct{ Words, Characters int }

// docsEditorStatsOf counts the words and characters a reader sees, not
// the Markdown syntax around them.
func docsEditorStatsOf(markdown string) docsEditorStats {
	if len(markdown) > 256*1024 {
		return docsEditorStats{Words: len(strings.Fields(markdown)), Characters: utf8.RuneCountInString(markdown)}
	}
	source := []byte(markdown)
	root := docsMarkdownParser.Parse(text.NewReader(source))
	var b strings.Builder
	_ = ast.Walk(root, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			if node.Type() == ast.TypeBlock {
				b.WriteByte('\n')
			}
			return ast.WalkContinue, nil
		}
		switch n := node.(type) {
		case *ast.Text:
			b.Write(util.UnescapePunctuations(n.Value(source)))
			if n.SoftLineBreak() || n.HardLineBreak() {
				b.WriteByte('\n')
			}
		case *ast.String:
			b.Write(n.Value)
		case *ast.CodeSpan:
			b.Write(n.Text(source))
			return ast.WalkSkipChildren, nil
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			b.WriteString(docsBlockLines(node, source))
		case *ast.AutoLink:
			b.Write(n.Label(source))
		}
		return ast.WalkContinue, nil
	})
	plain := strings.TrimSpace(b.String())
	words := 0
	for _, field := range strings.Fields(plain) {
		if strings.IndexFunc(field, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0 {
			words++
		}
	}
	characters := 0
	for _, r := range plain {
		if r != '\n' {
			characters++
		}
	}
	return docsEditorStats{Words: words, Characters: characters}
}

// docsEditorFormatsAt names the formatting around a byte offset in the
// Markdown pane, for the toolbar's pressed states: "bold", "italic",
// "strike", "code", "link", "h1".."h6", "ul", "ol", "task", "quote" and
// "codeblock".
func docsEditorFormatsAt(markdown string, offset int) map[string]bool {
	out := map[string]bool{}
	if len(markdown) > 256*1024 {
		return out
	}
	source := []byte(markdown)
	root := docsMarkdownParser.Parse(text.NewReader(source))
	var found ast.Node
	_ = ast.Walk(root, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || found != nil {
			return ast.WalkContinue, nil
		}
		switch n := node.(type) {
		case *ast.Text:
			if n.Segment.Start <= offset && offset <= n.Segment.Stop {
				found = n
				return ast.WalkStop, nil
			}
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			lines := node.Lines()
			if lines.Len() > 0 && lines.At(0).Start <= offset && offset <= lines.At(lines.Len()-1).Stop {
				found = n
				return ast.WalkStop, nil
			}
		}
		return ast.WalkContinue, nil
	})
	for node := found; node != nil; node = node.Parent() {
		switch n := node.(type) {
		case *ast.Emphasis:
			if n.Level == 2 {
				out["bold"] = true
			} else {
				out["italic"] = true
			}
		case *east.Strikethrough:
			out["strike"] = true
		case *ast.CodeSpan:
			out["code"] = true
		case *ast.Link, *ast.AutoLink:
			out["link"] = true
		case *ast.Heading:
			out["h"+strconv.Itoa(n.Level)] = true
		case *ast.List:
			if n.IsOrdered() {
				out["ol"] = true
			} else {
				out["ul"] = true
			}
		case *ast.ListItem:
			if docsEditorIsTaskItem(n) {
				out["task"] = true
			}
		case *ast.Blockquote:
			out["quote"] = true
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			out["codeblock"] = true
		}
	}
	if out["task"] {
		delete(out, "ul")
	}
	return out
}

// Characters the formatted pane produces that are not text: a no-break
// space (browsers type one for a trailing space) and a zero-width space.
const (
	docsEditorNBSP = rune(0x00A0)
	docsEditorZWSP = rune(0x200B)
)

// docsEditorUnescape resolves a text node's backslash escapes and entity
// references in one pass, so an escaped "\&copy;" stays the literal text
// "&copy;" rather than becoming a copyright sign.
func docsEditorUnescape(value string) string {
	if !strings.ContainsAny(value, `\&`) {
		return value
	}
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c == '\\' && i+1 < len(value) && util.IsPunct(value[i+1]):
			b.WriteByte(value[i+1])
			i++
		case c == '&':
			end := strings.IndexByte(value[i:], ';')
			if end > 1 && end <= 33 && docsEditorEntityAhead([]rune(value[i+1:i+end+1])) {
				b.WriteString(stdhtml.UnescapeString(value[i : i+end+1]))
				i += end
				continue
			}
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
