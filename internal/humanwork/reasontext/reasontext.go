// Package reasontext holds the one rule for telling a machine token from a
// sentence in free-text reason fields. It is a leaf package on purpose: the
// wasm journey client imports it, so it must not pull server packages (and
// their package-level stylesheet builds) into the browser binary.
package reasontext

import "strings"

// TokenShaped reports whether a free-text value is a machine token rather
// than prose: no whitespace, and separated by the underscores or hyphens an
// identifier uses. A single word is not enough -- "Reorganisation" is prose --
// so a separator is required.
//
// It is the one definition of "token-shaped": the proposal write path refuses
// such a business reason before anything is recorded, and the journey
// display formatter sets an already-stored one as an identifier.
func TokenShaped(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.ContainsAny(trimmed, " \t\n\r") {
		return false
	}
	return strings.ContainsAny(trimmed, "_-")
}
