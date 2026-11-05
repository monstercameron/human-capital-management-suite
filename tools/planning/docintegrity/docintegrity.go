// Package docintegrity validates the normative planning-document corpus
// (DOC-001). It is deliberately read-only: checks consume Markdown and the
// generated todo heading index, and never rewrite planning artifacts.
package docintegrity

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Diagnostic is a deterministic planning-document finding.
type Diagnostic struct {
	File, Code, Message string
	Line                int
}

func (d Diagnostic) String() string {
	if d.Line > 0 {
		return fmt.Sprintf("%s:%d: %s: %s", d.File, d.Line, d.Code, d.Message)
	}
	return fmt.Sprintf("%s: %s: %s", d.File, d.Code, d.Message)
}

var todoID = regexp.MustCompile(`- \[[ xX]\] ` + "`" + `([^` + "`" + `]+)` + "`")
var markdownLink = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)
var fence = regexp.MustCompile(`^\s*(` + "`" + `{3,}|~{3,})(.*)$`)
var mojibake = []string{"�", "Ã", "Â", "â€", "ðŸ", "ï»¿"}

// Check validates planning/**/*.md, README.md, and the generated heading
// index. The returned findings are sorted by file, line, and code.
func Check(root string) ([]Diagnostic, error) {
	files, err := markdownFiles(root)
	if err != nil {
		return nil, err
	}
	contents := make(map[string][]byte, len(files))
	var out []Diagnostic
	for _, file := range files {
		b, e := os.ReadFile(file)
		if e != nil {
			return nil, e
		}
		contents[file] = b
		out = append(out, checkBytes(file, b)...)
	}
	for _, file := range files {
		out = append(out, checkLinks(file, contents[file], contents)...)
	}
	out = append(out, checkDuplicateIDs(files, contents)...)
	out = append(out, checkGeneratedIndex(root, contents)...)
	sortDiagnostics(out)
	return out, nil
}

// CheckMarkdown validates one in-memory Markdown document. It is useful for
// unit tests and mutation/property tests that must not touch the corpus.
func CheckMarkdown(file, markdown string) []Diagnostic { return checkBytes(file, []byte(markdown)) }

