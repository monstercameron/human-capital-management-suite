package productui

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestChannelWidgetCopyAcrossLocales(t *testing.T) {
	keys := []string{chatui.KeyTeamWidget, chatui.KeyTeamNote, chatui.KeyTeamRole, chatui.KeyChannelManager, chatui.KeyChannelMember, chatui.KeyProjectWidget, chatui.KeyProjectNote, chatui.KeyMilestone, chatui.KeyMilestoneStatus, chatui.KeyMilestoneOwner, chatui.KeyMilestoneDue, chatui.KeyWidgetPin, chatui.KeyWidgetUnpin, chatui.KeyWidgetError}
	english := chatui.EnglishCopy()
	for _, locale := range []string{"de-DE", "ar"} {
		translations := chatTranslations(locale)
		for _, key := range keys {
			if translations[key] == "" || (key != chatui.KeyMilestoneStatus && translations[key] == english[key]) {
				t.Errorf("%s missing translated %s", locale, key)
			}
		}
	}
}
