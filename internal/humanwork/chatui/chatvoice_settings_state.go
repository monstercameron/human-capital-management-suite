package chatui

// The voice switch rows keep their state for the page, not for one instance of
// the component. The Chat preferences popover and Manage channel are drawn again
// whenever the page's model changes, and the instance that asked the server could
// be gone (its effect cleaned up, its request cancelled) before the answer came,
// which left the preferences row on "Loading voice settings…" for good. The
// reading row hit the same thing (chatbug045_reading.go), so this follows its
// shape: the state lives here, answers are applied on the render thread, and an
// instance only draws what the store holds.

// voiceSettingsStore is each row's state for the page, by voiceSettingsKey.
var voiceSettingsStore = map[string]voiceSettingsState{}

func (p voiceSettingsProps) storeKey() string {
	return p.Scope + "\x00" + p.Tenant + "\x00" + p.Conversation
}

// voiceSettingsCurrent is the row's state: the page's once anything has changed
// it, the instance's own before that.
func voiceSettingsCurrent(key string, local voiceSettingsState) voiceSettingsState {
	if stored, ok := voiceSettingsStore[key]; ok {
		return stored
	}
	return local
}

// voiceSettingsChange applies a change to the row's state, keeps it for the page
// and returns it. It runs on the render thread.
func voiceSettingsChange(key string, local voiceSettingsState, change func(*voiceSettingsState)) voiceSettingsState {
	next := voiceSettingsCurrent(key, local)
	change(&next)
	voiceSettingsStore[key] = next
	return next
}

// voiceSettingsAnswered is the row's state once the server has answered a read
// or a save. restore is true when a save failed after a value was on show: the
// switch must be put back to the position the server holds.
func voiceSettingsAnswered(current voiceSettingsState, data VoiceSettingsData, status int, err error, saved bool) (next voiceSettingsState, restore bool) {
	current.Loading, current.Saving = false, false
	switch {
	case err == nil:
		current.Data, current.Loaded, current.LoadFailed, current.Denied = data, true, false, false
		current.Saved, current.Fails = saved, false
	case status == 403 && !current.Loaded:
		current.Denied, current.Loaded = true, true
	case status == 503 && !current.Loaded:
		// The server answered that voice messages are not composed.
		current.Unavailable, current.Loaded = true, true
	default:
		// The last good value stays on show; the line says what failed.
		current.LoadFailed = !current.Loaded
		current.Saved, current.Fails = false, current.Loaded
		restore = current.Loaded
	}
	return current, restore
}
