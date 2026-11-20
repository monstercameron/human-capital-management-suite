package productui

// CanStartPromotion says whether the viewer may start a governed promotion:
// the server-resolved promote-worker action is available to them, they may
// create a journey and they may open the People directory it starts from. It
// is the same verdict the global launcher uses, so a surface that offers
// "start a promotion" (issue 3723: Chat's message menu and /promote) never
// offers it to a person the flow would turn away.
func (view View) CanStartPromotion() bool {
	return globalSearchCanStartPromotion(view)
}