func markdownFiles(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(filepath.Join(root, "planning"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk planning: %w", err)
	}
	readme := filepath.Join(root, "README.md")
	if _, err := os.Stat(readme); err == nil {
		files = append(files, readme)
	}
	sort.Strings(files)
	return files, nil
}

func checkBytes(file string, b []byte) []Diagnostic {
	var out []Diagnostic
	if !utf8.Valid(b) {
		out = append(out, Diagnostic{file, "INVALID_UTF8", "document is not valid UTF-8", 1})
	}
	lines := strings.Split(string(b), "\n")
	open := false
	opener := ""
	ascii := false
	for i, line := range lines {
		n := i + 1
		if i == 0 && strings.HasPrefix(line, "\ufeff") {
			out = append(out, Diagnostic{file, "UTF8_BOM", "UTF-8 BOM is not permitted", n})
		}
		for _, marker := range mojibake {
			if strings.Contains(line, marker) {
				out = append(out, Diagnostic{file, "MOJIBAKE", fmt.Sprintf("mojibake marker %q", marker), n})
				break
			}
		}
		if m := fence.FindStringSubmatch(line); m != nil {
			if !open {
				open = true
				opener = m[1]
				ascii = strings.EqualFold(strings.TrimSpace(m[2]), "ascii")
			} else if strings.HasPrefix(strings.TrimSpace(line), opener[:3]) {
				open = false
				ascii = false
			}
			continue
		}
		if open && ascii && !utf8.Valid([]byte(line)) || open && ascii && !isASCII(line) {
			out = append(out, Diagnostic{file, "INVALID_ASCII_BLOCK", "ASCII fenced block contains non-ASCII text", n})
		}
	}
	if open {
		out = append(out, Diagnostic{file, "UNBALANCED_FENCE", "fenced code block is not closed", len(lines)})
	}
	return out
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

func checkLinks(file string, b []byte, all map[string][]byte) []Diagnostic {
	var out []Diagnostic
	lines := strings.Split(string(b), "\n")
	inFence := false
	for i, line := range lines {
		if fence.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, m := range markdownLink.FindAllStringSubmatch(line, -1) {
			dest := strings.Trim(m[1], "<>")
			if strings.HasPrefix(dest, "http://") || strings.HasPrefix(dest, "https://") || strings.HasPrefix(dest, "mailto:") {
				continue
			}
			parts := strings.SplitN(dest, "#", 2)
			rel := parts[0]
			anchor := ""
			if len(parts) == 2 {
				anchor = parts[1]
			}
			target := file
			if rel != "" {
				target = filepath.Clean(filepath.Join(filepath.Dir(file), rel))
			}
			data, ok := all[target]
			if !ok {
				var e error
				data, e = os.ReadFile(target)
				ok = e == nil
			}
			if !ok {
				out = append(out, Diagnostic{file, "BROKEN_LOCAL_LINK", fmt.Sprintf("target %q does not exist", dest), i + 1})
				continue
			}
			if anchor != "" && !hasAnchor(data, anchor) {
				out = append(out, Diagnostic{file, "BROKEN_LOCAL_ANCHOR", fmt.Sprintf("anchor %q not found in %q", anchor, dest), i + 1})
			}
		}
	}
	return out
}

func hasAnchor(b []byte, want string) bool {
	counts := map[string]int{}
	for _, line := range strings.Split(string(b), "\n") {
		s := strings.TrimSpace(line)
		if !strings.HasPrefix(s, "#") {
			continue
		}
		h := strings.TrimSpace(strings.TrimLeft(s, "#"))
		a := anchor(h)
		n := counts[a]
		counts[a]++
		if a == want || (n > 0 && fmt.Sprintf("%s-%d", a, n) == want) {
			return true
		}
	}
	return false
}
func anchor(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else if r == ' ' {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func checkDuplicateIDs(files []string, all map[string][]byte) []Diagnostic {
	seen := map[string]Diagnostic{}
	var out []Diagnostic
	for _, f := range files {
		for i, line := range strings.Split(string(all[f]), "\n") {
			m := todoID.FindStringSubmatch(line)
			if len(m) == 0 {
				continue
			}
			d := Diagnostic{f, "DUPLICATE_NORMATIVE_ID", fmt.Sprintf("normative ID %q already declared at %s:%d", m[1], seen[m[1]].File, seen[m[1]].Line), i + 1}
			if p, ok := seen[m[1]]; ok {
				_ = p
				out = append(out, d)
			} else {
				seen[m[1]] = Diagnostic{f, "", "", i + 1}
			}
		}
	}
	return out
}

func checkGeneratedIndex(root string, all map[string][]byte) []Diagnostic {
	path := filepath.Join(root, "planning", "todo-headings.txt")
	b, err := os.ReadFile(path)
	if err != nil {
		return []Diagnostic{{path, "STALE_GENERATED_INDEX", "generated todo heading index is missing", 1}}
	}
	src := all[filepath.Join(root, "planning", "archive", "todos-2026-10-22.md")]
	want := []string{}
	for i, l := range strings.Split(string(src), "\n") {
		if todoID.MatchString(l) || strings.HasPrefix(l, "## ") {
			want = append(want, fmt.Sprintf("%d:%s", i+1, l))
		}
	}
	got := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if strings.Join(want, "\n") != strings.Join(got, "\n") {
		return []Diagnostic{{path, "STALE_GENERATED_INDEX", fmt.Sprintf("index has %d entries; current corpus generates %d", len(got), len(want)), 1}}
	}
	return nil
}

func sortDiagnostics(ds []Diagnostic) {
	sort.SliceStable(ds, func(i, j int) bool {
		if ds[i].File != ds[j].File {
			return ds[i].File < ds[j].File
		}
		if ds[i].Line != ds[j].Line {
			return ds[i].Line < ds[j].Line
		}
		return ds[i].Code < ds[j].Code
	})
}
