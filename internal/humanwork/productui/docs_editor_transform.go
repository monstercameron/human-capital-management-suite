package productui

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// docsEditorTransform applies one toolbar command to Markdown text with a
// selection given as byte offsets, and returns the new text and selection.
// Both panes use it: the Markdown pane passes its own text and selection,
// the formatted pane passes its serialized text with the selection carried
// through by marks. arg is the link address for "link" and the column
// label for "table".
func docsEditorTransform(value string, start, end int, command, arg string) (string, int, int) {
	start = docsEditorClamp(value, start)
	end = docsEditorClamp(value, end)
	if end < start {
		start, end = end, start
	}
	switch command {
	case "bold":
		return docsEditorToggleWrap(value, start, end, "**", nil)
	case "italic":
		return docsEditorToggleWrap(value, start, end, "_", []string{"*"})
	case "strike":
		return docsEditorToggleWrap(value, start, end, "~~", nil)
	case "code":
		return docsEditorToggleWrap(value, start, end, "`", nil)
	case "link":
		return docsEditorInsertLink(value, start, end, arg)
	case "h0", "h1", "h2", "h3", "h4", "h5", "h6":
		return docsEditorSetHeading(value, start, end, int(command[1]-'0'))
	case "ul", "ol", "task", "quote":
		return docsEditorToggleLines(value, start, end, command)
	case "codeblock":
		return docsEditorCodeBlock(value, start, end)
	case "table":
		label := strings.TrimSpace(arg)
		if label == "" {
			label = "Column"
		}
		cells := []string{label + " 1", label + " 2", label + " 3"}
		block := "| " + strings.Join(cells, " | ") + " |\n| --- | --- | --- |\n|  |  |  |\n|  |  |  |"
		next, at := docsEditorInsertBlock(value, end, block)
		return next, at + 2, at + 2 + len(cells[0])
	case "hr":
		next, at := docsEditorInsertBlock(value, end, "---")
		caret := min(at+len("---\n\n"), len(next))
		return next, caret, caret
	}
	return value, start, end
}

// docsEditorToggleWrap puts inline delimiters around the selection, or
// takes them away when the selection already sits inside (or includes) a
// pair. Whitespace at the selection's edges stays outside the delimiters.
func docsEditorToggleWrap(value string, start, end int, delimiter string, alternates []string) (string, int, int) {
	for _, d := range append([]string{delimiter}, alternates...) {
		n := len(d)
		if start >= n && end+n <= len(value) && value[start-n:start] == d && value[end:end+n] == d && !docsEditorDoubled(value, start-n, end+n, d) {
			return value[:start-n] + value[start:end] + value[end+n:], start - n, end - n
		}
		selected := value[start:end]
		if len(selected) >= 2*n && strings.HasPrefix(selected, d) && strings.HasSuffix(selected, d) && !docsEditorDoubled(value, start, end, d) {
			inner := selected[n : len(selected)-n]
			return value[:start] + inner + value[end:], start, start + len(inner)
		}
	}
	core := value[start:end]
	lead := len(core) - len(strings.TrimLeft(core, " \t"))
	core = strings.TrimLeft(core, " \t")
	trimmed := strings.TrimRight(core, " \t")
	trail := len(core) - len(trimmed)
	core = trimmed
	d := delimiter
	if d == "_" && (docsEditorWordBefore(value, start+lead) || docsEditorWordAfter(value, end-trail)) {
		// Underscores do not emphasise inside a word; asterisks do.
		d = "*"
	}
	pad := 0
	if d == "`" && strings.Contains(core, "`") {
		d = "``"
		core = " " + core + " "
		pad = 1
	}
	from := start + lead
	to := end - trail
	next := value[:from] + d + core + d + value[to:]
	return next, from + len(d) + pad, from + len(d) + len(core) - pad
}

// docsEditorDoubled reports whether a single-character delimiter found at
// the edges is really half of a longer run ("*" inside "**bold**").
func docsEditorDoubled(value string, open, close int, d string) bool {
	if len(d) != 1 {
		return false
	}
	c := d[0]
	return (open > 0 && value[open-1] == c) || (close < len(value) && value[close] == c)
}

