package chatui

// translateUnavailableTitle is the tooltip of the translate switch: the
// "not available here yet" explanation only when translation really is
// unavailable, nothing on a working switch (issue 3884).
func translateUnavailableTitle(m RenderingSettingsModel) string {
	if !m.TranslationUnavailable {
		return ""
	}
	return ChatFeatureUnavailable(m.Locale)
}
