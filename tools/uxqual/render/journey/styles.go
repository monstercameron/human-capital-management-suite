package journey

import (
	"github.com/monstercameron/human-capital-management-suite/internal/experience/journeycss"
)

// This file holds the Promotion journey page's design tokens twice over: as
// Go values (Palette, Swatches, TextPairs, UIPairs) that styles_test.go
// scores with tokens.ContrastRatio, and as the typed CSS custom properties
// at the top of the stylesheet (typed_journey_a.go). They are deliberately
// two representations of one truth rather than one generated from the
// other, because the server pins the sha256 of Stylesheet() in its
// content-security-policy: the custom properties must name exactly the
// palette the contrast tests score. TestStylesheetDeclaresEverySwatch
// closes the gap by asserting every Go swatch appears verbatim in the CSS.

// Swatch is one named color and its sRGB hex value. Name is the CSS custom
// property's suffix: Swatch{"accent", "#2b3a8f"} is --jn-accent:#2b3a8f.
type Swatch struct {
	Name string
	Hex  string // "#rrggbb"
}

// ColorPair is one foreground/background combination the page actually
// renders, with the reason it exists. Purpose is what a failing contrast
// assertion prints, so it names the element, not the color.
type ColorPair struct {
	Purpose    string
	Foreground Swatch
	Background Swatch
}

// Palette is every color the journey page uses. Nothing in stylesheet may
// name a color that is not here (a raw hex outside the :root block is a
// defect TestStylesheetUsesOnlyPaletteHexes catches).
var Palette = struct {
	Canvas         Swatch
	Surface        Swatch
	SurfaceSunk    Swatch
	SurfaceMuted   Swatch
	Hairline       Swatch
	ControlBorder  Swatch
	Ink            Swatch
	InkMuted       Swatch
	Masthead       Swatch
	MastheadInk    Swatch
	MastheadMuted  Swatch
	MastheadChip   Swatch
	MastheadChipIn Swatch
	Accent         Swatch
	AccentStrong   Swatch
	AccentInk      Swatch
	AccentSoft     Swatch
	Info           Swatch
	InfoSoft       Swatch
	Success        Swatch
	SuccessSoft    Swatch
	Warning        Swatch
	WarningSoft    Swatch
	Danger         Swatch
	DangerInk      Swatch
	DangerSoft     Swatch
	Neutral        Swatch
	NeutralSoft    Swatch
}{
	Canvas:         Swatch{"canvas", "#f6f7fb"},
	Surface:        Swatch{"surface", "#ffffff"},
	SurfaceSunk:    Swatch{"surface-sunk", "#eef0f7"},
	SurfaceMuted:   Swatch{"surface-muted", "#eceef5"},
	Hairline:       Swatch{"hairline", "#dfe3ee"},
	ControlBorder:  Swatch{"control-border", "#7d859c"},
	Ink:            Swatch{"ink", "#16192a"},
	InkMuted:       Swatch{"ink-muted", "#545a70"},
	Masthead:       Swatch{"masthead", "#10142b"},
	MastheadInk:    Swatch{"masthead-ink", "#ffffff"},
	MastheadMuted:  Swatch{"masthead-muted", "#b9c0d8"},
	MastheadChip:   Swatch{"masthead-chip", "#232a4a"},
	MastheadChipIn: Swatch{"masthead-chip-ink", "#ccd3e8"},
	Accent:         Swatch{"accent", "#2b3a8f"},
	AccentStrong:   Swatch{"accent-strong", "#1f2c73"},
	AccentInk:      Swatch{"accent-ink", "#ffffff"},
	AccentSoft:     Swatch{"accent-soft", "#e4ecfb"},
	Info:           Swatch{"info", "#14448f"},
	InfoSoft:       Swatch{"info-soft", "#e4ecfb"},
	Success:        Swatch{"success", "#0f6136"},
	SuccessSoft:    Swatch{"success-soft", "#dff3e6"},
	Warning:        Swatch{"warning", "#7a4a00"},
	WarningSoft:    Swatch{"warning-soft", "#fdeed3"},
	Danger:         Swatch{"danger", "#9b1130"},
	DangerInk:      Swatch{"danger-ink", "#ffffff"},
	DangerSoft:     Swatch{"danger-soft", "#fde6ea"},
	Neutral:        Swatch{"neutral", "#3f465e"},
	NeutralSoft:    Swatch{"neutral-soft", "#eceef5"},
}

// Swatches returns the palette in the order the :root block declares it.
func Swatches() []Swatch {
	p := Palette
	return []Swatch{
		p.Canvas, p.Surface, p.SurfaceSunk, p.SurfaceMuted, p.Hairline, p.ControlBorder,
		p.Ink, p.InkMuted,
		p.Masthead, p.MastheadInk, p.MastheadMuted, p.MastheadChip, p.MastheadChipIn,
		p.Accent, p.AccentStrong, p.AccentInk, p.AccentSoft,
		p.Info, p.InfoSoft, p.Success, p.SuccessSoft, p.Warning, p.WarningSoft,
		p.Danger, p.DangerInk, p.DangerSoft, p.Neutral, p.NeutralSoft,
	}
}

