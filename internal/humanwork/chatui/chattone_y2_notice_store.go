package chatui

// FirstReadNoticeStore is where the browser remembers that the first-read
// translation notice was answered. The page host fills it in with its storage
// adapter; Chat never touches browser storage itself. Left empty (the native
// build, a browser that refuses storage) the notice shows until it is answered
// in this page session.
var FirstReadNoticeStore struct {
	Seen     func() bool
	Remember func()
}

// chatlangNoticeStoredSeen reports whether this browser session has already shown
// the first-read notice. A missing store means it has not.
func chatlangNoticeStoredSeen() bool {
	return FirstReadNoticeStore.Seen != nil && FirstReadNoticeStore.Seen()
}

// chatlangRememberNoticeSeen remembers that the notice was answered, best effort.
func chatlangRememberNoticeSeen() {
	if FirstReadNoticeStore.Remember != nil {
		FirstReadNoticeStore.Remember()
	}
}
