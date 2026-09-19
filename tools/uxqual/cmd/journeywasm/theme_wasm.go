//go:build js && wasm

package main

import (
	"reflect"
	"strconv"
	"strings"
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

type browserThemeController struct {
	saved productui.CustomerTheme
	// loaded reports whether saved holds the customer's real selection. Until
	// the first Load it holds only DefaultCustomerTheme -- a placeholder, not
	// a choice -- and the <html> attributes the server rendered from the
	// stored theme are the better answer. See Reapply.
	loaded    bool
	tenant    string
	save      func(productui.CustomerTheme, func(error))
	logoLoad  js.Func
	logoError js.Func
}

func newBrowserThemeController(tenant string, save func(productui.CustomerTheme, func(error))) *browserThemeController {
	controller := &browserThemeController{saved: productui.DefaultCustomerTheme(), tenant: tenant, save: save}
	controller.logoLoad = js.FuncOf(func(_ js.Value, args []js.Value) any {
		setBrandLogoEventState(args, "configured")
		return nil
	})
	controller.logoError = js.FuncOf(func(_ js.Value, args []js.Value) any {
		setBrandLogoEventState(args, "fallback")
		return nil
	})
	return controller
}

func (c *browserThemeController) Load(theme productui.CustomerTheme) {
	if c != nil {
		c.saved = productui.NormalizeCustomerTheme(theme)
		c.loaded = true
		c.Apply(c.saved)
	}
}

// Reapply restores the last known customer selection, discarding any unsaved
// preview. Before the first Load it does nothing, and that is the fix for the
// theme flashing on every page load.
//
// The server reads the stored theme and renders it into <html> -- a light
// workspace arrives with data-hcm-color-mode="light" and first paints light.
// This controller used to start from DefaultCustomerTheme, whose colour mode
// is "system", and the boot sequence applied that immediately, overwriting the
// server's correct answer. On a device set to dark mode "system" resolves to
// dark, so a light workspace turned dark the moment the client started and
// only turned light again when the page's data read finished and Load ran.
// Measured on a warm load that window was 634ms; it is as long as the page's
// whole projection read, and every in-app link is a full document load, so it
// happened on every navigation.
//
// The server-rendered attributes are the truth until the client has something
// newer. A client that knows nothing yet has no business replacing them.
func (c *browserThemeController) Reapply() {
	if c != nil && c.loaded {
		c.Apply(c.saved)
	}
}

func (c *browserThemeController) Saved() productui.CustomerTheme {
	if c == nil {
		return productui.DefaultCustomerTheme()
	}
	return productui.NormalizeCustomerTheme(c.saved)
}

func (c *browserThemeController) Preview(theme productui.CustomerTheme) {
	c.Apply(theme)
	c.syncLogoReference(theme.BrandLogoURL)
	dirty := c.syncEditContext(theme)
	if theme.Palette == "custom" {
		if err := productui.ValidateCustomerTheme(theme); err != nil {
			c.setStatus(appearanceThemeFeedback(err), "warning")
			c.setSaveDisabled(true)
		} else {
			c.setStatus(appearanceThemeText("appearance.custom_valid"), "preview")
			c.setSaveDisabled(!dirty)
		}
		input := js.Global().Get("document").Call("querySelector", `input[name="palette"][value="custom"]`)
		if input.Truthy() {
			input.Set("checked", true)
		}
		if err := productui.ValidateCustomerTheme(theme); err == nil {
			c.syncColorWells(theme)
		}
		return
	}
	c.syncColorWells(theme)
	c.setSaveDisabled(!dirty)
	if dirty {
		c.setStatus(appearanceThemeText("appearance.preview_unsaved"), "preview")
	} else {
		c.setStatus(appearanceThemeText("appearance.no_changes"), "preview")
	}
}

func (c *browserThemeController) syncLogoReference(value string) {
	input := js.Global().Get("document").Call("querySelector", `input[name="brand_logo_url"]`)
	if input.Truthy() && input.Get("value").String() != value {
		input.Set("value", value)
	}
}

func (c *browserThemeController) Save(theme productui.CustomerTheme) {
	if c == nil {
		return
	}
	theme = productui.NormalizeCustomerTheme(theme)
	if err := productui.ValidateCustomerTheme(theme); err != nil {
		c.setStatus(appearanceThemeFeedback(err), "warning")
		c.setSaveDisabled(true)
		return
	}
	previousPalette := c.saved.Palette
	c.loaded = true
	c.Apply(theme)
	c.syncEditContext(theme)
	c.setSaveDisabled(true)
	c.setStatus(appearanceThemeText("appearance.saving"), "preview")
	if c.save == nil {
		c.setStatus(appearanceThemeText("appearance.service_unavailable"), "warning")
		c.setSaveDisabled(false)
		return
	}
	c.save(theme, func(err error) {
		if err != nil {
			c.setStatus(appearanceThemeText("appearance.save_failed"), "warning")
			c.setSaveDisabled(false)
		} else {
			c.saved = theme
			c.syncEditContext(theme)
			c.setStatus(appearanceThemeText("appearance.saved"), "success")
			c.setSaveDisabled(true)
			if theme.Palette == "custom" || previousPalette == "custom" {
				// The server reissues a hash-pinned stylesheet for admitted
				// organization colors; do not inject runtime CSS in this client.
				js.Global().Get("location").Call("reload")
			}
		}
	})
}

func appearanceThemeFeedback(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "contrast"):
		return appearanceThemeText("appearance.error_contrast")
	case strings.Contains(message, "six-digit"):
		return appearanceThemeText("appearance.error_hex")
	default:
		return appearanceThemeText("appearance.error_generic")
	}
}

