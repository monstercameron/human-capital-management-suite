package productui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// docsEditorParseHTML builds the serializer's tree from HTML the way the
// browser half builds it from the live DOM.
func docsEditorParseHTML(t *testing.T, markup string) *docsEditorNode {
	t.Helper()
	nodes, err := xhtml.ParseFragment(strings.NewReader(markup), &xhtml.Node{Type: xhtml.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		t.Fatalf("parse %q: %v", markup, err)
	}
	root := &docsEditorNode{Tag: "div"}
	for _, node := range nodes {
		if converted := docsEditorFromHTML(node); converted != nil {
			root.Children = append(root.Children, converted)
		}
	}
	return root
}

func docsEditorFromHTML(node *xhtml.Node) *docsEditorNode {
	switch node.Type {
	case xhtml.TextNode:
		return &docsEditorNode{Text: node.Data}
	case xhtml.ElementNode:
		out := &docsEditorNode{Tag: strings.ToLower(node.Data)}
		for _, attr := range node.Attr {
			if out.Attrs == nil {
				out.Attrs = map[string]string{}
			}
			out.Attrs[attr.Key] = attr.Val
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if converted := docsEditorFromHTML(child); converted != nil {
				out.Children = append(out.Children, converted)
			}
		}
		return out
	}
	return nil
}

func docsEditorRoundTrip(t *testing.T, markdown string) (string, string) {
	t.Helper()
	rendered := docsEditorHTML("en-US", markdown)
	return docsEditorMarkdown(docsEditorParseHTML(t, rendered)), rendered
}

// The formatted pane's Markdown must come back exactly as written for the
// canonical forms the serializer itself produces.
func TestDocsEditorRoundTripCanonical(t *testing.T) {
	corpus := []string{
		"# Title\n\nPlain paragraph with **bold**, _italic_, ~~struck~~ and `code`.\n",
		"## Second\n\n### Third\n\n#### Fourth\n\n##### Fifth\n\n###### Sixth\n",
		"- one\n- two\n  - nested a\n  - nested b\n- three\n",
		"3. three\n4. four\n   1. inner\n   2. inner two\n",
		"- [ ] todo\n- [x] done\n  - [ ] nested task\n",
		"- a\n\n- b\n",
		"> quoted\n>\n> > nested quote\n",
		"> - quoted list\n> - second\n",
		"| Left | Center | Right | None |\n| :--- | :---: | ---: | --- |\n| a | b | c | d |\n| **bold** | `x` | [l](https://e.com) | a \\| b |\n",
		"```go\nfunc main() {\n\tprintln(\"hi\")\n}\n```\n",
		"```mermaid\ngraph TD\n  A --> B\n```\n",
		"````\n```\ninner\n```\n````\n",
		"[site](https://example.com) and [doc](doc:handbook-7) and [path](/workspace/app/docs) and [top](#intro)\n",
		"[titled](https://example.com \"The title\")\n",
		"<https://example.com/a>\n",
		"![alt text](https://example.com/a.png \"Title\")\n",
		"_**both**_ and **_both_** and a*b*c\n",
		"**bold _and italic_ text** and ~~strike **bold**~~\n",
		"line one\nline two\n",
		"para\n\n---\n\nafter\n",
		"1\\. not a list\n\n\\# not a heading\n\n\\- not a bullet\n\n\\> not a quote\n",
		"2024\\. A year and + plus\n\n\\+ plus at the start\n",
		"\\[not a link\\] and \\`not code\\` and \\*not em\\* and \\~not struck\\~\n",
		"snake_case stays and \\_leading underscore\\_\n",
		"Raw <span>html</span> stays\n",
		"<div>\nblock\n</div>\n",
		"Unicode: مرحبا · Grüße · 😀\n",
		"a \\\\ backslash and AT&T\n",
	}
	for _, markdown := range corpus {
		got, rendered := docsEditorRoundTrip(t, markdown)
		if got != markdown {
			t.Errorf("round trip changed the text\n in: %q\nout: %q\nhtml: %s", markdown, got, rendered)
		}
	}
}

// Other spellings of the same document come back in canonical form and
// render exactly as the original did.
func TestDocsEditorRoundTripEquivalent(t *testing.T) {
	cases := []struct{ in, want string }{
		{"* star\n+ plus\n", "- star\n\n* plus\n"},
		{"1. a\n\n2) b\n", "1. a\n\n2) b\n"},
		{"1) paren\n2) two\n", "1. paren\n2. two\n"},
		{"Title\n=====\n\nSub\n---\n", "# Title\n\n## Sub\n"},
		{"a  \nb\n", "a\nb\n"},
		{"    indented code\n", "```\nindented code\n```\n"},
		{"*em* and __strong__\n", "_em_ and **strong**\n"},
		{"***both***\n", "_**both**_\n"},
		{"\\&amp; and AT&amp;T\n", ""},
		{"1. one\n1. two\n1. three\n", "1. one\n2. two\n3. three\n"},
		{"- [X] upper\n", "- [x] upper\n"},
		{"~~~python\nx = 1\n~~~\n", "```python\nx = 1\n```\n"},
	}
	for _, tc := range cases {
		got, rendered := docsEditorRoundTrip(t, tc.in)
		if tc.want != "" && got != tc.want {
			t.Errorf("canonical form of %q = %q, want %q", tc.in, got, tc.want)
		}
		if again := docsEditorHTML("en-US", got); again != rendered {
			t.Errorf("%q re-rendered differently\nwas: %s\nnow: %s", tc.in, rendered, again)
		}
	}
}

func TestDocsEditorHTMLIsSafe(t *testing.T) {
	markdown := "<script>alert(1)</script>\n\nHi <img src=x onerror=alert(1)> there\n\n[bad](javascript:alert(1)) [data](data:text/html,x) [ok](https://e.com)\n\n<https://e.com/\"onmouseover=x>\n\n![i](javascript:x)\n"
	rendered := docsEditorHTML("en-US", markdown)
	for _, forbidden := range []string{"<script", "<img", "javascript:alert", "href=\"data:", "style=", "onerror=alert(1)>"} {
		if strings.Contains(strings.ToLower(rendered), strings.ToLower(forbidden)) && forbidden != "javascript:alert" {
			t.Errorf("rendered HTML holds %q: %s", forbidden, rendered)
		}
	}
	if strings.Contains(rendered, `href="javascript`) {
		t.Errorf("a javascript: link kept its address: %s", rendered)
	}
	if !strings.Contains(rendered, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Errorf("raw HTML was not shown as text: %s", rendered)
	}
	if !strings.Contains(rendered, `<a href="https://e.com">ok</a>`) {
		t.Errorf("a safe link was dropped: %s", rendered)
	}
	// The unsafe link keeps its words but loses its target; raw HTML comes
	// back as the literal source it was.
	back := docsEditorMarkdown(docsEditorParseHTML(t, rendered))
	if !strings.Contains(back, "bad data [ok](https://e.com)") || !strings.HasPrefix(back, "<script>alert(1)</script>\n") {
		t.Errorf("serialized unsafe links wrongly: %q", back)
	}
	// Whatever the formatted pane holds, nothing unsafe is written back.
	pasted := docsEditorParseHTML(t, `<p><a href="javascript:alert(1)">click</a> <a href="//evil.example/x">proto</a> <a href="https://ok.example">fine</a></p>`)
	if got := docsEditorMarkdown(pasted); got != "click proto [fine](https://ok.example)\n" {
		t.Errorf("pasted links = %q", got)
	}
	for _, level := range []string{"1", "2", "3", "4", "5", "6"} {
		if html := docsEditorHTML("en-US", strings.Repeat("#", int(level[0]-'0'))+" H\n"); !strings.HasPrefix(html, "<h"+level) {
			t.Errorf("heading level %s rendered as %s", level, html)
		}
	}
	if html := docsEditorHTML("de-DE", "```mermaid\nA-->B\n```\n"); !strings.Contains(html, `data-label="`+docsText("de-DE", "editor_diagram")+`"`) || !strings.Contains(html, `class="language-mermaid"`) {
		t.Errorf("mermaid block is not an editable, labelled code block: %s", html)
	}
}

// Markup browsers and other editors produce is reduced to Markdown: styles
// and spans vanish, divs become paragraphs, scripts are dropped.
func TestDocsEditorSerializesPastedMarkup(t *testing.T) {
	cases := []struct{ html, want string }{
		{`<div style="color:red"><span>Hello</span>   <b>world</b></div><div>Line&nbsp;two</div><script>alert(1)</script>`, "Hello **world**\n\nLine two\n"},
		{`Loose text<p>para</p>more`, "Loose text\n\npara\n\nmore\n"},
		{`<p><strong> spaced </strong>word</p>`, "**spaced** word\n"},
		{`<p><em>a</em>b</p>`, "*a*b\n"},
		{`<ul><li>one</li><ul><li>indented</li></ul><li>two</li></ul>`, "- one\n  - indented\n- two\n"},
		{`<ol start="7"><li>seven</li><li>eight</li></ol>`, "7. seven\n8. eight\n"},
		{`<ul><li><input type="checkbox"><ul><li>n</li></ul></li><li><ul><li>m</li></ul></li></ul>`, "- [ ]\n  - n\n-\n  - m\n"},
		{`<ul><li><input type="checkbox" checked> done</li><li><p><input type="checkbox">loose</p></li></ul>`, "- [x] done\n\n- [ ] loose\n"},
		{`<table><tr><td align="right">r</td><td>x|y</td></tr><tr><td>1</td></tr></table>`, "| r | x\\|y |\n| ---: | --- |\n| 1 |  |\n"},
		{`<pre><code class="language-js">let a = 1;<br>let b = 2;</code></pre>`, "```js\nlet a = 1;\nlet b = 2;\n```\n"},
		{`<h2>Heading <br>two lines</h2>`, "## Heading two lines\n"},
		{`<p>a<br><br>b</p>`, "a\n\nb\n"},
		{`<p>1. looks like a list<br>- and a bullet</p>`, "1\\. looks like a list\n\\- and a bullet\n"},
		{`<blockquote><p>q</p><blockquote>deep</blockquote></blockquote>`, "> q\n>\n> > deep\n"},
		{`<p><img src="https://e.com/p.png" alt="pic"> <img src="javascript:x" alt="bad"></p>`, "![pic](https://e.com/p.png) bad\n"},
		{`<p><a href="https://e.com/a b">space</a> <a href="https://e.com">https://e.com</a></p>`, "[space](<https://e.com/a b>) <https://e.com>\n"},
		{`<p><code>a ` + "`" + ` b</code> <code>` + "`" + `x</code></p>`, "``a ` b`` `` `x ``\n"},
		{`<hr><p></p><p>  </p>`, "---\n"},
		{`<p>C#</p><h1>C#</h1>`, "C#\n\n# C\\#\n"},
		{`<section><header>Head</header><article><p>Body</p></article></section>`, "Head\n\nBody\n"},
	}
	for _, tc := range cases {
		if got := docsEditorMarkdown(docsEditorParseHTML(t, tc.html)); got != tc.want {
			t.Errorf("serialize %s\n got: %q\nwant: %q", tc.html, got, tc.want)
		}
	}
}

// Text typed into the formatted pane reads back as the same text, however
// many Markdown characters it holds.
func TestDocsEditorEscapesPlainText(t *testing.T) {
	samples := []string{
		"*a* _b_ `c` [d](e) <f> ~g~ \\h & AT&T &copy;",
		"# not a heading",
		"1. not a list",
		"- not a bullet",
		"+ not a bullet either",
		"> not a quote",
		"===",
		"---",
		"snake_case and _edge_",
		"![not an image](x)",
		"a | b",
		"<div>not html</div>",
	}
	for _, sample := range samples {
		markdown := docsEditorPlainMarkdown(sample)
		tree := docsEditorParseHTML(t, docsEditorHTML("en-US", markdown))
		if got := strings.TrimSpace(tree.textContent()); got != sample {
			t.Errorf("plain text %q became Markdown %q which reads %q", sample, markdown, got)
		}
		if len(tree.Children) != 1 || tree.Children[0].Tag != "p" {
			t.Errorf("plain text %q did not stay one paragraph: %q", sample, markdown)
		}
	}
	if got := docsEditorPlainMarkdown("one\ntwo\n\nthree"); got != "one\ntwo\n\nthree" {
		t.Errorf("plain paragraphs = %q", got)
	}
}

func TestDocsEditorTransforms(t *testing.T) {
	cases := []struct {
		name, value  string
		start, end   int
		command, arg string
		want         string
		wantS, wantE int
	}{
		{"bold", "hello world", 0, 5, "bold", "", "**hello** world", 2, 7},
		{"bold off", "**hello** world", 2, 7, "bold", "", "hello world", 0, 5},
		{"bold off including markers", "**hello**", 0, 9, "bold", "", "hello", 0, 5},
		{"bold keeps edge space out", "hello ", 0, 6, "bold", "", "**hello** ", 2, 7},
		{"bold caret", "ab", 1, 1, "bold", "", "a****b", 3, 3},
		{"italic", "a word here", 2, 6, "italic", "", "a _word_ here", 3, 7},
		{"italic inside a word", "abcdef", 2, 4, "italic", "", "ab*cd*ef", 3, 5},
		{"italic off asterisk", "*x*", 1, 2, "italic", "", "x", 0, 1},
		{"italic inside bold", "**b**", 2, 3, "italic", "", "**_b_**", 3, 4},
		{"strike", "gone", 0, 4, "strike", "", "~~gone~~", 2, 6},
		{"code with backtick", "a`b", 0, 3, "code", "", "`` a`b ``", 3, 6},
		{"link", "see docs", 4, 8, "link", "https://e.com", "see [docs](https://e.com)", 5, 9},
		{"link caret", "x", 1, 1, "link", "https://e.com", "x[https://e.com](https://e.com)", 2, 15},
		{"heading", "Title", 2, 2, "h2", "", "## Title", 5, 5},
		{"heading toggle off", "## Title", 4, 4, "h2", "", "Title", 1, 1},
		{"heading change", "## Title", 4, 4, "h1", "", "# Title", 3, 3},
		{"paragraph", "### T", 5, 5, "h0", "", "T", 1, 1},
		{"heading in list item", "- item", 3, 3, "h2", "", "- ## item", 6, 6},
		{"bullets", "a\nb", 0, 3, "ul", "", "- a\n- b", 0, 7},
		{"bullets off", "- a\n- b", 0, 7, "ul", "", "a\nb", 0, 3},
		{"numbers", "a\nb\nc", 0, 5, "ol", "", "1. a\n2. b\n3. c", 0, 14},
		{"numbers to bullets", "1. a\n2. b", 0, 9, "ul", "", "- a\n- b", 0, 7},
		{"task", "a", 0, 0, "task", "", "- [ ] a", 6, 6},
		{"quote", "a\n\nb", 0, 4, "quote", "", "> a\n>\n> b", 0, 9},
		{"unquote", "> a\n>\n> b", 0, 9, "quote", "", "a\n\nb", 0, 4},
		{"selection ending at a line start keeps that line", "a\nb", 0, 2, "ul", "", "- a\nb", 0, 3},
		{"code block", "x = 1", 0, 5, "codeblock", "", "```\nx = 1\n```", 4, 9},
		{"code block off", "```\nx = 1\n```", 0, 13, "codeblock", "", "x = 1", 0, 5},
		{"table", "Intro", 5, 5, "table", "Column", "Intro\n\n| Column 1 | Column 2 | Column 3 |\n| --- | --- | --- |\n|  |  |  |\n|  |  |  |\n", 9, 17},
		{"rule", "a\nb", 1, 1, "hr", "", "a\n\n---\n\nb", 8, 8},
		{"unknown", "a", 0, 1, "nope", "", "a", 0, 1},
	}
	for _, tc := range cases {
		got, s, e := docsEditorTransform(tc.value, tc.start, tc.end, tc.command, tc.arg)
		if got != tc.want || s != tc.wantS || e != tc.wantE {
			t.Errorf("%s: got %q [%d,%d], want %q [%d,%d]", tc.name, got, s, e, tc.want, tc.wantS, tc.wantE)
		}
	}
}

// A command in the formatted pane runs on the serialized text with the
// selection carried by marks; the result renders with the marks in place.
func TestDocsEditorMarksCarryTheSelection(t *testing.T) {
	marked := `<h2>Head</h2><p>say <strong>` + string(docsEditorMarkStart) + `hi` + string(docsEditorMarkEnd) + `</strong> now</p>`
	serialized := docsEditorMarkdown(docsEditorParseHTML(t, marked))
	clean, start, end := docsEditorExtractMarks(serialized)
	if clean != "## Head\n\nsay **hi** now\n" || clean[start:end] != "hi" {
		t.Fatalf("marks: %q [%d,%d]", clean, start, end)
	}
	next, s, e := docsEditorTransform(clean, start, end, "bold", "")
	if next != "## Head\n\nsay hi now\n" || next[s:e] != "hi" {
		t.Fatalf("bold off in formatted pane: %q [%d,%d]", next, s, e)
	}
	// Marks at the start of a heading line move past the "## " so the line
	// still renders as a heading.
	rendered := docsEditorHTML("en-US", docsEditorInsertMarks(next, 0, 2))
	if !strings.HasPrefix(rendered, `<h2 dir="auto">`+string(docsEditorMarkStart)) {
		t.Fatalf("mark broke the heading: %s", rendered)
	}
	// A mark that lands between list items still serializes inside one.
	between := `<ul><li>a</li>` + string(docsEditorMarkStart) + string(docsEditorMarkEnd) + `<li>b</li></ul>`
	if got, _, _ := docsEditorExtractMarks(docsEditorMarkdown(docsEditorParseHTML(t, between))); !strings.HasPrefix(got, "- a\n") {
		t.Errorf("mark between items: %q", got)
	}
	// A mark at the start of a line never hides a list marker from the
	// escaper.
	lineStart := `<p>` + string(docsEditorMarkStart) + `- literal` + string(docsEditorMarkEnd) + `</p>`
	if got, _, _ := docsEditorExtractMarks(docsEditorMarkdown(docsEditorParseHTML(t, lineStart))); got != "\\- literal\n" {
		t.Errorf("marked line start = %q", got)
	}
	if got, s, e := docsEditorExtractMarks("no marks"); got != "no marks" || s != 8 || e != 8 {
		t.Errorf("no marks: %q %d %d", got, s, e)
	}
	if got := docsEditorHTML("en-US", "[x](https://e.com/"+string(docsEditorMarkStart)+")"); strings.Contains(got, string(docsEditorMarkStart)) {
		t.Errorf("a mark reached an attribute: %s", got)
	}
}

func TestDocsEditorOffsets(t *testing.T) {
	value := "a😀b€"
	for units, want := range map[int]int{0: 0, 1: 1, 3: 5, 4: 6, 5: 9, 9: 9} {
		if got := docsEditorUTF16ToByte(value, units); got != want {
			t.Errorf("utf16 %d -> byte %d, want %d", units, got, want)
		}
	}
	for offset, want := range map[int]int{0: 0, 1: 1, 5: 3, 6: 4, 9: 5} {
		if got := docsEditorByteToUTF16(value, offset); got != want {
			t.Errorf("byte %d -> utf16 %d, want %d", offset, got, want)
		}
	}
	if got := docsEditorFirstDifference("abc€x", "abc€y"); got != 6 {
		t.Errorf("first difference = %d", got)
	}
	if got := docsEditorFirstDifference("€", "₭"); got != 0 {
		t.Errorf("first difference inside a rune = %d", got)
	}
}

func TestDocsEditorInsertFragment(t *testing.T) {
	cases := []struct {
		value          string
		start, end     int
		fragment, want string
		wantAt         int
	}{
		{"hello world", 6, 11, "**there**", "hello **there**", 15},
		{"before after", 7, 7, "- a\n- b", "before\n\n- a\n- b\n\nafter", 15},
		{"", 0, 0, "one\n\ntwo", "one\n\ntwo\n", 8},
	}
	for _, tc := range cases {
		got, s, e := docsEditorInsertFragment(tc.value, tc.start, tc.end, tc.fragment)
		if got != tc.want || s != tc.wantAt || e != tc.wantAt {
			t.Errorf("insert %q into %q: got %q [%d,%d], want %q at %d", tc.fragment, tc.value, got, s, e, tc.want, tc.wantAt)
		}
	}
}

func TestDocsEditorFormats(t *testing.T) {
	markdown := "## Head\n\n- **bold _both_**\n- [ ] task\n\n> quote `code`\n\n1. one [link](https://e.com)\n\n```\nblock\n```\n"
	at := func(needle string) int { return strings.Index(markdown, needle) + 1 }
	cases := map[string]string{
		"Head": "h2", "bold": "bold,ul", "both": "bold,italic,ul", "task": "task", "quote": "quote", "code": "code,quote",
		"link": "link,ol", "block": "codeblock",
	}
	for needle, want := range cases {
		if got := docsEditorFormatsKey(docsEditorFormatsAt(markdown, at(needle))); got != want {
			t.Errorf("formats at %q = %q, want %q", needle, got, want)
		}
	}
	ancestors := []*docsEditorNode{{Tag: "em"}, {Tag: "strong"}, {Tag: "li", Children: []*docsEditorNode{{Tag: "input", Attrs: map[string]string{"type": "checkbox"}}}}, {Tag: "ul"}, {Tag: "blockquote"}}
	if got := docsEditorFormatsKey(docsEditorFormatsOfAncestors(ancestors)); got != "bold,italic,task,quote" {
		t.Errorf("rich formats = %q", got)
	}
	if got := docsEditorFormatsKey(docsEditorFormatsOfAncestors([]*docsEditorNode{{Tag: "code"}, {Tag: "pre"}, {Tag: "ol"}, {Tag: "h3"}})); got != "h3,ol,codeblock" {
		t.Errorf("rich formats in code block = %q", got)
	}
}

func TestDocsEditorStats(t *testing.T) {
	stats := docsEditorStatsOf("# Hello world\n\n- **one** two\n- [x](https://e.com)\n\n| a | b |\n| - | - |\n| c | d |\n")
	if stats.Words != 9 {
		t.Errorf("words = %d, want 9", stats.Words)
	}
	if got := docsEditorCounts("en-US", docsEditorStats{Words: 1, Characters: 3}); got != "1 word · 3 characters" {
		t.Errorf("counts = %q", got)
	}
	if got := docsEditorCounts("de-DE", docsEditorStats{Words: 2, Characters: 9}); got != "2 Wörter · 9 Zeichen" {
		t.Errorf("counts de = %q", got)
	}
}

func TestDocsEditorHistory(t *testing.T) {
	c := newDocsEditorController("en-US", "T", "a")
	start := time.Unix(1000, 0)
	c.record("ab", true, start)
	c.record("abc", true, start.Add(200*time.Millisecond)) // folds into "ab"
	c.record("abcd", true, start.Add(3*time.Second))
	if len(c.history) != 3 || c.history[1] != "abc" {
		t.Fatalf("history = %q", c.history)
	}
	c.markdown = "abcd"
	c.undo(-1)
	if c.markdown != "abc" || !c.dirty {
		t.Fatalf("undo -> %q dirty=%v", c.markdown, c.dirty)
	}
	c.undo(-1)
	c.undo(-1)
	if c.markdown != "a" || c.dirty {
		t.Fatalf("undo to start -> %q dirty=%v", c.markdown, c.dirty)
	}
	c.undo(1)
	if c.markdown != "abc" {
		t.Fatalf("redo -> %q", c.markdown)
	}
	c.setMarkdown("abX", false, false)
	if _, ok := c.step(1); ok {
		t.Fatal("a new edit must drop the redo branch")
	}
	c.apply("hr", "")
	if c.markdown != "abX\n\n---\n" {
		t.Fatalf("apply at the end = %q", c.markdown)
	}
}

func TestDocsEditorNormalizeHref(t *testing.T) {
	cases := map[string]string{
		"https://e.com/x": "https://e.com/x", " example.com ": "https://example.com", "/workspace/app": "/workspace/app",
		"#intro": "#intro", "doc:handbook": "doc:handbook", "javascript:alert(1)": "", "": "", "not a url": "", "//evil.example": "",
	}
	for in, want := range cases {
		got, ok := docsEditorNormalizeHref(in)
		if got != want || ok != (want != "") {
			t.Errorf("normalize %q = %q %v, want %q", in, got, ok, want)
		}
	}
}

// The editor renders on the server with every control labelled, the
// formatted pane as an empty labelled textbox, and no inline styles.
func TestDocsEditorRendersAccessibleMarkup(t *testing.T) {
	var saved DocumentEditRequest
	props := docsSplitEditorProps{
		Locale: "en-US", DocumentID: "doc-1", BaseVersionID: "v1", Title: "Handbook", Markdown: "# Hi\n\nText",
		Save: func(request DocumentEditRequest, done func(error)) {
			saved = request
			done(errors.New("document.stale_version"))
		},
		Cancel: func() {},
	}
	markup, err := ui.RenderToString(ui.CreateElement(docsSplitEditor, props))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`id="docs-editor-source"`, `id="docs-editor-rich"`, `role="textbox"`, `aria-multiline="true"`, `contenteditable="true"`,
		`role="radiogroup"`, `role="toolbar"`, `aria-label="Bold (Ctrl+B)"`, `aria-pressed="false"`, `aria-haspopup="menu"`,
		`id="docs-editor-title"`, "Save new version", "Split", "Formatted", `dir="ltr"`, "2 words · 6 characters",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("editor markup lacks %q", want)
		}
	}
	if strings.Contains(markup, "style=") {
		t.Error("editor markup carries an inline style attribute")
	}
	// The formatted pane has no GWC children to clobber the caret with.
	i := strings.Index(markup, `id="docs-editor-rich"`)
	if rest := markup[i:]; !strings.HasPrefix(rest[strings.Index(rest, ">"):], "></div>") {
		t.Errorf("formatted pane is not empty: %s", rest[:min(len(rest), 300)])
	}
	for _, locale := range []string{"de-DE", "ar"} {
		props.Locale = locale
		localized, err := ui.RenderToString(ui.CreateElement(docsSplitEditor, props))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(localized, "Bold (Ctrl+B)") || strings.Contains(localized, "Save new version") {
			t.Errorf("%s editor shows English copy", locale)
		}
	}
	_ = saved
}

func TestDocsEditorCopyIsComplete(t *testing.T) {
	for key := range docsEditorCopy["en-US"] {
		for _, locale := range []string{"de-DE", "ar"} {
			if docsEditorCopy[locale][key] == "" {
				t.Errorf("%s lacks %q", locale, key)
			}
		}
	}
	for _, key := range []string{"title", "edit_save", "edit_cancel", "edit_busy", "edit_saved", "edit_failed", "edit_conflict"} {
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			if docsEditorText(locale, key) == "" || docsEditorText(locale, key) == key {
				t.Errorf("%s lacks %q", locale, key)
			}
		}
	}
	if !strings.Contains(docsEditorStylesheet(), ".docs-editor-panes") || strings.Contains(docsEditorStylesheet(), "left:") {
		t.Error("stylesheet is missing the panes or uses a physical left offset")
	}
}
