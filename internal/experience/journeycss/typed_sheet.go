package journeycss

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

// mediaRule scopes parts inside one @media query. The single-spread form
// keeps every call site clear of fixed-arg-plus-spread mixing.
func mediaRule(query gwccss.MediaQuery, parts ...any) []gwccss.Rule {
	return gwccss.Media(query, gwccss.Rules(parts...)...)
}
