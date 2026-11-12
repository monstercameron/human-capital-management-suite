package chatui

import (
	"os"
	"strings"
	"testing"
)

// CHAT-3716: a message list that was on its newest message must stay on it
// when the composer grows while the viewer types. The pin is decided before
// the text changes (beforeinput) and applied after it (input), because by the
// time input fires the list has already shrunk.
func TestChat3716_AtNewestSlack(t *testing.T) {
	cases := []struct {
		name                            string
		scrollHeight, scrollTop, client float64
		want                            bool
	}{
		{"exactly at the newest message", 1000, 600, 400, true},
		{"within the slack", 1000, 570, 400, true},
		{"scrolled up past the slack", 1000, 500, 400, false},
		{"list shorter than its viewport", 300, 0, 400, true},
	}
	for _, tc := range cases {
		if got := chat3716AtNewest(tc.scrollHeight, tc.scrollTop, tc.client); got != tc.want {
			t.Errorf("%s: chat3716AtNewest(%v, %v, %v) = %v, want %v", tc.name, tc.scrollHeight, tc.scrollTop, tc.client, got, tc.want)
		}
	}
}

// The wasm pin is built only for js/wasm, so the host test reads its source and
// checks that the typing path is subscribed, not only focus.
func TestChat3716_ComposerPinFollowsTyping(t *testing.T) {
	src, err := os.ReadFile("composer_pin_js.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	for _, want := range []string{
		`"beforeinput", beforeInput)`,
		`"input", input)`,
		"chat3716AtNewest(",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("composer_pin_js.go lacks %s: a composer that grows by typing covers the newest message", want)
		}
	}
}
