package tokens

import (
	"sync"

	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

// typedSheetMu serializes typed-stylesheet builds. The css package emits into
// a process-wide, content-deduped sink, so each stylesheet is built as one
// atomic Reset -> declare -> Harvest sequence; without the mutex, another
// package's declarations could land in this sheet depending on call order.
var typedSheetMu sync.Mutex

// buildTypedSheet runs declare and returns exactly the CSS it emitted, in
// declaration order. Declarations must be deterministic: same calls, same
// bytes, so the CSP hash over the final stylesheet is stable.
func buildTypedSheet(declare func()) string {
	typedSheetMu.Lock()
	defer typedSheetMu.Unlock()
	gwccss.Reset()
	declare()
	return gwccss.Harvest()
}

// declareGlobal emits one literal selector. Variant helpers (Hover, Media,
// …) return rule slices, so every call funnels through Rules, which accepts
// both single rules and slices.
func declareGlobal(selector string, parts ...any) {
	gwccss.Global(selector, gwccss.Rules(parts...)...)
}

// atRule wraps harvested typed CSS in an at-rule header the GWC css API has
// no constructor for (@supports, @starting-style), or in an @media header
// that must wrap several selectors in ONE block (per-selector mediaRule
// would emit one @media block per selector). The inner declarations are
// fully typed; only the header keyword is literal, pending upstream support.
func atRule(header, inner string) string {
	return header + "{" + inner + "}"
}
