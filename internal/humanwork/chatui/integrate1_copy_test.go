package chatui

import (
	"strings"
	"testing"
)

func TestIntegrate1CompactCopy(t *testing.T) {
	for _, tc := range []struct{ table, locale, key, want string }{
		{chattoneCopyTable, "DE-de", "professional", "Professionell"},
		{chatvoiceCopyTable, "en-US", "send", "Send voice message"},
		{chatremoveCopyTable, "de-DE", "removed", "Von einem Administrator entfernt"},
		{chatremoveCopyTable, "en-US", "not-a-key", ""},
		{chatvoiceCopyTable, "en-US", "send\x00anything", ""},
		{chattoneCopyTable, "en-US", "", ""},
	} {
		if got := integrate1Copy(tc.table, tc.locale, tc.key); got != tc.want {
			t.Fatal(tc.key, got, tc.want)
		}
	}
	for _, table := range []string{chattoneCopyTable, chatvoiceCopyTable, chatremoveCopyTable} {
		for _, row := range strings.Split(table, "\n")[1:] {
			key, _, _ := strings.Cut(row, "\x00")
			for _, locale := range []string{"en-US", "de-DE", "ar"} {
				if integrate1Copy(table, locale, key) == "" {
					t.Fatal("missing translation", key, locale)
				}
			}
		}
	}
}
