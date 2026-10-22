package values

import (
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// NFKC applies Unicode compatibility composition. It is a text mechanic, not
// a field policy; callers choose where compatibility normalization is valid.
func NFKC(s string) string { return norm.NFKC.String(s) }

// NFKD applies Unicode compatibility decomposition. It is a text mechanic,
// not a field policy; callers choose how to handle the decomposed runes.
func NFKD(s string) string { return norm.NFKD.String(s) }

// CaseFolder applies full Unicode case folding and reuses its internal caser
// across calls. Keep a folder local to one operation; it is not safe for
// concurrent use.
type CaseFolder struct {
	caser       cases.Caser
	initialized bool
}

// Fold returns the full Unicode case fold of s. The zero value is ready to
// use and initializes its caser only when first needed.
func (f *CaseFolder) Fold(s string) string {
	if !f.initialized {
		f.caser = cases.Fold()
		f.initialized = true
	}
	return f.caser.String(s)
}

// CaseFold returns the full Unicode case fold of s using a fresh per-call
// folder. Repeated operations can use CaseFolder to reuse the caser.
func CaseFold(s string) string {
	var folder CaseFolder
	return folder.Fold(s)
}
