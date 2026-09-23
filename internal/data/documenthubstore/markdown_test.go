package documenthubstore

import (
	"strings"
	"testing"
)

// TestTodo_HUB_005 is the PRIMARY test for HUB-005: the bounded Markdown
// profile renders headings, lists, tables, code and protected assets
// deterministically, and unsafe content never reaches the reader origin.
func TestTodo_HUB_005(t *testing.T) {
	html, err := RenderMarkdown("# Guide\n\n- one\n- two\n\n```go\nx := 1\n```\n")
	if err != nil {
		t.Fatalf("valid markdown refused: %v", err)
	}
	for _, want := range []string{"<h1>Guide</h1>", "<ul>", "<li>one</li>", "<code", "x := 1"} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q in %q", want, html)
		}
	}
	html, err = RenderMarkdown("| a | b |\n|---|---|\n| 1 | 2 |\n")
	if err != nil || !strings.Contains(html, "<table>") {
		t.Fatalf("table refused: %q err=%v", html, err)
	}
	html, err = RenderMarkdown("See [policy](doc:abc123) and ![alt](artifact://sha256:deadbeef).\n")
	if err != nil || !strings.Contains(html, "doc:abc123") || !strings.Contains(html, "artifact://sha256:deadbeef") {
		t.Fatalf("protected references dropped: %q err=%v", html, err)
	}
	for _, hostile := range []string{
		"<script>alert(1)</script>",
		"<img src=x onerror=alert(1)>",
		"[x](javascript:alert(1))",
		"[x](JaVaScRiPt:alert(1))",
		"[x](data:text/html,<script>alert(1)</script>)",
		"![evil](https://evil.example/img.png)",
		"![evil](http://evil.example/img.png)",
	} {
		html, err = RenderMarkdown(hostile + "\n")
		if err != nil {
			t.Fatalf("hostile input errored instead of sanitized: %q err=%v", hostile, err)
		}
		assertNoExecutable(t, hostile, html)
		if strings.Contains(html, "evil.example") {
			t.Fatalf("remote target survived: %q -> %q", hostile, html)
		}
	}
	again, err := RenderMarkdown("# Guide\n\n- one\n- two\n")
	if err != nil {
		t.Fatal(err)
	}
	first, err := RenderMarkdown("# Guide\n\n- one\n- two\n")
	if err != nil || first != again {
		t.Fatal("rendering not deterministic")
	}
	if _, err := RenderMarkdown(strings.Repeat("x", MaxMarkdownBytes+1)); err == nil {
		t.Fatal("oversize document accepted")
	}
	deep := strings.Repeat("> ", MaxMarkdownNesting+10) + "deep\n"
	if _, err := RenderMarkdown(deep); err == nil {
		t.Fatal("excessive nesting accepted")
	}
}

// TestTodo_HUB_005_Security is the SECURITY test for HUB-005: hostile
// Markdown never yields executable output, and rejected inputs carry no
// document bytes to an unauthorized reader.
func TestTodo_HUB_005_Security(t *testing.T) {
	hostile := []string{
		"[a](\"'><script>alert(1)</script>)",
		"<svg onload=alert(1)>",
		"<a href=\"javascript&#58;alert(1)\">x</a>",
		"[x](\njavascript:alert(1))",
		"![x](artifact://ok) <iframe src=https://evil.example>",
		"```\n</code><script>alert(1)</script>\n```\n",
		"# <script>alert(1)</script>\n",
		"| <script> | x |\n|---|---|\n| 1 | 2 |\n",
	}
	for _, src := range hostile {
		html, err := RenderMarkdown(src + "\n")
		if err != nil {
			continue
		}
		assertNoExecutable(t, src, html)
	}
}

// assertNoExecutable fails when rendered output carries a tag outside the
// profile allowlist or a link/image target outside the admitted schemes.
// All literal text is HTML-escaped by the renderer, so every raw "<" in the
// output opens one of our own tags: anything else is executable markup.
// Inert scheme words inside escaped text are legitimate documentation
// content and are allowed.
func assertNoExecutable(t *testing.T, src, html string) {
	t.Helper()
	rest := html
	for {
		open := strings.Index(rest, "<")
		if open < 0 {
			return
		}
		close := strings.Index(rest[open:], ">")
		if close < 0 {
			t.Fatalf("unclosed tag for %q: %q", src, html)
		}
		if !allowedTag(rest[open+1 : open+close]) {
			t.Fatalf("executable output for %q: tag %q in %q", src, rest[open:open+close+1], html)
		}
		rest = rest[open+close+1:]
	}
}

func allowedTag(tag string) bool {
	switch tag {
	case "ul", "/ul", "ol", "/ol", "li", "/li", "pre", "/pre", "code", "/code",
		"table", "/table", "thead", "/thead", "tbody", "/tbody", "tr", "/tr",
		"th", "/th", "td", "/td", "blockquote", "/blockquote", "p", "/p",
		"hr/", "/a",
		"h1", "/h1", "h2", "/h2", "h3", "/h3", "h4", "/h4", "h5", "/h5", "h6", "/h6":
		return true
	}
	if rest, ok := strings.CutPrefix(tag, "code class=\"language-"); ok {
		if !strings.HasSuffix(rest, "\"") {
			return false
		}
		for _, r := range strings.TrimSuffix(rest, "\"") {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '+' || r == '#' || r == '-') {
				return false
			}
		}
		return true
	}
	if rest, ok := strings.CutPrefix(tag, "a href=\""); ok {
		if !strings.HasSuffix(rest, "\"") {
			return false
		}
		target := strings.ToLower(strings.TrimSuffix(rest, "\""))
		return strings.HasPrefix(target, "doc:") && len(target) > len("doc:") ||
			strings.HasPrefix(target, "#") ||
			strings.HasPrefix(target, "https://") && len(target) > len("https://")
	}
	if rest, ok := strings.CutPrefix(tag, "img alt=\""); ok {
		if !strings.HasSuffix(rest, "/") {
			return false
		}
		body := strings.TrimSuffix(rest, "/")
		mid := strings.Index(body, "\" src=\"")
		if mid < 0 {
			return false
		}
		target := body[mid+len("\" src=\""):]
		if !strings.HasSuffix(target, "\"") {
			return false
		}
		lowered := strings.ToLower(strings.TrimSuffix(target, "\""))
		return strings.HasPrefix(lowered, "artifact:") && len(lowered) > len("artifact:")
	}
	return false
}

// FuzzTodo_HUB_005 is the FUZZ test for HUB-005: bounded hostile input never
// panics and never renders an executable target.
func FuzzTodo_HUB_005(f *testing.F) {
	seeds := []string{
		"# Title\n\n- a\n- b\n",
		"| a | b |\n|---|---|\n| 1 | 2 |\n",
		"```go\nx := 1\n```\n",
		"See [policy](doc:abc) and ![alt](artifact://sha256:00).\n",
		"<script>alert(1)</script>\n",
		"[x](javascript:alert(1))\n",
		"![evil](https://evil.example/img.png)\n",
		"> > > quoted\n",
		"1. one\n2. two\n",
		"[a](\"'><script>alert(1)</script>)\n",
		"```\n</code><script>alert(1)</script>\n```\n",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		html, err := RenderMarkdown(src)
		if err != nil {
			return
		}
		assertNoExecutable(t, src, html)
	})
}
