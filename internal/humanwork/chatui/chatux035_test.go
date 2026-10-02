package chatui

import (
	"strings"
	"testing"
)

// TestTodo_CHATUX_035: the plural helper picks the language's form (Arabic has
// six, German two), the member count noun after 18 is not the noun after 8, and
// an RTL page's blocks take the page direction while Latin version strings stay
// Latin by rule (identifiers, not prose numbers).
func TestTodo_CHATUX_035(t *testing.T) {
	cases := []struct {
		locale, noun string
		n            int
		want         string
	}{
		{"en-US", "members", 1, "1 member"}, {"en-US", "members", 18, "18 members"},
		{"de-DE", "members", 1, "1 Mitglied"}, {"de-DE", "members", 18, "18 Mitglieder"},
		{"ar", "members", 0, "لا أعضاء"}, {"ar", "members", 1, "عضو واحد"}, {"ar", "members", 2, "عضوان"},
		{"ar", "members", 8, "٨ أعضاء"}, {"ar", "members", 18, "١٨ عضوًا"}, {"ar", "members", 100, "١٠٠ عضو"},
		{"ar", "replies", 18, "١٨ ردًا"}, {"de-DE", "agents", 1, "1 Agent"}, {"en-US", "agents", 2, "2 agents"},
	}
	for _, c := range cases {
		if got := chatPlural(c.locale, c.noun, c.n); got != c.want {
			t.Errorf("%s %s %d: %q, want %q", c.locale, c.noun, c.n, got, c.want)
		}
	}
	if got := memberCountLabel(Model{Locale: "ar"}, 18); got != "١٨ عضوًا" {
		t.Errorf("header member count in Arabic: %q", got)
	}
	rtl := Model{Direction: "rtl"}
	if markdownBlockProps(rtl).Dir != "auto" || markdownBlockProps(Model{}).Dir != "" {
		t.Error("only a right-to-left page isolates paragraphs with dir=auto")
	}
	body := func(sel string) bool {
		return strings.HasSuffix(sel, `.message-body`) && strings.Contains(sel, `[dir="rtl"]`) && !strings.Contains(sel, "thread")
	}
	if got := s11Value(t, s11Env{width: 1280}, "direction", body); got != "rtl" {
		t.Errorf("an Arabic page's message body direction is %q, want rtl", got)
	}
	if got := s11Value(t, s11Env{width: 1280}, "text-align", body); got != "start" {
		t.Errorf("an Arabic page's message body text-align is %q, want start", got)
	}
}
