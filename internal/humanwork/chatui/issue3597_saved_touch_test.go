package chatui

import (
	"strings"
	"testing"
)

// Issue 3597: on a phone the Saved page's item actions, Back and Close must be
// at least 44x44 CSS pixels, whatever the icon size. The geometry is stated in
// the stylesheet; the served-app check measures the rendered boxes.
func TestIssue3597_SavedPhoneTargetsAre44px(t *testing.T) {
	phone := issue3597Between(t, Stylesheet, "@media(max-width:760px),(pointer:coarse){\n.chatsave-actions{", "\n}\n@media(max-width:760px){")
	for _, want := range []string{
		".chatsave-actions .message-action{width:44px;min-width:44px;height:44px;min-height:44px}",
		".chatsave-header .chatsave-back.icon-button,.chatsave-header .chat-panel-close.chat-panel-close.icon-button{inline-size:44px;min-inline-size:44px;block-size:44px;min-block-size:44px}",
	} {
		if !strings.Contains(phone, want) {
			t.Errorf("the phone/coarse block lacks %q", want)
		}
	}
	// The compact 24px toolbar is for a fine pointer on a wide screen only.
	if !strings.Contains(Stylesheet, "@media(min-width:761px) and (pointer:fine){.chat-workspace .chatsave-actions .message-action{height:24px;min-height:24px}}") {
		t.Error("the compact toolbar is not limited to wide fine-pointer screens")
	}
}

func issue3597Between(t *testing.T, s, from, to string) string {
	t.Helper()
	i := strings.Index(s, from)
	if i < 0 {
		t.Fatalf("stylesheet has no %q", from)
	}
	rest := s[i:]
	j := strings.Index(rest, to)
	if j < 0 {
		t.Fatalf("stylesheet has no %q after %q", to, from)
	}
	return rest[:j]
}
