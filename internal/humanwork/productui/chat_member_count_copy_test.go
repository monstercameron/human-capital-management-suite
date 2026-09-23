package productui

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestChatSingleMemberCountCopyAcrossLocales(t *testing.T) {
	if got := chatui.EnglishCopy()[chatui.KeyMemberCountOne]; got != "1 member" {
		t.Fatalf("English singular count = %q", got)
	}
	for _, tt := range []struct{ locale, want string }{
		{locale: "de-DE", want: "1 Mitglied"},
		{locale: "ar", want: "عضو واحد"},
	} {
		if got := chatTranslations(tt.locale)[chatui.KeyMemberCountOne]; got != tt.want {
			t.Errorf("%s singular count = %q, want %q", tt.locale, got, tt.want)
		}
	}
}
