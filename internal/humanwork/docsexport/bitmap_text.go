package docsexport

import (
	"image"
	"image/color"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// DrawBitmapText draws centered ASCII text onto an image. It is used by the
// deterministic document preview asset generator and keeps font mechanics
// inside the document export boundary.
func DrawBitmapText(dst *image.RGBA, centerX, baselineY int, text string, ink color.Color) {
	d := font.Drawer{Dst: dst, Src: image.NewUniform(ink), Face: basicfont.Face7x13}
	width := d.MeasureString(text).Round()
	d.Dot = fixed.P(centerX-width/2, baselineY)
	d.DrawString(text)
}
