package chatui

// menuTop chooses the side with more visible space and caps the menu height
// when neither side can fit it. Coordinates are viewport CSS pixels.
func menuTop(anchorTop, anchorBottom, menuHeight, boundTop, boundBottom float64) (top, maxHeight float64) {
	const gap = 4.0
	below := boundBottom - anchorBottom - gap
	above := anchorTop - boundTop - gap
	if below >= menuHeight || below >= above {
		if below < 1 {
			below = 1
		}
		return anchorBottom + gap, below
	}
	if above < 1 {
		above = 1
	}
	if menuHeight < above {
		above = menuHeight
	}
	return anchorTop - gap - above, above
}
