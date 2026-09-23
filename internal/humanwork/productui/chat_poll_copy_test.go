package productui

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestChannelPollCopyAcrossLocales(t *testing.T) {
	keys := []string{chatui.KeyPollTitle, chatui.KeyPollQuestion, chatui.KeyPollOptions, chatui.KeyPollOptionsHint, chatui.KeyPollCreate, chatui.KeyPollVote, chatui.KeyPollChangeVote, chatui.KeyPollResults, chatui.KeyPollVoteOne, chatui.KeyPollVotes, chatui.KeyPollVoted, chatui.KeyPollLoading, chatui.KeyPollError, chatui.KeyPollNoPoll}
	english := chatui.EnglishCopy()
	for _, key := range keys {
		if english[key] == "" {
			t.Errorf("missing English %s", key)
		}
		for _, locale := range []string{"de-DE", "ar"} {
			translation := chatTranslations(locale)[key]
			if translation == "" || translation == english[key] {
				t.Errorf("missing %s translation for %s", locale, key)
			}
		}
	}
}
