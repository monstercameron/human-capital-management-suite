package chatui

// Where the picker goes. From the composer it opens directly above the emoji
// button, aligned to it, inside the conversation column; from a message it opens
// beside the message's action bar, aligned to the bar's end. It never covers the
// button that opened it and never leaves the viewport. (At phone width it is a
// sheet across the bottom of the screen, which the stylesheet places.)

const (
	emojiPickerWidth  = 340.0
	emojiPickerHeight = 392.0
	emojiPickerGap    = 8.0
	emojiPickerEdge   = 8.0
	emojiPickerMinH   = 220.0
)

// emojiPlacement is a rectangle in viewport pixels.
type emojiPlacement struct{ Left, Top, Width, Height float64 }

// chatEmojiPlace places a picker against anchor, the opener's rectangle, inside
// bounds, the column the opener lives in. alignEnd puts the picker's end edge on
// the anchor's end edge; otherwise the start edges line up. rtl mirrors both.
func chatEmojiPlace(anchor, bounds chatLayerRect, vw, vh float64, rtl, alignEnd bool) emojiPlacement {
	width := min(emojiPickerWidth, max(0, vw-2*emojiPickerEdge))
	height := min(emojiPickerHeight, max(0, vh-2*emojiPickerEdge))

	startAligned := (rtl && alignEnd) || (!rtl && !alignEnd)
	left := anchor.right - width
	if startAligned {
		left = anchor.left
	}
	minLeft, maxRight := max(emojiPickerEdge, bounds.left), min(vw-emojiPickerEdge, bounds.right)
	if bounds.right-bounds.left < width || maxRight-minLeft < width {
		minLeft, maxRight = emojiPickerEdge, vw-emojiPickerEdge
	}
	left = max(minLeft, min(left, maxRight-width))

	above := anchor.top - emojiPickerGap - emojiPickerEdge
	below := vh - anchor.bottom - emojiPickerGap - emojiPickerEdge
	switch {
	case above >= height:
		return emojiPlacement{left, anchor.top - emojiPickerGap - height, width, height}
	case below >= height:
		return emojiPlacement{left, anchor.bottom + emojiPickerGap, width, height}
	case above >= below:
		// Neither side holds the whole picker: take the roomier side and let the
		// grid, which scrolls, give up the difference.
		h := max(min(height, above), min(emojiPickerMinH, height))
		return emojiPlacement{left, max(emojiPickerEdge, anchor.top-emojiPickerGap-h), width, h}
	default:
		h := max(min(height, below), min(emojiPickerMinH, height))
		return emojiPlacement{left, anchor.bottom + emojiPickerGap, width, min(h, max(0, vh-anchor.bottom-emojiPickerGap-emojiPickerEdge))}
	}
}