// TextPairs enumerates every foreground/background combination the page
// renders text over. Each must reach WCAG 2.2 AA for normal text (4.5:1);
// none of them is declared large-text-only, so the stricter threshold
// applies to all of them and a later type-scale change cannot silently
// invalidate the list.
//
// Gradients are scored at both ends: the masthead brand well runs
// masthead to masthead-chip and the primary button runs accent to
// accent-strong, so the pairs below cover the lightest stop of each, which
// is the one that could fail.
func TextPairs() []ColorPair {
	p := Palette
	return []ColorPair{
		{"body text on the page canvas", p.Ink, p.Canvas},
		{"body text on a card", p.Ink, p.Surface},
		{"body text on a sunk panel", p.Ink, p.SurfaceSunk},
		{"secondary text on the page canvas", p.InkMuted, p.Canvas},
		{"secondary text on a card", p.InkMuted, p.Surface},
		{"secondary text on a sunk panel", p.InkMuted, p.SurfaceSunk},
		{"table header text", p.InkMuted, p.SurfaceMuted},
		{"zebra row text", p.Ink, p.SurfaceSunk},
		{"masthead text", p.MastheadInk, p.Masthead},
		{"masthead text over the brand gradient's light stop", p.MastheadInk, p.MastheadChip},
		{"masthead secondary text", p.MastheadMuted, p.Masthead},
		{"masthead role chip text", p.MastheadChipIn, p.MastheadChip},
		{"primary button label", p.AccentInk, p.Accent},
		{"primary button label over the gradient's dark stop", p.AccentInk, p.AccentStrong},
		{"danger button label", p.DangerInk, p.Danger},
		{"link and accent text on a card", p.Accent, p.Surface},
		{"link and accent text on the canvas", p.Accent, p.Canvas},
		{"accent text on a tinted panel", p.Accent, p.AccentSoft},
		{"info chip text", p.Info, p.InfoSoft},
		{"info text on a card", p.Info, p.Surface},
		{"success chip text", p.Success, p.SuccessSoft},
		{"success text on a card", p.Success, p.Surface},
		{"warning chip text", p.Warning, p.WarningSoft},
		{"warning text on a card", p.Warning, p.Surface},
		{"danger chip text", p.Danger, p.DangerSoft},
		{"danger text on a card", p.Danger, p.Surface},
		{"neutral chip text", p.Neutral, p.NeutralSoft},
		{"neutral text on a card", p.Neutral, p.Surface},
		{"secondary text inside a success chip", p.InkMuted, p.SuccessSoft},
		{"secondary text inside a warning chip", p.InkMuted, p.WarningSoft},
		{"secondary text inside a danger chip", p.InkMuted, p.DangerSoft},
		{"secondary text inside an info chip", p.InkMuted, p.InfoSoft},
		{"gauge scale labels", p.InkMuted, p.SurfaceSunk},
		{"people table text on the selected row", p.Ink, p.AccentSoft},
		{"people table secondary text on the selected row", p.InkMuted, p.AccentSoft},
		{"the selected row's name", p.AccentStrong, p.AccentSoft},
	}
}

// UIPairs enumerates the non-text boundaries that carry meaning on their
// own -- form control edges, the focus ring, the stepper's state markers,
// the gauge and meter fills -- and so must reach WCAG 2.2 AA for non-text
// contrast (1.4.11, 3:1). The purely decorative hairline between cards is
// deliberately absent: the card is distinguished from the canvas by its own
// surface color, so the rule does not apply to it.
func UIPairs() []ColorPair {
	p := Palette
	return []ColorPair{
		{"input border on a card", p.ControlBorder, p.Surface},
		{"input border on the canvas", p.ControlBorder, p.Canvas},
		{"input border on a sunk panel", p.ControlBorder, p.SurfaceSunk},
		{"focus ring on the canvas", p.Accent, p.Canvas},
		{"focus ring on a card", p.Accent, p.Surface},
		{"focus ring on the masthead", p.MastheadInk, p.Masthead},
		{"completed step marker", p.Accent, p.Surface},
		{"failed step marker", p.Danger, p.Surface},
		{"timeline dot on a card", p.Neutral, p.Surface},
		{"pay band fill on its track", p.Accent, p.SurfaceMuted},
		{"pay band current marker", p.Neutral, p.SurfaceMuted},
		{"budget meter fill, healthy", p.Success, p.SurfaceMuted},
		{"budget meter fill, tight", p.Warning, p.SurfaceMuted},
		{"budget meter fill, over", p.Danger, p.SurfaceMuted},
	}
}

// Stylesheet returns the page's CSS as one string so the server can pin its
// sha256 in the content-security-policy's style-src. The string is built
// from typed GWC declarations (see typed_journey_a/b/c.go) and memoized, so
// every call returns byte-identical bytes cheaply; the guarantees the old
// literal const gave (exact rule order, hashed @keyframes names) are
// preserved by TestStylesheetAnimationNamesHaveKeyframes and the content
// assertions below.
func Stylesheet() string { return journeycss.Stylesheet() }
