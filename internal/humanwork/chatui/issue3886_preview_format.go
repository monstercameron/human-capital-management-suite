package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// keyComposerFormatPreviewOff explains why the formatting buttons are disabled
// while the draft is shown as read-only formatted text (issue 3886).
const keyComposerFormatPreviewOff = "chat.composer.format_preview_off"

func init() {
	for locale, text := range map[string]string{
		"en-US": "Turn off Preview to format your draft",
		"de-DE": "Vorschau ausschalten, um den Entwurf zu formatieren",
		"ar":    "أوقف المعاينة لتنسيق المسودة",
	} {
		composerToolsCopy[locale][keyComposerFormatPreviewOff] = text
	}
}

// composerFormatRowFor is the formatting row of the composer target names. The
// buttons edit the field, which a preview replaces, so while the preview shows
// they are disabled and each carries the reason as its tooltip.
func composerFormatRowFor(m Model, local localUI, target string, disabled bool) ui.Node {
	if !composerPreviewOn(local, target) {
		return composerFormatRow(m, target, disabled)
	}
	return html.Div(html.Props{ID: target + "-format-row", Class: "composer-format-row"}, formatToolbarNote(m, target, true, composerText(m, keyComposerFormatPreviewOff)))
}
