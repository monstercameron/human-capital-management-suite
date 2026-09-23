// Bounded Markdown profile for HUB-005: headings, lists, tables, fenced
// code, links and protected-asset images. Everything else, including all
// raw HTML, is escaped. Links admit only doc:, #fragment and https targets;
// images admit only artifact: references, so no author-controlled bytes can
// execute on the reader origin. Rendering is deterministic for its input.
package documenthubstore

import (
	"errors"
	"html"
	"strings"
)

// MaxMarkdownBytes bounds one rendered document; MaxMarkdownNesting bounds
// blockquote and list depth.
const (
	MaxMarkdownBytes   = 256 << 10
	MaxMarkdownNesting = 8
)

// ErrTooLarge is returned when input exceeds MaxMarkdownBytes;
// ErrTooDeep when nesting exceeds MaxMarkdownNesting.
var (
	ErrTooLarge = errors.New("document markdown: input exceeds size bound")
	ErrTooDeep  = errors.New("document markdown: nesting exceeds depth bound")
)

// RenderMarkdown parses src with NormalizeMarkdown applied and returns
// deterministic safe HTML.
func RenderMarkdown(src string) (string, error) {
	if len(src) > MaxMarkdownBytes {
		return "", ErrTooLarge
	}
	lines := strings.Split(NormalizeMarkdown(src), "\n")
	var out strings.Builder
	i := 0
	for i < len(lines) {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			i++
			continue
		}
		if depth := quoteDepth(line); depth > 0 {
			if depth > MaxMarkdownNesting {
				return "", ErrTooDeep
			}
			var inner []string
			for i < len(lines) && quoteDepth(lines[i]) > 0 {
				d := quoteDepth(lines[i])
				if d > MaxMarkdownNesting {
					return "", ErrTooDeep
				}
				inner = append(inner, stripQuotes(lines[i]))
				i++
			}
			body, err := RenderMarkdown(strings.Join(inner, "\n"))
			if err != nil {
				return "", err
			}
			out.WriteString("<blockquote>\n" + body + "</blockquote>\n")
			continue
		}
		if level, text, ok := headingLevel(line); ok {
			out.WriteString("<h" + string(rune('0'+level)) + ">" + renderInline(text) + "</h" + string(rune('0'+level)) + ">\n")
			i++
			continue
		}
		if fence, ok := fenceLang(line); ok {
			var code []string
			i++
			for i < len(lines) && !isFence(lines[i]) {
				code = append(code, lines[i])
				i++
			}
			if i < len(lines) {
				i++
			}
			out.WriteString("<pre><code")
			if fence != "" {
				out.WriteString(` class="language-` + sanitizeLang(fence) + `"`)
			}
			out.WriteString(">" + html.EscapeString(strings.Join(code, "\n")) + "</code></pre>\n")
			continue
		}
		if strings.Contains(line, "|") && i+1 < len(lines) && isTableDelimiter(lines[i+1]) {
			header := line
			i += 2
			var rows []string
			for i < len(lines) && strings.Contains(lines[i], "|") && strings.TrimSpace(lines[i]) != "" {
				rows = append(rows, lines[i])
				i++
			}
			out.WriteString(renderTable(header, rows))
			continue
		}
		if isHR(line) {
			out.WriteString("<hr/>\n")
			i++
			continue
		}
		if _, ordered, ok := listItem(line); ok {
			tag := "ul"
			if ordered {
				tag = "ol"
			}
			out.WriteString("<" + tag + ">\n")
			for i < len(lines) {
				text, isOrdered, isItem := listItem(lines[i])
				if !isItem || isOrdered != ordered {
					break
				}
				if indentLevel(lines[i]) > MaxMarkdownNesting {
					return "", ErrTooDeep
				}
				out.WriteString("<li>" + renderInline(strings.TrimSpace(text)) + "</li>\n")
				i++
			}
			out.WriteString("</" + tag + ">\n")
			continue
		}
		para := []string{strings.TrimSpace(line)}
		i++
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" && !isBlockStart(lines[i]) {
			para = append(para, strings.TrimSpace(lines[i]))
			i++
		}
		out.WriteString("<p>" + renderInline(strings.Join(para, " ")) + "</p>\n")
	}
	return out.String(), nil
}

func headingLevel(line string) (int, string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	n := 0
	for n < len(trimmed) && trimmed[n] == '#' {
		n++
	}
	if n == 0 || n > 6 || n >= len(trimmed) || trimmed[n] != ' ' {
		return 0, "", false
	}
	return n, strings.TrimSpace(trimmed[n+1:]), true
}

func isFence(line string) bool { _, ok := fenceLang(line); return ok }

// sanitizeLang keeps only identifier characters for a code language class,
// so a hostile fence line cannot inject attributes.
func sanitizeLang(lang string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '+' || r == '#' || r == '-' {
			return r
		}
		return -1
	}, lang)
}

func fenceLang(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "```") {
		return "", false
	}
	fields := strings.Fields(strings.TrimPrefix(trimmed, "```"))
	if len(fields) == 0 {
		return "", true
	}
	return fields[0], true
}

func isTableDelimiter(line string) bool {
	trimmed := strings.TrimSpace(strings.ReplaceAll(line, "|", ""))
	if trimmed == "" {
		return false
	}
	for _, cell := range strings.Split(trimmed, " ") {
		c := strings.TrimSpace(cell)
		if c == "" {
			continue
		}
		c = strings.Trim(c, ":")
		if c == "" {
			continue
		}
		for _, r := range c {
			if r != '-' {
				return false
			}
		}
	}
	return true
}

