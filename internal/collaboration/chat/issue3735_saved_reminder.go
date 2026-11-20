package chat

// Issue 3735: a saved message's reminder reaches its owner as a private notice
// on the Moderation page (counted in the sidebar). It holds a reference to the
// saved message, never its text or the person's private note.
const ModerationOutcomeSavedReminder = "saved_reminder"

// IsSavedReminderOutcome reports whether a notice is a saved-message reminder.
func IsSavedReminderOutcome(outcome string) bool {
	return outcome == ModerationOutcomeSavedReminder
}
