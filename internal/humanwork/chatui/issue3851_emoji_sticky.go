package chatui

// The sticky heading hides the top emojiHeadH pixels of the grid, so the category
// a person is looking at is the one under that heading's lower edge, not the row
// behind it. A browser lands on a device pixel, so a grid scrolled to a category's
// own heading can report a position a fraction short of it; reading the row at the
// top would then name the category before.

// emojiRowShown is the row just below the sticky heading at this scroll position.
func emojiRowShown(rows []emojiRow, scroll float64, m emojiGeometry) int {
	return emojiRowAtScroll(rows, scroll+m.Head)
}

// emojiTail is the empty room after the last section so that every category's
// heading can be scrolled to the top, however short the last categories are.
func emojiTail(rows []emojiRow, total, viewH float64) float64 {
	last := -1
	for i, row := range rows {
		if row.Head {
			last = i
		}
	}
	if last < 0 {
		return 0
	}
	return max(0, viewH-(total-rows[last].Y))
}
