//go:build js && wasm

package main

import (
	"syscall/js"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestTodo_I18N_005_Browser(t *testing.T) {
	oldDocument := js.Global().Get("document")
	root := js.Global().Get("Object").New()
	attributes := map[string]string{
		"lang": "ar", "dir": "rtl", "data-hcm-locale": "ar", "data-hcm-catalog": "product-ui.v1",
	}
	setAttribute := js.FuncOf(func(_ js.Value, args []js.Value) any {
		attributes[args[0].String()] = args[1].String()
		return nil
	})
	removeAttribute := js.FuncOf(func(_ js.Value, args []js.Value) any {
		delete(attributes, args[0].String())
		return nil
	})
	root.Set("setAttribute", setAttribute)
	root.Set("removeAttribute", removeAttribute)
	document := js.Global().Get("Object").New()
	document.Set("documentElement", root)
	js.Global().Set("document", document)
	t.Cleanup(func() {
		js.Global().Set("document", oldDocument)
		setAttribute.Release()
		removeAttribute.Release()
	})

	// These are the identity attributes already present in the authenticated
	// server document before the WASM loader adopts the same stored preference.
	serverLang, serverDir := attributes["lang"], attributes["dir"]
	hydrated := productui.ResolveProductLocalePreference("", "ar")
	applyLocaleDocumentIdentity(hydrated)
	if attributes["lang"] != serverLang || attributes["dir"] != serverDir {
		t.Fatalf("hydration changed document identity from %s/%s to %s/%s", serverLang, serverDir, attributes["lang"], attributes["dir"])
	}
	if hydrated.Resolved != "ar" || hydrated.Direction != "rtl" || attributes["data-hcm-locale"] != "ar" {
		t.Fatalf("hydrated Arabic locale = %+v, html attributes=%v", hydrated, attributes)
	}
	forged := productui.ResolveProductLocalePreference("tenant-admin", "ar")
	applyLocaleDocumentIdentity(forged)
	if forged.Resolved != "ar" || forged.Direction != "rtl" || attributes["lang"] != serverLang || attributes["dir"] != serverDir || attributes["data-hcm-locale-fallback"] != "unsupported_locale" {
		t.Fatalf("unsupported route locale changed hydrated preference identity: locale=%+v attributes=%v", forged, attributes)
	}

	// The URL remains the explicit override source after hydration.
	if got := productui.ResolveProductLocalePreference("de-DE", "ar"); got.Resolved != "de-DE" || got.Direction != "ltr" {
		t.Fatalf("explicit URL locale did not keep precedence: %+v", got)
	}
}