func appearanceThemeText(key string) string {
	root := js.Global().Get("document").Get("documentElement")
	locale := "en-US"
	if root.Truthy() {
		locale = root.Call("getAttribute", "lang").String()
	}
	return productui.ResolveProductLocale(locale).Text(key)
}

func (c *browserThemeController) Reset() {
	if c == nil {
		return
	}
	c.Preview(productui.DefaultCustomerTheme())
	c.syncEditor(productui.DefaultCustomerTheme())
}

func (c *browserThemeController) syncEditContext(theme productui.CustomerTheme) bool {
	if c == nil {
		return false
	}
	dirty := !reflect.DeepEqual(productui.NormalizeCustomerTheme(theme), productui.NormalizeCustomerTheme(c.saved))
	document := js.Global().Get("document")
	root := document.Get("documentElement")
	locale := productui.ResolveProductLocale(root.Call("getAttribute", "lang").String())
	setThemeText(`[data-hcm-theme-current]`, productui.AppearanceThemeSummary(locale, c.saved))
	setThemeText(`[data-hcm-theme-proposed]`, productui.AppearanceThemeSummary(locale, theme))
	bar := document.Call("querySelector", `.appearance-actions-sticky`)
	if bar.Truthy() {
		bar.Call("setAttribute", "data-hcm-edit-dirty", strconv.FormatBool(dirty))
	}
	return dirty
}

func (c *browserThemeController) setSaveDisabled(disabled bool) {
	button := js.Global().Get("document").Call("querySelector", `[data-hcm-action="save-appearance"]`)
	if button.Truthy() {
		button.Set("disabled", disabled || button.Call("getAttribute", "data-hcm-editable").String() != "true")
	}
}

func (c *browserThemeController) Apply(theme productui.CustomerTheme) {
	theme = productui.NormalizeCustomerTheme(theme)
	root := js.Global().Get("document").Get("documentElement")
	if !root.Truthy() {
		return
	}
	attributes := productui.CustomerThemeAttributes(theme)
	for _, name := range []string{"data-hcm-color-mode", "data-hcm-palette", "data-hcm-shape", "data-hcm-density", "data-hcm-glyphs", "data-hcm-typeface", "data-hcm-navigation", "data-hcm-motion"} {
		root.Call("setAttribute", name, attributes[name])
	}
	setThemeText(`[data-hcm-brand-name]`, theme.BrandName)
	setThemeText(`[data-hcm-brand-mark]`, theme.BrandMark)
	brandName, brandMark := productui.HeaderBrandIdentity(theme, c.tenant)
	setThemeText(`[data-hcm-brand-link] [data-hcm-brand-name]`, brandName)
	setThemeText(`[data-hcm-brand-link] [data-hcm-brand-mark]`, brandMark)
	setThemeText(`.appearance-preview-live-header [data-hcm-brand-name]`, brandName)
	setThemeText(`.appearance-preview-live-header [data-hcm-brand-mark]`, brandMark)
	setThemeAttribute(`[data-hcm-brand-link]`, "title", brandName)
	applyThemeBrandLogo(theme.BrandLogoURL, c.logoLoad, c.logoError)
	applyThemeDocumentIdentity("", theme, c.tenant)
}

func applyThemeDocumentIdentity(pageTitle string, theme productui.CustomerTheme, tenant string) {
	theme = productui.NormalizeCustomerTheme(theme)
	brandName, _ := productui.HeaderBrandIdentity(theme, tenant)
	document := js.Global().Get("document")
	if !document.Truthy() {
		return
	}
	root := document.Get("documentElement")
	if strings.TrimSpace(pageTitle) == "" && root.Truthy() {
		value := root.Call("getAttribute", "data-hcm-page-title")
		if value.Type() == js.TypeString {
			pageTitle = value.String()
		}
	}
	if strings.TrimSpace(pageTitle) != "" {
		document.Set("title", pageTitle+" · "+brandName)
		if root.Truthy() {
			root.Call("setAttribute", "data-hcm-page-title", pageTitle)
		}
	}
	meta := document.Call("querySelector", `meta[name="application-name"]`)
	if meta.Truthy() {
		meta.Call("setAttribute", "content", brandName)
	}
}

