//go:build js && wasm

package main

import (
	"strconv"
	"strings"
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

type browserThemeController struct {
	saved     productui.CustomerTheme
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
	c.setStatus("Previewing unsaved appearance changes", "preview")
}

func (c *browserThemeController) Save(theme productui.CustomerTheme) {
	if c == nil {
		return
	}
	theme = productui.NormalizeCustomerTheme(theme)
	c.saved = theme
	c.Apply(theme)
	c.setStatus("Saving appearance…", "preview")
	if c.save == nil {
		c.setStatus("Appearance service is unavailable", "warning")
		return
	}
	c.save(theme, func(err error) {
		if err != nil {
			c.setStatus("Appearance could not be saved", "warning")
		} else {
			c.setStatus("Appearance saved for your organization", "success")
		}
	})
}

func (c *browserThemeController) Reset() {
	if c == nil {
		return
	}
	c.Save(productui.DefaultCustomerTheme())
	c.syncEditor(c.saved)
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
