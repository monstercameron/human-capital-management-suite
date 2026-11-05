package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// glyphEmbedAttrs is the shared wrapper of the project and journey embed
// glyphs: 24 units, 2 stroke, round caps. Shared like iconSVGAttrs: the
// renderer copies Raw maps, so sharing saves a map allocation per glyph.
var glyphEmbedAttrs = map[string]any{
	"viewBox": "0 0 24 24", "fill": "none", "stroke": "currentColor", "stroke-width": "2",
	"stroke-linecap": "round", "stroke-linejoin": "round", "aria-hidden": "true", "focusable": "false",
}

// embedGlyph renders a one-path project/journey embed glyph. The class is the
// 13px glyph of the embed cards; only the path varies per kind.
func embedGlyph(d string) ui.Node {
	return html.Tag("svg", html.Props{Class: "chat-project-embed-glyph", Raw: glyphEmbedAttrs}, html.Tag("path", html.Props{Raw: map[string]any{"d": d}}))
}

// glyphVoiceAttrs and glyphVoiceSizedAttrs are the shared wrappers of the
// voice glyphs: 24 units, 1.8 stroke, no caps. The sized wrapper keeps the
// 20px width/height fallback for framings without the chat stylesheet; where
// the stylesheet sizes .chat-icon the plain wrapper draws the same.
var glyphVoiceAttrs = map[string]any{
	"viewBox": "0 0 24 24", "fill": "none", "stroke": "currentColor", "stroke-width": "1.8",
	"aria-hidden": "true", "focusable": "false",
}

var glyphVoiceSizedAttrs = map[string]any{
	"viewBox": "0 0 24 24", "width": "20", "height": "20", "fill": "none", "stroke": "currentColor", "stroke-width": "1.8",
	"aria-hidden": "true", "focusable": "false",
}

// voiceGlyph renders a one-path voice glyph in the chat-icon class. sized
// keeps the explicit 20px fallback of framings drawn without the chat
// stylesheet; the chat stylesheet sizes .chat-icon either way.
func voiceGlyph(d string, sized bool) ui.Node {
	attrs := glyphVoiceAttrs
	if sized {
		attrs = glyphVoiceSizedAttrs
	}
	return html.Tag("svg", html.Props{Class: "chat-icon", Raw: attrs}, html.Tag("path", html.Props{Raw: map[string]any{"d": d}}))
}
