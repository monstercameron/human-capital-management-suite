package main

import (
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// purposeSavedFor is how long "Saved" stays beside a channel's purpose.
const purposeSavedFor = 2 * time.Second

// confirmPurposeSaved watches a purpose save and answers "Saved" once it went
// through (CHATUX-021). The save itself runs in the widget callbacks; this
// reads the model until the room holds the purpose that was sent and nothing is
// pending, shows "Saved" for two seconds, and gives up, saying nothing, when
// the save failed, the person moved to another room, or it never settled.
func confirmPurposeSaved(purpose string, read func() chatui.Model, show func(saved bool), sleep func(time.Duration)) {
	purpose = strings.TrimSpace(purpose)
	room := read().SelectedID
	for attempt := 0; attempt < 50; attempt++ {
		sleep(200 * time.Millisecond)
		model := read()
		if model.SelectedID != room || model.ChannelWidgetsError != "" {
			return
		}
		if model.ChannelWidgetsPending || strings.TrimSpace(model.ChannelTeam.Purpose) != purpose {
			continue
		}
		show(true)
		sleep(purposeSavedFor)
		show(false)
		return
	}
}
