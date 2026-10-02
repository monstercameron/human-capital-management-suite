package chatui

// LayerRect is a rectangle in viewport coordinates.
type LayerRect struct{ Left, Top, Right, Bottom float64 }

// LayerPlacement is where a menu or panel goes and how big it may be.
type LayerPlacement struct{ Left, Top, Width, Height float64 }

// PlaceChatLayer is Chat's one placement rule for a menu or panel (CHATBUG-051),
// for the pieces of the page that draw their own menu instead of an anchored
// layer: the Saved panel's Remind me menu uses it, so it opens against its bell
// the way a message's menu opens against its button. The layer touches its
// opener, opens toward the side with more room when the preferred side cannot
// hold it, and stays inside bounds.
func PlaceChatLayer(anchor, bounds LayerRect, width, height float64, above, rtl bool) LayerPlacement {
	g := anchoredChatGeometryIn(chatLayerRect{anchor.Left, anchor.Top, anchor.Right, anchor.Bottom}, chatLayerRect{bounds.Left, bounds.Top, bounds.Right, bounds.Bottom}, width, height, above, rtl)
	return LayerPlacement{Left: g.left, Top: g.top, Width: g.width, Height: g.height}
}
