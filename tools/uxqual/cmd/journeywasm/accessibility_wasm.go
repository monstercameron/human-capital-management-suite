//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

type browserAccessibilityController struct {
	saved productui.AccessibilityPreferences
	// loaded reports whether saved holds the person's real preferences rather
	// than the defaults it starts with. See Reapply.
	loaded bool
	save   func(productui.AccessibilityPreferences, func(error))
}

func newBrowserAccessibilityController(save func(productui.AccessibilityPreferences, func(error))) *browserAccessibilityController {
	return &browserAccessibilityController{saved: productui.DefaultAccessibilityPreferences(), save: save}
}

func (c *browserAccessibilityController) Load(value productui.AccessibilityPreferences) {
	if c != nil {
		c.saved = productui.NormalizeAccessibilityPreferences(value)
		c.loaded = true
		c.Apply(c.saved)
	}
}

// Reapply restores the last known preferences, discarding any unsaved preview,
// and does nothing before the first Load -- for the same reason as the theme
// controller's. Applying the defaults at boot reset a person who reads at
// large text, or needs more contrast, to standard text and system contrast
// until the page's data read finished: the layout reflowed twice on every
// page for exactly the people least able to follow it.
func (c *browserAccessibilityController) Reapply() {
	if c != nil && c.loaded {
		c.Apply(c.saved)
	}
}

func (c *browserAccessibilityController) Saved() productui.AccessibilityPreferences {
	if c == nil {
		return productui.DefaultAccessibilityPreferences()
	}
	return productui.NormalizeAccessibilityPreferences(c.saved)
}

func (c *browserAccessibilityController) Preview(value productui.AccessibilityPreferences) {
	c.Apply(value)
	c.setStatus("preview", "preview")
}

func (c *browserAccessibilityController) Save(value productui.AccessibilityPreferences) {
	if c == nil {
		return
	}
	value = productui.NormalizeAccessibilityPreferences(value)
	c.saved = value
	c.loaded = true
	c.Apply(value)
	c.setStatus("saving", "preview")
	if c.save == nil {
		c.setStatus("service_unavailable", "warning")
		return
	}
	c.save(value, func(err error) {
		if err != nil {
			c.setStatus("save_failed", "warning")
		} else {
			c.setStatus("saved", "success")
		}
	})
}

func (c *browserAccessibilityController) Reset() {
	if c == nil {
		return
	}
	c.Save(productui.DefaultAccessibilityPreferences())
	c.syncEditor(c.saved)
}

func (c *browserAccessibilityController) Apply(value productui.AccessibilityPreferences) {
	value = productui.NormalizeAccessibilityPreferences(value)
	root := js.Global().Get("document").Get("documentElement")
	if !root.Truthy() {
		return
	}
	attributes := productui.AccessibilityPreferenceAttributes(value)
	for _, name := range []string{"data-hcm-text-size", "data-hcm-contrast", "data-hcm-motion-preference", "data-hcm-links"} {
		root.Call("setAttribute", name, attributes[name])
	}
}

func (c *browserAccessibilityController) syncEditor(value productui.AccessibilityPreferences) {
	values := map[string]string{"text-size": value.TextSize, "contrast": value.Contrast, "motion-preference": value.Motion, "links": value.Links}
	document := js.Global().Get("document")
	for name, selected := range values {
		inputs := document.Call("querySelectorAll", `input[name="`+name+`"]`)
		for index := 0; index < inputs.Get("length").Int(); index++ {
			input := inputs.Index(index)
			input.Set("checked", input.Get("value").String() == selected)
		}
	}
}

func (c *browserAccessibilityController) setStatus(code, tone string) {
	status := js.Global().Get("document").Call("getElementById", "accessibility-status")
	if !status.Truthy() {
		return
	}
	status.Set("textContent", accessibilityStatusMessage(code))
	status.Call("setAttribute", "data-tone", tone)
}

func accessibilityStatusMessage(code string) string {
	locale := js.Global().Get("document").Get("documentElement").Call("getAttribute", "lang").String()
	messages := map[string]map[string]string{
		"en-US": {"preview": "Previewing unsaved accessibility preferences", "saving": "Saving accessibility preferences…", "saved": "Accessibility preferences saved to your account", "save_failed": "Accessibility preferences could not be saved", "service_unavailable": "Preference service is unavailable"},
		"de-DE": {"preview": "Nicht gespeicherte Einstellungen werden angezeigt", "saving": "Barrierefreiheitseinstellungen werden gespeichert…", "saved": "Barrierefreiheitseinstellungen wurden im Konto gespeichert", "save_failed": "Barrierefreiheitseinstellungen konnten nicht gespeichert werden", "service_unavailable": "Einstellungsdienst ist nicht verfügbar"},
		"ar":    {"preview": "تتم معاينة تفضيلات إمكانية الوصول غير المحفوظة", "saving": "جارٍ حفظ تفضيلات إمكانية الوصول…", "saved": "تم حفظ تفضيلات إمكانية الوصول في حسابك", "save_failed": "تعذر حفظ تفضيلات إمكانية الوصول", "service_unavailable": "خدمة التفضيلات غير متاحة"},
	}
	if localized, ok := messages[locale]; ok {
		if message := localized[code]; message != "" {
			return message
		}
	}
	return messages[productui.DefaultProductLocale][code]
}
