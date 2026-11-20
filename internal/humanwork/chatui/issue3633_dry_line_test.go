package chatui

import (
	"strings"
	"testing"
)

// Issue 3633: the dry-run line counts messages through chatPlural. The
// rendered one/many cases in en-US, de-DE and ar are pinned by
// TestTodo_CHATMOD_003_Browser_HitsDryLineCount. This sweep covers the rest of
// chatmod003_copy.go: the only count-bearing template is hits_dry_line, whose
// {count} is filled with a pluralized noun; no template carries a bare {n}.
func TestIssue3633_NoBareCountTemplatesInFilterCopy(t *testing.T) {
	for key, forms := range modadminCopy {
		for i, form := range forms {
			if strings.Contains(form, "{n}") {
				t.Errorf("%s[%d] has a bare {n} count with no plural forms: %q", key, i, form)
			}
			if strings.Contains(form, "{count}") && key != "hits_dry_line" {
				t.Errorf("%s[%d] has a {count} outside the plural-filled dry line: %q", key, i, form)
			}
		}
	}
	for _, locale := range chatmodLocales {
		if one, many := chatPlural(locale, "messages", 1), chatPlural(locale, "messages", 3); one == many || strings.Contains(one+many, "{n}") {
			t.Errorf("%s: one %q and many %q do not differ or leak the placeholder", locale, one, many)
		}
	}
}
