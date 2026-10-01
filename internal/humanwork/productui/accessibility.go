package productui

// AccessibilityPreferences is a bounded presentation preference set. It has
// no authorization or business meaning and is safe to apply across every
// product component and embedded workflow surface.
type AccessibilityPreferences struct {
	TextSize string `json:"text_size"`
	Contrast string `json:"contrast"`
	Motion   string `json:"motion"`
	Links    string `json:"links"`
	// ColorMode is empty when the person follows the organization's mode.
	ColorMode string `json:"color_mode,omitempty"`
}

type AccessibilityOption struct {
	ID             string
	LabelKey       string
	DescriptionKey string
	Detail         string
}

func DefaultAccessibilityPreferences() AccessibilityPreferences {
	return AccessibilityPreferences{TextSize: "standard", Contrast: "system", Motion: "system", Links: "standard"}
}

func NormalizeAccessibilityPreferences(value AccessibilityPreferences) AccessibilityPreferences {
	defaults := DefaultAccessibilityPreferences()
	if !allowedAccessibilityValue(value.TextSize, "standard", "large", "larger") {
		value.TextSize = defaults.TextSize
	}
	if !allowedAccessibilityValue(value.Contrast, "system", "more") {
		value.Contrast = defaults.Contrast
	}
	if !allowedAccessibilityValue(value.Motion, "system", "limited", "reduce") {
		value.Motion = defaults.Motion
	}
	if !allowedAccessibilityValue(value.Links, "standard", "underlined") {
		value.Links = defaults.Links
	}
	if !allowedAccessibilityValue(value.ColorMode, "", "organization", "light", "dark") {
		value.ColorMode = defaults.ColorMode
	}
	if value.ColorMode == "organization" {
		value.ColorMode = defaults.ColorMode
	}
	return value
}

func AccessibilityPreferenceAttributes(value AccessibilityPreferences) map[string]string {
	value = NormalizeAccessibilityPreferences(value)
	return map[string]string{
		"data-hcm-text-size":           value.TextSize,
		"data-hcm-contrast":            value.Contrast,
		"data-hcm-motion-preference":   value.Motion,
		"data-hcm-links":               value.Links,
		"data-hcm-personal-color-mode": value.ColorMode,
	}
}

func allowedAccessibilityValue(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func AccessibilityTextSizeOptions() []AccessibilityOption {
	return []AccessibilityOption{
		{ID: "standard", LabelKey: "accessibility.text_standard", Detail: "100%"},
		{ID: "large", LabelKey: "accessibility.text_large", Detail: "112%"},
		{ID: "larger", LabelKey: "accessibility.text_larger", Detail: "125%"},
	}
}

func AccessibilityContrastOptions() []AccessibilityOption {
	return []AccessibilityOption{
		{ID: "system", LabelKey: "accessibility.system", DescriptionKey: "accessibility.contrast_system_help"},
		{ID: "more", LabelKey: "accessibility.contrast_more", DescriptionKey: "accessibility.contrast_more_help"},
	}
}

func AccessibilityMotionOptions() []AccessibilityOption {
	return []AccessibilityOption{
		{ID: "system", LabelKey: "accessibility.system", DescriptionKey: "accessibility.motion_system_help"},
		{ID: "limited", LabelKey: "accessibility.motion_limited", DescriptionKey: "accessibility.motion_limited_help"},
		{ID: "reduce", LabelKey: "accessibility.motion_reduce", DescriptionKey: "accessibility.motion_reduce_help"},
	}
}

func AccessibilityLinkOptions() []AccessibilityOption {
	return []AccessibilityOption{
		{ID: "standard", LabelKey: "accessibility.links_standard", DescriptionKey: "accessibility.links_standard_help"},
		{ID: "underlined", LabelKey: "accessibility.links_underlined", DescriptionKey: "accessibility.links_underlined_help"},
	}
}

func AccessibilityColorModeOptions() []AccessibilityOption {
	return []AccessibilityOption{
		{ID: "organization", LabelKey: "accessibility.color_mode_organization", DescriptionKey: "accessibility.color_mode_organization_help"},
		{ID: "light", LabelKey: "accessibility.color_mode_light", DescriptionKey: "accessibility.color_mode_light_help"},
		{ID: "dark", LabelKey: "accessibility.color_mode_dark", DescriptionKey: "accessibility.color_mode_dark_help"},
	}
}
