package productui

import "strings"

// productIconSVGAttrs and productIconPathAttrs are built once and only read.
// html.Tag copies a Raw map into the element's own props, so sharing them
// saves two map allocations per icon per render; a document list row draws
// up to eight icons (star, folder, access, the row menu and its items).
var productIconSVGAttrs = map[string]any{
	"viewBox": "0 0 24 24", "fill": "none", "stroke": "currentColor", "stroke-width": "1.8",
	"stroke-linecap": "round", "stroke-linejoin": "round", "aria-hidden": "true", "focusable": "false",
}

var productIconPathAttrs = func() map[string]map[string]any {
	out := make(map[string]map[string]any, len(registeredIcons))
	for _, definition := range registeredIcons {
		if _, ok := out[definition.Name]; !ok {
			out[definition.Name] = map[string]any{"d": definition.Path}
		}
	}
	return out
}()

var productIconFallbackAttrs = map[string]any{"d": fallbackIconPath}

// productIconPathAttr is the shared path attribute map for a registered
// icon name, or the fallback ring for an unknown one.
func productIconPathAttr(name string) map[string]any {
	if attrs, ok := productIconPathAttrs[name]; ok {
		return attrs
	}
	return productIconFallbackAttrs
}

// docsTypedSearching reports whether the search box holds a query, the one
// fact about the typed text that the document list renders from.
func docsTypedSearching(typed string) bool {
	return strings.TrimSpace(typed) != ""
}
