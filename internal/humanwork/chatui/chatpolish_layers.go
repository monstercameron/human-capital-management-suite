package chatui

type chatPolishLayer struct {
	kind    string
	order   int
	visible bool
}

// Escape follows opening order rather than document order (a sidebar panel
// can be newer than a message menu rendered later in the tree).
func chatPolishTopLayer(layers []chatPolishLayer) int {
	top := -1
	for i, layer := range layers {
		if layer.visible && (top < 0 || layer.order >= layers[top].order) {
			top = i
		}
	}
	return top
}

func chatPolishManagedLayer(kind string) bool {
	switch kind {
	case "quiet-hours", "reading-languages", chatux002PrefsKind, chatux002ChannelsKind, "formatting", "writing-style", "location", "voice", "saved", chatComposerAddKind:
		return true
	}
	return false
}