func docsEditorWordBefore(value string, at int) bool {
	r, _ := utf8.DecodeLastRuneInString(value[:at])
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func docsEditorWordAfter(value string, at int) bool {
	r, _ := utf8.DecodeRuneInString(value[at:])
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func docsEditorInsertLink(value string, start, end int, href string) (string, int, int) {
	href = strings.TrimSpace(href)
	label := value[start:end]
	if strings.TrimSpace(label) == "" || strings.Contains(label, "\n") {
		label = docsEditorEscapeLinkText(href)
		if strings.Contains(value[start:end], "\n") {
			start = end
		}
	}
	link := "[" + label + "](" + docsEditorDestination(href) + ")"
	next := value[:start] + link + value[end:]
	return next, start + 1, start + 1 + len(label)
}

// docsEditorLineSpan widens a selection to whole lines. A selection that
// ends at the very start of a line does not take that line.
func docsEditorLineSpan(value string, start, end int) (int, int) {
	lineStart := strings.LastIndexByte(value[:start], '\n') + 1
	if end > start && end > 0 && value[end-1] == '\n' {
		end--
	}
	lineEnd := strings.IndexByte(value[end:], '\n')
	if lineEnd < 0 {
		lineEnd = len(value)
	} else {
		lineEnd += end
	}
	return lineStart, max(lineEnd, lineStart)
}

// docsEditorReplaceLines rewrites every selected line and maps the
// selection through the change: a caret stays on its text, a range covers
// the rewritten lines.
func docsEditorReplaceLines(value string, start, end int, rewrite func(lines []string) []string) (string, int, int) {
	from, to := docsEditorLineSpan(value, start, end)
	lines := strings.Split(value[from:to], "\n")
	rewritten := rewrite(append([]string(nil), lines...))
	block := strings.Join(rewritten, "\n")
	next := value[:from] + block + value[to:]
	if start == end && len(lines) == 1 {
		oldPrefix := docsEditorLinePrefixLength(lines[0])
		newPrefix := docsEditorLinePrefixLength(rewritten[0])
		caret := max(start-from, oldPrefix) - oldPrefix + newPrefix
		caret = from + min(caret, len(rewritten[0]))
		return next, caret, caret
	}
	return next, from, from + len(block)
}

// docsEditorSplitLine separates a line into its indentation, its quote
// and list markers, and its text.
func docsEditorSplitLine(line string) (indent, quote, list, rest string) {
	i := 0
	for i < len(line) && line[i] == ' ' {
		i++
	}
	indent = line[:i]
	j := i
	for j < len(line) && line[j] == '>' {
		j++
		if j < len(line) && line[j] == ' ' {
			j++
		}
	}
	quote = line[i:j]
	k := j
	if n := docsEditorListMarkerLength(line[k:]); n > 0 {
		k += n
		if strings.HasPrefix(line[k:], "[ ] ") || strings.HasPrefix(line[k:], "[x] ") || strings.HasPrefix(line[k:], "[X] ") {
			k += 4
		}
	}
	list = line[j:k]
	rest = line[k:]
	return indent, quote, list, rest
}

func docsEditorListKind(marker string) string {
	switch {
	case marker == "":
		return ""
	case strings.HasSuffix(marker, "] "):
		return "task"
	case marker[0] >= '0' && marker[0] <= '9':
		return "ol"
	default:
		return "ul"
	}
}

func docsEditorSetHeading(value string, start, end, level int) (string, int, int) {
	return docsEditorReplaceLines(value, start, end, func(lines []string) []string {
		// Choosing the level a line already has takes the heading away.
		same := level > 0
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			if _, _, _, rest := docsEditorSplitLine(line); docsEditorHeadingMarkerLength(rest) != level+1 {
				same = false
			}
		}
		target := level
		if same {
			target = 0
		}
		for i, line := range lines {
			indent, quote, list, rest := docsEditorSplitLine(line)
			if n := docsEditorHeadingMarkerLength(rest); n > 0 {
				rest = rest[n:]
			}
			if target > 0 && (strings.TrimSpace(rest) != "" || len(lines) == 1) {
				rest = strings.Repeat("#", target) + " " + rest
			}
			lines[i] = indent + quote + list + rest
		}
		return lines
	})
}

func docsEditorToggleLines(value string, start, end int, command string) (string, int, int) {
	return docsEditorReplaceLines(value, start, end, func(lines []string) []string {
		if command == "quote" {
			all := true
			for _, line := range lines {
				if strings.TrimSpace(line) != "" && !strings.HasPrefix(strings.TrimLeft(line, " "), ">") {
					all = false
				}
			}
			for i, line := range lines {
				if all {
					trimmed := strings.TrimLeft(line, " ")
					trimmed = strings.TrimPrefix(trimmed, ">")
					lines[i] = strings.TrimPrefix(trimmed, " ")
				} else if strings.TrimSpace(line) == "" {
					lines[i] = ">"
				} else {
					lines[i] = "> " + line
				}
			}
			return lines
		}
		all := true
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			_, _, list, _ := docsEditorSplitLine(line)
			if docsEditorListKind(list) != command {
				all = false
			}
		}
		number := 1
		for i, line := range lines {
			indent, quote, list, rest := docsEditorSplitLine(line)
			if strings.TrimSpace(line) == "" && len(lines) > 1 {
				continue
			}
			if heading := docsEditorHeadingMarkerLength(rest); heading > 0 && !all {
				rest = rest[heading:]
			}
			switch {
			case all:
				list = ""
			case command == "ul":
				list = "- "
			case command == "task":
				list = "- [ ] "
			case command == "ol":
				list = strconv.Itoa(number) + ". "
				number++
			}
			lines[i] = indent + quote + list + rest
		}
		return lines
	})
}

