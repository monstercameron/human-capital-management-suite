//go:build !(js && wasm)

package chatui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// #3773: the day pill is never drawn inside the fade the list keeps under the
// conversation header. The pill sticks at or below the last pixel of that fade,
// and a jump lands below the pill.
func TestChatDesign_3773_DayPillSticksBelowTheHeaderFade(t *testing.T) {
	fade := regexp.MustCompile(`\.message-list\{mask-image:linear-gradient\(to bottom,transparent 0,#000 (\d+)px\)`).FindStringSubmatch(Stylesheet)
	if fade == nil {
		t.Fatal("the message list no longer fades under the header; this test's premise changed")
	}
	fadeEnd, _ := strconv.Atoi(fade[1])
	rule := regexp.MustCompile(`\.chat-workspace \.message-list \.day-divider\{([^}]*)\}`).FindAllStringSubmatch(Stylesheet, -1)
	sticky := ""
	for _, match := range rule {
		if strings.Contains(match[1], "position:sticky") {
			sticky = match[1]
		}
	}
	if sticky == "" {
		t.Fatal("the day pill does not stick below the header")
	}
	top := regexp.MustCompile(`(?:^|;)top:(\d+)px`).FindStringSubmatch(sticky)
	if top == nil {
		t.Fatalf("the sticky day pill has no top offset: %s", sticky)
	}
	offset, _ := strconv.Atoi(top[1])
	if offset < fadeEnd {
		t.Fatalf("the day pill sticks at %dpx, inside the %dpx fade under the header", offset, fadeEnd)
	}
	if !strings.Contains(sticky, "pointer-events:none") {
		t.Error("the floating day row takes clicks meant for the messages under it")
	}
	// A stuck rule line would strike through the messages behind the pill.
	if !strings.Contains(Stylesheet, `.chat-workspace .message-list .day-divider::before,.chat-workspace .message-list .day-divider::after{content:none}`) {
		t.Error("the day separator's rule lines would float over the messages")
	}
	// The pill is opaque: text never shows through it.
	if got := chatbugCascadeValue(Stylesheet, ".day-divider span", "background"); got != "var(--surface)" {
		t.Errorf("the day pill background is %q, want the opaque surface", got)
	}
	// A jump or reveal parks below the stuck pill, not under it.
	padding := regexp.MustCompile(`\.chat-workspace \.message-list\{scroll-padding-top:(\d+)px\}`).FindStringSubmatch(Stylesheet)
	if padding == nil {
		t.Fatal("jumps no longer leave room for the stuck day pill")
	}
	if room, _ := strconv.Atoi(padding[1]); room < offset+24 {
		t.Fatalf("a jump parks %dpx down, under a pill that sticks at %dpx", room, offset)
	}
	if last := strings.LastIndex(Stylesheet, `.message-list{scroll-padding-top:20px}`); last < 0 || last > strings.Index(Stylesheet, padding[0]) {
		t.Error("the earlier 20px landing rule is declared after the pill's")
	}
	// Both lists carry the same separator markup the rule addresses.
	for _, locale := range []string{"en-US", "ar"} {
		m := chat4Fixture(locale, "sent", false)
		markup := render(t, m)
		if !regexp.MustCompile(`class="message-list[^>]*>.*class="day-divider" role="separator"><span>[^<]+</span>`).MatchString(markup) {
			t.Errorf("%s: the message list has no day separator pill: the rule would address nothing", locale)
		}
	}
}
