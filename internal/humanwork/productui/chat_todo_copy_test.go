package productui

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestChannelTodoAndPinReferenceCopyAcrossLocales(t *testing.T) {
	keys := []string{chatui.KeyPinJump, chatui.KeyPinCopy, chatui.KeyPinCopyGuestUnavailable, chatui.KeyTodoTitle, chatui.KeyTodoLoading, chatui.KeyTodoError, chatui.KeyTodoSaveError, chatui.KeyTodoEmpty, chatui.KeyTodoNew, chatui.KeyTodoAdd, chatui.KeyTodoComplete, chatui.KeyTodoReopen, chatui.KeyTodoDelete, chatui.KeyTodoPin, chatui.KeyTodoUnpin, chatui.KeyTodoOpen, chatui.KeyTodoRemaining, chatui.KeyTodoNoOpen, chatui.KeyTodoAttachPin, chatui.KeyTodoNoPin, chatui.KeyTodoSource, chatui.KeyTodoCompletedBy, chatui.KeyTodoMemberFallback, chatui.KeyTodoModeEveryone, chatui.KeyTodoModeMe, chatui.KeyTodoModeSelected, chatui.KeyTodoModeLabel, chatui.KeyTodoAddMember, chatui.KeyTodoRemoveMember, chatui.KeyTodoOnlyCreator, chatui.KeyTodoOnlySelected, chatui.KeyTodoSelectedMember}
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
