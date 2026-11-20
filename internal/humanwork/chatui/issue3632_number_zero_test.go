package chatui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Issue 3632: the browser's Number formatter answers "" for zero. Model.n is the
// general count formatter, so it must write the locale's zero itself; a surface
// that means "no value" hides the whole element instead of printing a blank.
//
// Call-site decisions (every Model.n / Model.nz caller in this package):
//   - zero written (n or nz): sidebar section count, search group count, tray
//     poll votes / todo progress / "+N more", project board progress, composer
//     audience counts, message audience tag, audience line.
//   - element hidden or not drawn at zero: header pinned / members / map count
//     buttons, rail unread and mention badges, moderation and saved sidebar
//     badges (Hidden), reaction chips (never below one), attachment badges,
//     counted labels, "show all N", gate question counts (a gate has at least
//     one question), archived section (not drawn when empty), search count (says
//     "no results" at zero), project milestone count.
func TestIssue3632_ModelNWritesLocalizedZero(t *testing.T) {
	for _, locale := range chatmodLocales {
		m := chatZeroModel(locale)
		zero := chatNumeral(locale, "0")
		if got := m.n(0); got != zero || got == "" {
			t.Errorf("%s: n(0) = %q, want %q", locale, got, zero)
		}
		if got := m.nz(0); got != zero {
			t.Errorf("%s: nz(0) = %q, want %q", locale, got, zero)
		}
		if got, want := m.n(7), chatNumeral(locale, "7"); got != want {
			t.Errorf("%s: n(7) = %q, want %q", locale, got, want)
		}
	}
}

// A badge that is hidden at zero stays hidden and a count button with nothing to
// count draws no blank count.
func TestIssue3632_ZeroCountsHideTheElementOrWriteZero(t *testing.T) {
	for _, locale := range chatmodLocales {
		m := chatZeroModel(locale)
		button := renderNode(t, chatux001CountButton("x", "header-members", "Members", false, "people", 0, m))
		if strings.Contains(button, "chatux001-count") {
			t.Errorf("%s: a count button with nothing to count draws a count: %s", locale, button)
		}
		headers := ""
		for _, node := range chatside001Header(m, SidebarSection{ID: "s", Name: "Rooms"}, "Rooms", nil) {
			headers += renderNode(t, node)
		}
		if !regexp.MustCompile(`section-count"[^>]*>` + regexp.QuoteMeta(chatNumeral(locale, "0")) + `<`).MatchString(headers) {
			t.Errorf("%s: the empty section count is not a zero: %s", locale, headers)
		}
		for _, n := range []int{0, 5} {
			if got := m.n(n); strings.TrimSpace(got) == "" {
				t.Errorf("%s: n(%s) is blank", locale, strconv.Itoa(n))
			}
		}
	}
}
