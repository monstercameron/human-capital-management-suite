package rowbatch

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// scanOneFile reads one file and scans it.
func scanOneFile(path, rel string) ([]Finding, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("rowbatch: read %s: %w", rel, err)
	}
	return ScanFile(rel, src)
}

// Unexcused returns the findings no exception covers. An exception covers
// every finding in its file: the file stays only with its reason recorded.
func Unexcused(findings []Finding) []Finding {
	covered := make(map[string]bool, len(Exceptions))
	for _, e := range Exceptions {
		covered[e.File] = true
	}
	var out []Finding
	for _, f := range findings {
		if !covered[f.File] {
			out = append(out, f)
		}
	}
	return out
}

// CanonicalExceptions renders the exception table one file-per-line with its
// reason, for the golden pin.
func CanonicalExceptions() string {
	lines := make([]string, 0, len(Exceptions))
	for _, e := range Exceptions {
		lines = append(lines, e.File+" :: "+e.Reason)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
