package journey

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Every icon on this page is an inline SVG built here. The document is
// served under `default-src 'none'`, so there is no sprite sheet, no icon
// font and no <img> to load one from: the only glyphs that can reach the
// page are the ones in the markup.
//
// All of them are decorative -- they sit beside text that already carries
// the meaning -- so each is aria-hidden and focusable="false" (the latter
// for Internet Explorer-era SVG focus behaviour that some enterprise
// browsers still inherit). Nothing on this page depends on an icon, or on
// its color, to be understood: tones are always paired with a word.

// iconSize is the default square icon edge, in px, matching the 16px step of
// the type scale.
const iconSize = "16"

// icon wraps a set of SVG children in a decorative, currentColor-inheriting
// <svg> of the given pixel edge.
func icon(class, size string, children ...ui.Node) ui.Node {
	return html.Svg(html.Props{
		Class:  class,
		Width:  size,
		Height: size,
		Raw: map[string]any{
			"viewBox":         "0 0 24 24",
			"fill":            "none",
			"stroke":          "currentColor",
			"stroke-width":    "2",
			"stroke-linecap":  "round",
			"stroke-linejoin": "round",
			"aria-hidden":     "true",
			"focusable":       "false",
		},
	}, children...)
}

func strokePath(d string) ui.Node {
	return html.Path(html.Props{Raw: map[string]any{"d": d}})
}

// BrandMark is the masthead monogram: an enclosing rounded square with an
// upward step inside it, the same "one governed step up" the page is about.
// It is the only filled icon on the page.
func BrandMark() ui.Node {
	return brandMark("jn-mark")
}

// brandMark renders the masthead monogram with the caller's styling hook.
// Keeping the public BrandMark helper preserves its historical class while
// registry callers can apply the same class contract as every other icon.
func brandMark(class string) ui.Node {
	return html.Svg(html.Props{
		Class:  class,
		Width:  "28",
		Height: "28",
		Raw: map[string]any{
			"viewBox":     "0 0 32 32",
			"fill":        "none",
			"aria-hidden": "true",
			"focusable":   "false",
		},
	},
		html.Rect(html.Props{Raw: map[string]any{
			"x": "1.25", "y": "1.25", "width": "29.5", "height": "29.5", "rx": "8.5",
			"stroke": "currentColor", "stroke-width": "2.5", "fill": "none",
		}}),
		html.Path(html.Props{Raw: map[string]any{
			"d":    "M9 22v-5h4.5v5H9Zm7-8V22h-4.5V14H16Zm7-5v13h-4.5V9H23Z",
			"fill": "currentColor",
		}}),
	)
}

func iconCheck(class string) ui.Node {
	return icon(class, "14", strokePath("M20 6 9 17l-5-5"))
}

func iconArrowRight(class string) ui.Node {
	return icon(class, iconSize, strokePath("M5 12h14"), strokePath("m13 6 6 6-6 6"))
}

func iconArrowLeft(class string) ui.Node {
	return icon(class, iconSize, strokePath("M19 12H5"), strokePath("m11 18-6-6 6-6"))
}

func iconInfo(class string) ui.Node {
	return icon(class, "18",
		html.Circle(html.Props{Raw: map[string]any{"cx": "12", "cy": "12", "r": "9"}}),
		strokePath("M12 11v5"),
		strokePath("M12 8h.01"),
	)
}

func iconSuccess(class string) ui.Node {
	return icon(class, "18",
		html.Circle(html.Props{Raw: map[string]any{"cx": "12", "cy": "12", "r": "9"}}),
		strokePath("m8.5 12.2 2.4 2.4 4.6-4.9"),
	)
}

func iconWarning(class string) ui.Node {
	return icon(class, "18",
		strokePath("M10.3 4.3 2.6 17.5A2 2 0 0 0 4.3 20.5h15.4a2 2 0 0 0 1.7-3L13.7 4.3a2 2 0 0 0-3.4 0Z"),
		strokePath("M12 10v4"),
		strokePath("M12 17h.01"),
	)
}

func iconDanger(class string) ui.Node {
	return icon(class, "18",
		html.Circle(html.Props{Raw: map[string]any{"cx": "12", "cy": "12", "r": "9"}}),
		strokePath("m15 9-6 6"),
		strokePath("m9 9 6 6"),
	)
}

// iconForTone maps a semantic tone onto its icon so a toned surface never
// relies on color alone (WCAG 1.4.1).
func iconForTone(tone, class string) ui.Node {
	switch tone {
	case toneSuccess:
		return RenderIcon(IconSuccess, class, nil)
	case toneWarning:
		return RenderIcon(IconWarning, class, nil)
	case toneDanger:
		return RenderIcon(IconDanger, class, nil)
	default:
		return RenderIcon(IconInfo, class, nil)
	}
}

// iconForSeverity is iconForTone for simulation findings, whose severity
// vocabulary ("blocking") differs from the tone vocabulary ("danger").
func iconForSeverity(severity, class string) ui.Node {
	switch severity {
	case severityBlocking:
		return RenderIcon(IconDanger, class, nil)
	case severityWarning, severityNeedsData:
		return RenderIcon(IconWarning, class, nil)
	case severitySuccess:
		return RenderIcon(IconSuccess, class, nil)
	default:
		return RenderIcon(IconInfo, class, nil)
	}
}

// iconLedger marks the recorded-outcome card: a document with a seal.
func iconLedger(class string) ui.Node {
	return icon(class, "18",
		strokePath("M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8Z"),
		strokePath("M14 3v5h5"),
		strokePath("m9 14 2 2 4-4"),
	)
}

// iconEmpty is the list's empty state mark: an open, unfilled tray.
func iconEmpty(class string) ui.Node {
	return icon(class, "32",
		strokePath("M4 14h4l2 3h4l2-3h4"),
		strokePath("M5.5 6.5 4 14v4a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-4l-1.5-7.5A2 2 0 0 0 16.5 5h-9a2 2 0 0 0-2 1.5Z"),
	)
}

// iconClock marks a deadline or a pending step.
func iconClock(class string) ui.Node {
	return icon(class, "14",
		html.Circle(html.Props{Raw: map[string]any{"cx": "12", "cy": "12", "r": "9"}}),
		strokePath("M12 7v5l3 2"),
	)
}

// iconSpark marks a record this page created, as opposed to one that
// arrived with the cell. It is a glyph rather than a tint so the two sources
// of an employee differ in monochrome as well as in color.
func iconSpark(class string) ui.Node {
	return icon(class, "12",
		strokePath("M10 3.5 11.5 8 16 9.5 11.5 11 10 15.5 8.5 11 4 9.5 8.5 8Z"),
		strokePath("M17.5 15.5v4"),
		strokePath("M15.5 17.5h4"),
	)
}

// iconPerson marks the "acts as" line on a routed action.
func iconPerson(class string) ui.Node {
	return icon(class, "14",
		html.Circle(html.Props{Raw: map[string]any{"cx": "12", "cy": "8", "r": "3.5"}}),
		strokePath("M5 20a7 7 0 0 1 14 0"),
	)
}
