package chatui

// issue3852LoadedPosition is "{n} of {total} loaded images" for a conversation
// whose older or newer messages are not all on the page: the viewer steps
// through the images it has, so it must not read as the whole gallery.
var issue3852LoadedPosition = map[string]string{
	"en-US": "{n} of {total} loaded images",
	"de-DE": "{n} von {total} geladenen Bildern",
	"ar":    "{n} من {total} صور محملة",
}

// chatImageViewerPosition is the viewer's place label. partial is true while
// the list still offers older or newer messages to load.
func chatImageViewerPosition(l chatbug083Labels, n, total int, partial bool) string {
	if !partial || l.PositionLoaded == "" {
		return l.position(n, total)
	}
	return l.withPosition(l.PositionLoaded).position(n, total)
}

// withPosition is l with its place template replaced.
func (l chatbug083Labels) withPosition(template string) chatbug083Labels {
	l.Position = template
	return l
}
