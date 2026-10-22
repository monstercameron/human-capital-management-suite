package chatui

// BarDismissalStore is where the browser remembers that the conversation bar
// was dismissed (CHATLANG-008). The page host fills it in with its storage
// adapter; Chat never touches browser storage itself. Left empty (the native
// build, a browser that refuses storage) a dismissal lasts the page session.
var BarDismissalStore struct {
	Dismissed func() bool
	Remember  func()
}

// chatlangBarStoredDismissed reports whether the person dismissed the
// conversation bar before. A missing store means it was not.
func chatlangBarStoredDismissed() bool {
	return BarDismissalStore.Dismissed != nil && BarDismissalStore.Dismissed()
}

// chatlangRememberBarDismissed remembers a dismissal, best effort.
func chatlangRememberBarDismissed() {
	if BarDismissalStore.Remember != nil {
		BarDismissalStore.Remember()
	}
}
