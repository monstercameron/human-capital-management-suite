package chatui_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestChatPolish_Surfaces_Browser renders the channel with its thread, details
// and a message menu open, with the product's catalog in three languages: no
// internal identifier, no key and no inline style reaches the page, and the
// details show an agent's reach in words.
func TestChatPolish_Surfaces_Browser(t *testing.T) {
	raw := regexp.MustCompile(`\b[A-Z][A-Z0-9]+(?:_[A-Z0-9]+)+\b`)
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			m := agentux062Model(locale)
			m.ShowThread, m.ThreadParentID, m.ShowDetails = true, "root", true
			m.MenuID = "mine"
			page := agentux062Page(t, m)
			for _, forbidden := range []string{"<details", "<summary", ` style="`} {
				if strings.Contains(page, forbidden) {
					t.Errorf("%s reached the page", forbidden)
				}
			}
			visible := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(page, " ")
			if found := raw.FindString(visible); found != "" {
				t.Errorf("the page shows the identifier %s", found)
			}
			if got := chatui.AgentBadgeLabel(locale); got == nil {
				t.Error("no agent badge")
			}
		})
	}
}