func applyLocaleDocumentIdentity(locale productui.LocaleContext) {
	if locale.Resolved == "" {
		locale = productui.ResolveProductLocale(locale.Requested)
	}
	root := js.Global().Get("document").Get("documentElement")
	if !root.Truthy() {
		return
	}
	root.Call("setAttribute", "lang", locale.Resolved)
	root.Call("setAttribute", "dir", string(locale.Direction))
	root.Call("setAttribute", "data-hcm-locale", locale.Resolved)
	root.Call("setAttribute", "data-hcm-catalog", locale.CatalogVersion)
	if locale.Fallback != productui.LocaleFallbackNone {
		root.Call("setAttribute", "data-hcm-locale-fallback", string(locale.Fallback))
		root.Call("setAttribute", "data-hcm-requested-locale", locale.Requested)
	} else {
		root.Call("removeAttribute", "data-hcm-locale-fallback")
		root.Call("removeAttribute", "data-hcm-requested-locale")
	}
	if missing := productui.MissingProductTranslations(locale.Resolved); len(missing) > 0 {
		root.Call("setAttribute", "data-hcm-message-fallback", productui.DefaultProductLocale)
		root.Call("setAttribute", "data-hcm-message-fallback-count", strconv.Itoa(len(missing)))
	} else {
		root.Call("removeAttribute", "data-hcm-message-fallback")
		root.Call("removeAttribute", "data-hcm-message-fallback-count")
	}
}

func (c *browserThemeController) setStatus(message, tone string) {
	status := js.Global().Get("document").Call("getElementById", "appearance-status")
	if !status.Truthy() {
		return
	}
	status.Set("textContent", message)
	status.Call("setAttribute", "data-tone", tone)
}

func (c *browserThemeController) syncEditor(theme productui.CustomerTheme) {
	values := map[string]string{
		"color_mode": theme.ColorMode, "palette": theme.Palette, "shape": theme.Shape, "density": theme.Density,
		"glyphs": theme.Glyphs, "typeface": theme.Typeface, "navigation": theme.Navigation, "motion": theme.Motion,
	}
	document := js.Global().Get("document")
	for name, selected := range values {
		inputs := document.Call("querySelectorAll", `input[name="`+name+`"]`)
		for index := 0; index < inputs.Get("length").Int(); index++ {
			input := inputs.Index(index)
			input.Set("checked", input.Get("value").String() == selected)
		}
	}
	for name, value := range map[string]string{"brand_name": theme.BrandName, "brand_mark": theme.BrandMark, "brand_logo_url": theme.BrandLogoURL} {
		input := document.Call("querySelector", `input[name="`+name+`"]`)
		if input.Truthy() {
			input.Set("value", value)
		}
	}
	c.syncColorWells(theme)
}

func (c *browserThemeController) syncColorWells(theme productui.CustomerTheme) {
	modes, err := productui.ResolveCustomerThemeModes(theme)
	if err != nil {
		return
	}
	document := js.Global().Get("document")
	for _, mode := range []productui.ThemeMode{productui.ThemeModeLight, productui.ThemeModeDark} {
		for _, token := range productui.ThemeTokens() {
			if token.Kind != productui.ThemeColor || !token.CustomerOverridable {
				continue
			}
			value, _ := modes[mode].Value(token.Name)
			input := document.Call("querySelector", `input[name="`+string(mode)+`-`+token.Name+`"]`)
			if input.Truthy() {
				input.Set("value", value)
			}
		}
	}
}

func setThemeText(selector, value string) {
	nodes := js.Global().Get("document").Call("querySelectorAll", selector)
	for index := 0; index < nodes.Get("length").Int(); index++ {
		nodes.Index(index).Set("textContent", value)
	}
}

func setThemeAttribute(selector, name, value string) {
	nodes := js.Global().Get("document").Call("querySelectorAll", selector)
	for index := 0; index < nodes.Get("length").Int(); index++ {
		nodes.Index(index).Call("setAttribute", name, value)
	}
}

func applyThemeBrandLogo(logoURL string, onLoad, onError js.Func) {
	document := js.Global().Get("document")
	slots := document.Call("querySelectorAll", `[data-hcm-brand-logo-slot]`)
	for index := 0; index < slots.Get("length").Int(); index++ {
		slot := slots.Index(index)
		image := slot.Call("querySelector", `[data-hcm-brand-logo]`)
		if !image.Truthy() || logoURL == "" {
			slot.Call("setAttribute", "data-hcm-brand-logo-state", "fallback")
			if image.Truthy() {
				image.Call("removeAttribute", "src")
			}
			continue
		}
		slot.Call("setAttribute", "data-hcm-brand-logo-state", "loading")
		image.Set("onload", onLoad)
		image.Set("onerror", onError)
		image.Call("setAttribute", "src", logoURL)
		if image.Get("complete").Bool() && image.Get("naturalWidth").Int() > 0 {
			slot.Call("setAttribute", "data-hcm-brand-logo-state", "configured")
		}
	}
}

func setBrandLogoEventState(args []js.Value, state string) {
	if len(args) == 0 {
		return
	}
	target := args[0].Get("target")
	if !target.Truthy() {
		return
	}
	slot := target.Call("closest", `[data-hcm-brand-logo-slot]`)
	if slot.Truthy() {
		slot.Call("setAttribute", "data-hcm-brand-logo-state", state)
	}
}
