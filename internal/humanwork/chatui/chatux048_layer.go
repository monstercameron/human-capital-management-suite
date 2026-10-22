package chatui

// chatux048AboveFooter keeps a sidebar panel that opens downward off the band at
// the foot of the sidebar (the Archived channels). The panel was given the whole
// height under its opener, which put its lower edge over that band at 1280x900;
// it now ends a gap above the band and scrolls inside itself past that. A panel
// that opens upward already ends above its opener, and a band that was not found
// (footerTop of zero or less) changes nothing.
func chatux048AboveFooter(p chatLayerPlacement, footerTop float64) chatLayerPlacement {
	const gap = 8
	if p.up || footerTop <= 0 {
		return p
	}
	p.maxHeight = min(p.maxHeight, max(0, footerTop-p.top-gap))
	return p
}
