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

// Unexcused returns findings without an exact file, line, and method exception.
func Unexcused(findings []Finding) []Finding {
	covered := make(map[string]bool)
	for _, e := range Exceptions {
		for _, call := range e.Calls {
			covered[exceptionKey(e.File, call.Line, call.Method)] = true
		}
	}
	var out []Finding
	for _, f := range findings {
		if !covered[exceptionKey(f.File, f.Line, f.Method)] {
			out = append(out, f)
		}
	}
	return out
}

// MissingExceptions returns pinned entries that no longer match a scanned
// operation, preventing stale allowances from silently accumulating.
func MissingExceptions(findings []Finding) []string {
	remaining := make(map[string]int)
	for _, e := range Exceptions {
		for _, call := range e.Calls {
			remaining[exceptionKey(e.File, call.Line, call.Method)]++
		}
	}
	for _, f := range findings {
		key := exceptionKey(f.File, f.Line, f.Method)
		if remaining[key] > 0 {
			remaining[key]--
		}
	}
	var out []string
	for key, count := range remaining {
		if count > 0 {
			out = append(out, fmt.Sprintf("%s (unused %d)", key, count))
		}
	}
	sort.Strings(out)
	return out
}

// CanonicalExceptions renders the exception table one file-per-line with its
// reason, for the golden pin.
func CanonicalExceptions() string {
	lines := make([]string, 0)
	for _, e := range Exceptions {
		for _, call := range e.Calls {
			lines = append(lines, fmt.Sprintf("%s:%d:%s :: %s", e.File, call.Line, call.Method, e.Reason))
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func exceptionKey(file string, line int, method string) string {
	return fmt.Sprintf("%s:%d:%s", file, line, method)
}
