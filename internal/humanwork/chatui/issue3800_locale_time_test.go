package chatui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestIssue3800_ClockAndDayRouteThroughLocalize pins the shipped slice: the
// message clock and the day divider come from the localize package, so a
// French, Spanish or Japanese reader gets their own order, and Arabic keeps
// Arabic-Indic digits everywhere a clock is drawn.
func TestIssue3800_ClockAndDayRouteThroughLocalize(t *testing.T) {
	stamp := time.Date(2026, 9, 22, 15, 4, 0, 0, time.Local)
	for locale, want := range map[string]string{
		"en-US": "3:04 PM", "de-DE": "15:04", "fr-FR": "15:04", "es-ES": "15:04", "ja-JP": "15:04", "ar": "١٥:٠٤",
	} {
		if got := chat5Clock(locale, stamp); got != want {
			t.Errorf("%s clock %q, want %q", locale, got, want)
		}
		if got := chatsave002Clock(locale, stamp); got != want {
			t.Errorf("%s saved clock %q, want %q", locale, got, want)
		}
	}
	for locale, want := range map[string]string{
		"en-US": "Tuesday, September 22", "de-DE": "Dienstag, 22. September", "fr-FR": "mardi 22 septembre",
		"es-ES": "martes, 22 de septiembre", "ja-JP": "9月22日火曜日", "ar": "الثلاثاء، ٢٢ سبتمبر",
	} {
		if got := formatDay(locale, stamp, false); got != want {
			t.Errorf("%s day divider %q, want %q", locale, got, want)
		}
	}
}

// TestIssue3800_QuestionContextClockIsLocalized covers the reply card line
// that used to repeat the clock switch and showed Latin digits in Arabic.
func TestIssue3800_QuestionContextClockIsLocalized(t *testing.T) {
	at := time.Date(2026, 9, 22, 15, 4, 0, 0, time.Local)
	m := Model{Locale: "ar"}
	got := renderNode(t, renderAgentQuestionContext(m, agentReplyEnvelope{Backlink: "#c1", QuestionLabel: "general", QuestionAt: at}, ""))
	if !strings.Contains(got, "١٥:٠٤") {
		t.Errorf("Arabic question context lacks the Arabic clock: %s", got)
	}
}

// rawFormatBaseline is the count of hand-written display time layouts each chatui
// file still carries. The lint below fails when a file gains one, so new
// clock text goes through internal/experience/localize. Lower a number when
// a site is routed; never raise one.
var issue3800RawFormatBaseline = map[string]int{
	"agentux_ambient_cards.go": 2, "chatcmd002_card.go": 1, "chatdetails.go": 2,
	"chatgate_view.go": 2, "chatmap_location.go": 1, "chatsave002_view.go": 1,
	"timezone.go": 1,
}

// TestIssue3800_SearchHitClockFallbackIsLocalized: a hit with no ready-made time
// label used to get Go's English Kitchen clock in every language.
func TestIssue3800_SearchHitClockFallbackIsLocalized(t *testing.T) {
	at := time.Date(2026, 9, 22, 15, 4, 0, 0, time.Local)
	for locale, want := range map[string]string{"de-DE": "15:04", "ar": "١٥:٠٤", "en-US": "3:04 PM"} {
		got := searchWhen(Model{Locale: locale}, Message{SentAt: at})
		if !strings.HasSuffix(got, " · "+want) {
			t.Errorf("%s search time %q, want clock %q", locale, got, want)
		}
	}
}

// issue3800DisplayLayouts are the layout fragments that mean a person reads
// the result (a clock, a month or weekday name, a zone). Machine layouts such
// as RFC 3339 or a bare ISO date are not display text.
var issue3800DisplayLayouts = []string{"15:04", "3:04", "Jan", "Monday", "MST", "Kitchen"}

func TestIssue3800_NoNewRawTimeFormatInChatui(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if !strings.Contains(line, ".Format(") {
				continue
			}
			for _, layout := range issue3800DisplayLayouts {
				if strings.Contains(line, layout) {
					count++
					break
				}
			}
		}
		if allowed := issue3800RawFormatBaseline[file]; count > allowed {
			t.Errorf("%s has %d hand-written display time layouts, allowed %d: use localize.FormatClock/FormatDay/FormatTime/FormatRelative", file, count, allowed)
		}
	}
}
