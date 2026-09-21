package openapi

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// protoc-gen-go strips source_code_info from the embedded descriptors, so the
// leading comments of services and RPCs are read from the .proto sources the
// descriptors name. A missing comment is not an error; the summary then falls
// back to the method name.

var (
	protoPackageLine = regexp.MustCompile(`^\s*package\s+([\w.]+)\s*;`)
	protoServiceLine = regexp.MustCompile(`^\s*service\s+(\w+)`)
	protoRPCLine     = regexp.MustCompile(`^\s*rpc\s+(\w+)\s*\(`)
)

// comments maps a service ("pkg.Service") or RPC ("pkg.Service.Method") full
// name to its leading comment, one paragraph per blank comment line.
type comments map[string]string

// readComments scans every file path (relative to protoRoot) for leading
// comments.
func readComments(protoRoot string, paths []string) (comments, error) {
	out := comments{}
	for _, p := range paths {
		f, err := os.Open(filepath.Join(protoRoot, filepath.FromSlash(p)))
		if err != nil {
			return nil, err
		}
		err = scanComments(f, out)
		f.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// scanComments records the leading comment of each service and rpc in r.
func scanComments(r io.Reader, out comments) error {
	var pkg, service string
	var pending []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			pending = append(pending, strings.TrimSpace(strings.TrimPrefix(trimmed, "//")))
			continue
		}
		if m := protoPackageLine.FindStringSubmatch(line); m != nil {
			pkg = m[1]
		} else if m := protoServiceLine.FindStringSubmatch(line); m != nil {
			service = pkg + "." + m[1]
			if text := joinComment(pending); text != "" {
				out[service] = text
			}
		} else if m := protoRPCLine.FindStringSubmatch(line); m != nil && service != "" {
			if text := joinComment(pending); text != "" {
				out[service+"."+m[1]] = text
			}
		}
		pending = nil
	}
	return sc.Err()
}

// joinComment folds comment lines into paragraphs separated by one blank
// line.
func joinComment(lines []string) string {
	var paras []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			var b strings.Builder
			for i, l := range cur {
				// A line broken after a slash ("create/claim/") continues
				// the same token on the next line.
				if i > 0 && !strings.HasSuffix(cur[i-1], "/") {
					b.WriteByte(' ')
				}
				b.WriteString(l)
			}
			paras = append(paras, b.String())
			cur = nil
		}
	}
	for _, l := range lines {
		if l == "" {
			flush()
			continue
		}
		cur = append(cur, l)
	}
	flush()
	return strings.Join(paras, "\n\n")
}

// summaryOf returns the first sentence of a comment, or fallback when the
// comment is empty. Summaries longer than maxSummary are cut at a word
// boundary.
func summaryOf(comment, fallback string) string {
	const maxSummary = 120
	if comment == "" {
		return fallback
	}
	first := strings.SplitN(comment, "\n\n", 2)[0]
	if i := strings.Index(first, ". "); i >= 0 {
		first = first[:i+1]
	}
	if len(first) <= maxSummary {
		return first
	}
	cut := first[:maxSummary]
	if i := strings.LastIndex(cut, " "); i > 0 {
		cut = cut[:i]
	}
	return cut + "..."
}
