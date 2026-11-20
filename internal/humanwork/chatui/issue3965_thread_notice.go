package chatui

// ThreadUnavailableNotice is the line shown when an address asked for a thread
// that cannot be opened (deleted, moved, or not visible to this reader). It says
// nothing about the thread's content or why it is closed (#3965).
func ThreadUnavailableNotice(locale string) string {
	const en = "That thread is no longer available, so the conversation opened on its own."
	const de = "Dieser Thread ist nicht mehr verfügbar, deshalb wurde nur die Unterhaltung geöffnet."
	const ar = "هذا الموضوع لم يعد متاحًا، لذلك فُتحت المحادثة وحدها."
	return [3]string{en, de, ar}[chatbug039LocaleIndex(locale)]
}
