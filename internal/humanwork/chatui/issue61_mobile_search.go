package chatui

// mobileRailBackgroundInert reports which workspace regions are made inert
// behind the phone Conversations drawer. The main column normally is, but when
// search results are mounted inside it they are the screen the person is
// looking at (they paint over the drawer), so they must stay in the
// accessibility tree and keep receiving taps (issue 61).
func mobileRailBackgroundInert(modal, resultsMounted bool) (main, side, skip bool) {
	if !modal {
		return false, false, false
	}
	return !resultsMounted, true, true
}

// mobileRailAdvertisesModal reports whether the open phone drawer may claim
// aria-modal and trap Tab. While results are lifted over it they are the
// screen, and a modal claim would hide them from assistive technology and keep
// keyboard focus from reaching them.
func mobileRailAdvertisesModal(resultsMounted bool) bool {
	return !resultsMounted
}
