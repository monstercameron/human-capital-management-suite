package chatui

import "time"

// A closing time chosen as a preset ("In an hour", "End of day", ...) is worked
// out once, when the person chooses it, and kept with the preview's settings
// under settingClosesAt. Reading the text again (every keystroke) therefore
// does not move it, and Post sends the time the preview shows. A draft that sat
// until that time is out of date is refreshed and confirmed rather than posted
// at a time nobody reviewed (#3926).

const settingClosesAt = "closes-at"

// chatcmd003StaleAfter is how far "In an hour" may drift from the time the
// preview shows before Post asks for a second look.
const chatcmd003StaleAfter = 5 * time.Minute

func chatcmd003Relative(preset string) bool {
	switch preset {
	case "hour", "today", "tomorrow", "week":
		return true
	}
	return false
}

// chatcmd003ClosesPin is the closing time to keep for preset now, or "" when
// the preset stands for no fixed time (never, or the date typed in the text).
func chatcmd003ClosesPin(m Model, preset string) string {
	if !chatcmd003Relative(preset) {
		return ""
	}
	if at := chatcmd003Closes(m, preset, nil); at != nil {
		return at.Format(time.RFC3339Nano)
	}
	return ""
}

// chatcmd003ClosesKept is the closing time a card takes for preset: the one
// kept when it was chosen, or, with none kept, the one counted from now.
func chatcmd003ClosesKept(m Model, preset string, typed *time.Time, settings map[string]string) *time.Time {
	if chatcmd003Relative(preset) || preset == closesCustom {
		if at, err := time.Parse(time.RFC3339Nano, settings[settingClosesAt]); err == nil {
			return &at
		}
	}
	return chatcmd003Closes(m, preset, typed)
}

// chatcmd003ClosesStale reports whether the closing time kept for the chosen
// preset is no longer the one the preset stands for now: "In an hour" has drifted
// by more than a few minutes, or a day preset names a day that has passed.
func chatcmd003ClosesStale(m Model, settings map[string]string) bool {
	preset := settings["closes"]
	if !chatcmd003Relative(preset) {
		return false
	}
	kept, err := time.Parse(time.RFC3339Nano, settings[settingClosesAt])
	if err != nil {
		return false
	}
	fresh := chatcmd003Closes(m, preset, nil)
	if fresh == nil {
		return false
	}
	if preset == "hour" {
		drift := fresh.Sub(kept)
		return drift > chatcmd003StaleAfter || drift < -chatcmd003StaleAfter
	}
	return !fresh.Equal(kept)
}

// chatcmd003ClosesRefresh is state with the closing time counted again from
// now and the preview told to say so; nothing is posted.
func chatcmd003ClosesRefresh(m Model, state chatcmd003Preview) chatcmd003Preview {
	settings := map[string]string{}
	for name, value := range state.Settings {
		settings[name] = value
	}
	settings[settingClosesAt] = chatcmd003ClosesPin(m, settings["closes"])
	state.Settings = settings
	next := chatcmd003Draw(m, state, state.Draft.Card.Kind, state.Draft.Raw)
	next.Error = "closes-moved"
	return next
}