func docsEditorCodeBlock(value string, start, end int) (string, int, int) {
	from, to := docsEditorLineSpan(value, start, end)
	body := value[from:to]
	lines := strings.Split(body, "\n")
	if len(lines) >= 2 && strings.HasPrefix(strings.TrimSpace(lines[0]), "```") && strings.TrimSpace(lines[len(lines)-1]) == "```" {
		inner := strings.Join(lines[1:len(lines)-1], "\n")
		return value[:from] + inner + value[to:], from, from + len(inner)
	}
	block := "```\n" + body + "\n```"
	next := value[:from] + block + value[to:]
	if strings.TrimSpace(body) == "" {
		return next, from + 4, from + 4
	}
	return next, from + 4, from + 4 + len(body)
}

// docsEditorInsertBlock puts a block after the line holding at, with one
// blank line either side, and returns where the block starts.
func docsEditorInsertBlock(value string, at int, block string) (string, int) {
	lineEnd := strings.IndexByte(value[at:], '\n')
	if lineEnd < 0 {
		lineEnd = len(value)
	} else {
		lineEnd += at
	}
	lineStart := strings.LastIndexByte(value[:at], '\n') + 1
	before := value[:lineEnd]
	after := value[lineEnd:]
	if strings.TrimSpace(value[lineStart:lineEnd]) == "" {
		before = value[:lineStart]
		after = value[lineEnd:]
	}
	before = strings.TrimRight(before, "\n")
	after = strings.TrimLeft(after, "\n")
	prefix := before
	if before != "" {
		prefix += "\n\n"
	}
	suffix := "\n"
	if after != "" {
		suffix = "\n\n" + after
	}
	return prefix + block + suffix, len(prefix)
}

// docsEditorInsertFragment puts pasted Markdown in place of the selection.
// A fragment that is more than a run of text (several paragraphs, a list,
// a table) goes in as blocks of its own, splitting the paragraph around
// the selection; a run of text goes in where the caret is. The caret ends
// after the fragment.
func docsEditorInsertFragment(value string, start, end int, fragment string) (string, int, int) {
	start = docsEditorClamp(value, start)
	end = max(start, docsEditorClamp(value, end))
	fragment = strings.Trim(fragment, "\n")
	before, after := value[:start], value[end:]
	if !docsEditorIsBlockFragment(fragment) {
		next := before + fragment + after
		return next, len(before) + len(fragment), len(before) + len(fragment)
	}
	before = strings.TrimRight(before, " \n")
	after = strings.TrimLeft(after, " \n")
	if before != "" {
		before += "\n\n"
	}
	next := before + fragment
	caret := len(next)
	if after != "" {
		next += "\n\n" + after
	} else {
		next += "\n"
	}
	return next, caret, caret
}

func docsEditorIsBlockFragment(fragment string) bool {
	if strings.Contains(fragment, "\n\n") {
		return true
	}
	for _, line := range strings.Split(fragment, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if docsEditorLinePrefixLength(line) > 0 || strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "|") || trimmed == "---" {
			return true
		}
	}
	return false
}

// docsEditorFormatsOfAncestors names the formatting around the caret in
// the formatted pane from the elements that contain it, innermost first.
func docsEditorFormatsOfAncestors(ancestors []*docsEditorNode) map[string]bool {
	out := map[string]bool{}
	list := ""
	for _, node := range ancestors {
		switch node.Tag {
		case "strong", "b":
			out["bold"] = true
		case "em", "i":
			out["italic"] = true
		case "s", "del", "strike":
			out["strike"] = true
		case "code":
			out["code"] = true
		case "a":
			out["link"] = true
		case "h1", "h2", "h3", "h4", "h5", "h6":
			out[node.Tag] = true
		case "li":
			if list == "" && (node.hasClass("docs-editor-task") || docsEditorHasTaskBox(node)) {
				list = "task"
			}
		case "ul", "ol":
			if list == "" {
				list = node.Tag
			}
		case "blockquote":
			out["quote"] = true
		case "pre":
			out["codeblock"] = true
			delete(out, "code")
		}
	}
	if list != "" {
		out[list] = true
	}
	return out
}

func docsEditorHasTaskBox(li *docsEditorNode) bool {
	for _, child := range li.Children {
		if child.Tag == "input" && strings.EqualFold(child.attr("type"), "checkbox") {
			return true
		}
	}
	return false
}
