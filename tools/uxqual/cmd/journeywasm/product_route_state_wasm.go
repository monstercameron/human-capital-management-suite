//go:build js && wasm

package main

import (
	"net/url"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/router"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// productRouteLocale is the language for a route that has no page of its own:
// the last page's language when there was one, otherwise the address's.
func productRouteLocale() productui.LocaleContext {
	if lastResolvedProductView != nil && lastResolvedProductView.Locale.Resolved != "" {
		return lastResolvedProductView.Locale
	}
	values, _ := url.ParseQuery(strings.TrimPrefix(currentQuery(), "?"))
	return productui.ResolveProductLocale(values.Get("locale"))
}

type productRouteStateProps struct{ Error string }

// productRouteNotFoundComponent is the route for an address under the product
// that no page owns. It shows the product's ordinary not-found state, with the
// shell around it but without the previous page's heading.
func productRouteNotFoundComponent(_ router.Attrs) *router.Element {
	setActiveProductLayout(activeProductLayoutView, false)
	return ui.CreateElement(renderProductRouteNotFound, productRouteStateProps{})
}

func renderProductRouteNotFound(productRouteStateProps) ui.Node {
	return productui.RouteNotFound(productRouteLocale())
}

// productRouteErrorComponent is every product route's failed-read state. A
// route loader's error is an internal fact ("context canceled", a parse
// failure, a transport error) and is never printed; the one message a loader
// returns on purpose, the denied journey detail, is localized product copy and
// is recognised by its text.
func productRouteErrorComponent(attrs router.Attrs) *router.Element {
	raw, _ := attrs["error"].(string)
	setActiveProductLayout(activeProductLayoutView, false)
	return ui.CreateElement(renderProductRouteError, productRouteStateProps{Error: raw})
}

func renderProductRouteError(props productRouteStateProps) ui.Node {
	locale := productRouteLocale()
	var retry func()
	if productRouteRetry != nil {
		retry = retryProductRoute
	}
	detail := ""
	if props.Error != "" && props.Error == locale.Text("journey.error_denied_detail") {
		detail = props.Error
	}
	return productui.RouteLoadFailed(locale, retry, detail)
}