func isHR(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 {
		return false
	}
	for _, r := range trimmed {
		if r != '-' && r != '*' && r != '_' {
			return false
		}
	}
	return true
}

func isBlockStart(line string) bool {
	if _, _, ok := headingLevel(line); ok {
		return true
	}
	if isFence(line) || isHR(line) || quoteDepth(line) > 0 {
		return true
	}
	if _, _, ok := listItem(line); ok {
		return true
	}
	return strings.Contains(line, "|")
}

func quoteDepth(line string) int {
	n := 0
	rest := strings.TrimLeft(line, " \t")
	for strings.HasPrefix(rest, ">") {
		n++
		rest = strings.TrimLeft(rest[1:], " \t")
	}
	return n
}

func stripQuotes(line string) string {
	rest := strings.TrimLeft(line, " \t")
	for strings.HasPrefix(rest, ">") {
		rest = strings.TrimLeft(rest[1:], " \t")
	}
	return rest
}

func indentLevel(line string) int {
	n := 0
	for n < len(line) && (line[n] == ' ' || line[n] == '\t') {
		n++
	}
	return n / 2
}

func listItem(line string) (text string, ordered, ok bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if len(trimmed) >= 2 && (trimmed[0] == '-' || trimmed[0] == '*' || trimmed[0] == '+') && trimmed[1] == ' ' {
		return trimmed[2:], false, true
	}
	j := 0
	for j < len(trimmed) && trimmed[j] >= '0' && trimmed[j] <= '9' {
		j++
	}
	if j > 0 && j+1 < len(trimmed) && trimmed[j] == '.' && trimmed[j+1] == ' ' {
		return trimmed[j+2:], true, true
	}
	return "", false, false
}

func renderTable(header string, rows []string) string {
	var out strings.Builder
	out.WriteString("<table>\n<thead><tr>")
	for _, cell := range splitRow(header) {
		out.WriteString("<th>" + renderInline(strings.TrimSpace(cell)) + "</th>")
	}
	out.WriteString("</tr></thead>\n<tbody>\n")
	for _, row := range rows {
		out.WriteString("<tr>")
		for _, cell := range splitRow(row) {
			out.WriteString("<td>" + renderInline(strings.TrimSpace(cell)) + "</td>")
		}
		out.WriteString("</tr>\n")
	}
	out.WriteString("</tbody>\n</table>\n")
	return out.String()
}

func splitRow(line string) []string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	return strings.Split(trimmed, "|")
}

// renderInline renders code spans, links and artifact images; every other
// byte is HTML-escaped, so raw markup can never reach the reader origin.
func renderInline(src string) string {
	var out strings.Builder
	i := 0
	for i < len(src) {
		if src[i] == '`' {
			j := strings.Index(src[i+1:], "`")
			if j < 0 {
				out.WriteString(html.EscapeString(src[i:]))
				break
			}
			out.WriteString("<code>" + html.EscapeString(src[i+1:i+1+j]) + "</code>")
			i += j + 2
			continue
		}
		if src[i] == '!' && i+1 < len(src) && src[i+1] == '[' {
			if label, target, next, ok := parseBracket(src, i+1); ok {
				if isArtifactRef(target) {
					out.WriteString(`<img alt="` + html.EscapeString(label) + `" src="` + html.EscapeString(target) + `"/>`)
				} else {
					out.WriteString(html.EscapeString(label))
				}
				i = next
				continue
			}
		}
		if src[i] == '[' {
			if label, target, next, ok := parseBracket(src, i); ok {
				if isSafeLink(target) {
					out.WriteString(`<a href="` + html.EscapeString(target) + `">` + html.EscapeString(label) + `</a>`)
				} else {
					out.WriteString(html.EscapeString(label))
				}
				i = next
				continue
			}
		}
		j := strings.IndexAny(src[i:], "`![")
		if j < 0 {
			out.WriteString(html.EscapeString(src[i:]))
			break
		}
		if j == 0 {
			out.WriteString(html.EscapeString(src[i : i+1]))
			i++
			continue
		}
		out.WriteString(html.EscapeString(src[i : i+j]))
		i += j
	}
	return out.String()
}

func parseBracket(src string, open int) (label, target string, next int, ok bool) {
	close := strings.Index(src[open+1:], "]")
	if close < 0 {
		return "", "", 0, false
	}
	close += open + 1
	label = src[open+1 : close]
	rest := src[close+1:]
	if !strings.HasPrefix(rest, "(") {
		return "", "", 0, false
	}
	end := strings.Index(rest[1:], ")")
	if end < 0 {
		return "", "", 0, false
	}
	target = rest[1 : 1+end]
	if strings.ContainsAny(target, " \t\n<>\"'") {
		return "", "", 0, false
	}
	return label, target, close + 1 + 1 + end + 1, true
}

func linkScheme(target string) string {
	j := strings.Index(target, ":")
	if j <= 0 {
		return ""
	}
	for _, r := range target[:j] {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '+' || r == '-' || r == '.') {
			return ""
		}
	}
	return strings.ToLower(target[:j])
}

func isSafeLink(target string) bool {
	if strings.HasPrefix(target, "#") {
		return true
	}
	if rest, ok := strings.CutPrefix(target, "https://"); ok {
		return rest != ""
	}
	if rest, ok := strings.CutPrefix(target, "doc:"); ok {
		return rest != ""
	}
	return false
}

func isArtifactRef(target string) bool {
	rest, ok := strings.CutPrefix(strings.ToLower(target), "artifact:")
	return ok && rest != "" && linkScheme(target) == "artifact"
}
