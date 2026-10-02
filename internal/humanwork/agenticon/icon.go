// Package agenticon composes decorative agent identities from product-owned vectors.
package agenticon

import (
	"fmt"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type Input struct {
	Name, Description, Instructions string
	Skills                          []string
}

// Value contains only closed vocabulary identifiers, never source text.
type Value struct {
	Glyph      string `json:"glyph"`
	Shape      string `json:"shape"`
	Foreground string `json:"foreground"`
	Background string `json:"background"`
	Variation  uint32 `json:"variation"`
}

type ColourPair struct{ Foreground, Background string }

// ColourPairs uses the shell's governed status palettes in both themes.
func ColourPairs() []ColourPair {
	return []ColourPair{
		{"--hcm-color-info", "--hcm-color-info-surface"},
		{"--hcm-color-success", "--hcm-color-success-surface"},
		{"--hcm-color-warning", "--hcm-color-warning-surface"},
		{"--hcm-color-danger", "--hcm-color-danger-surface"},
	}
}

type shape struct{ id, path string }

func shapes() []shape {
	return []shape{
		{"circle", "M12 1a11 11 0 1 0 0 22 11 11 0 0 0 0-22Z"},
		{"rounded-square", "M5 1h14a4 4 0 0 1 4 4v14a4 4 0 0 1-4 4H5a4 4 0 0 1-4-4V5a4 4 0 0 1 4-4Z"},
		{"hexagon", "M6 1h12l6 11-6 11H6L0 12Z"},
		{"shield", "M12 0 24 5l-2 12-10 7L2 17 0 5Z"},
		{"squircle", "M12 0C23 0 24 1 24 12S23 24 12 24 0 23 0 12 1 0 12 0Z"},
		{"blob-round", "M11 0C19 0 24 6 24 13s-7 11-13 11S0 17 0 11 4 0 11 0Z"},
		{"blob-wide", "M12 1C20-2 24 5 24 12s-5 10-12 11S0 19 0 12 4 3 12 1Z"},
		{"blob-soft", "M12 0C17 0 24 3 23 12s-3 12-11 12S-1 19 1 12 5 0 12 0Z"},
	}
}

const Variations = 32

func (value Value) Valid() bool {
	glyphOK, shapeOK, pairOK := false, false, false
	for _, g := range glyphs() {
		glyphOK = glyphOK || g.ID == value.Glyph
	}
	for _, s := range shapes() {
		shapeOK = shapeOK || s.id == value.Shape
	}
	for _, p := range ColourPairs() {
		pairOK = pairOK || (p.Foreground == value.Foreground && p.Background == value.Background)
	}
	return glyphOK && shapeOK && pairOK && value.Variation < Variations
}

func drawing(value Value) (Value, string, string) {
	if !value.Valid() {
		value = Value{Glyph: "neutral", Shape: "circle", Foreground: "--hcm-color-info", Background: "--hcm-color-info-surface"}
	}
	var background, glyph string
	for _, s := range shapes() {
		if s.id == value.Shape {
			background = s.path
		}
	}
	for _, g := range glyphs() {
		if g.ID == value.Glyph {
			glyph = g.Path
		}
	}
	return value, background, glyph
}

// Node is decorative beside an independently rendered name and Agent badge.
func Node(value Value) ui.Node {
	value, background, glyph := drawing(value)
	return html.Tag("svg", html.Props{Class: "agent-icon", Width: "24", Height: "24", Raw: map[string]any{"viewBox": "0 0 24 24", "aria-hidden": "true", "focusable": "false"}},
		html.Tag("path", html.Props{Raw: map[string]any{"d": background, "fill": "var(" + value.Background + ")"}}),
		html.Tag("path", html.Props{Raw: map[string]any{"d": glyph, "transform": "translate(4 4) scale(.666667)", "fill": "none", "stroke": "var(" + value.Foreground + ")", "stroke-width": "2.4", "stroke-linecap": "round", "stroke-linejoin": "round"}}),
	)
}

// SVG is safe for an inline server-rendered fragment, including forged values.
func SVG(value Value) string {
	value, background, glyph := drawing(value)
	return fmt.Sprintf(`<svg class="agent-icon" width="24" height="24" viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path d="%s" fill="var(%s)"/><path d="%s" transform="translate(4 4) scale(.666667)" fill="none" stroke="var(%s)" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"/></svg>`, background, value.Background, glyph, value.Foreground)
}
