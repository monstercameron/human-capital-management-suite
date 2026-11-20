package chatui

import (
	"regexp"
	"strings"
	"testing"
)

// Issue 3598: at 320 and 390 px the thread header keeps its short title whole in
// en-US, de-DE and ar. The channel link is what truncates, the close control is
// an icon whose accessible name stays, and the title is read before the actions.
func TestIssue3598_ThreadHeaderKeepsTitleAndNamesEveryLocale(t *testing.T) {
	// The words the product catalog ships for these keys (productui/chat_copy.go).
	words := map[string][2]string{
		"en-US": {"Thread", "Close thread"},
		"de-DE": {"Thread", "Thread schließen"},
		"ar":    {"السلسلة", "إغلاق السلسلة"},
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			m := chatux008Model(false)
			m.Locale = locale
			m.Text = func(key string) string {
				switch key {
				case KeyThread:
					return words[locale][0]
				case KeyCloseThread:
					return words[locale][1]
				}
				return englishCopy[key]
			}
			m.Conversations[0].Name = "private-qa-escalations-and-long-running-investigations"
			pane := chatux008Pane(t, m)
			heading := pane[strings.Index(pane, `class="side-heading chat-panel-head thread-heading"`):strings.Index(pane, `class="thread-scroll"`)]
			title, closeName := words[locale][0], words[locale][1]
			if !strings.Contains(heading, "<h2>"+title+"</h2>") {
				t.Errorf("the header lost its short title %q: %s", title, heading)
			}
			if !strings.Contains(heading, `aria-label="`+closeName+`"`) {
				t.Errorf("the close control lost its accessible name %q: %s", closeName, heading)
			}
			if !strings.Contains(heading, `aria-label="`+chatux008Text(m, "notify")+`"`) {
				t.Errorf("the notify control lost its accessible name: %s", heading)
			}
			// Title, then channel context, then actions: the DOM order is the RTL order.
			chatux005Before(t, heading, "<h2>"+title+"</h2>", `class="thread-channel-link"`, `data-action="follow"`, `data-action="close-thread"`)
		})
	}
}

// The phone rules must win the cascade in the stylesheet the page actually
// ships: the close label hides, the title cannot shrink, and only the channel
// link truncates, with no later rule putting the label or a shrinking title back.
func TestIssue3598_PhoneCascadeKeepsTitleWholeAndHidesCloseLabel(t *testing.T) {
	sheet := Stylesheet
	hide := "@media(max-width:767px){.chat-workspace .thread-back-label{display:none}}"
	if !strings.Contains(ChatUX032Styles, hide) {
		t.Fatalf("the phone thread header still shows the close label")
	}
	// Any other rule that shows the label at phone widths must be less specific
	// than the hide rule (one class against two), so it cannot win.
	for _, m := range regexp.MustCompile(`([^{}]*)\.thread-back-label\{display:(inline|inline-flex|block|flex)`).FindAllStringSubmatch(sheet, -1) {
		if strings.Contains(m[1], ".chat-workspace .thread-back-label") || strings.Contains(m[1], "chat-panel-head") {
			t.Errorf("a rule at least as specific as the hide rule shows the close label: %q", m[0])
		}
	}
	title := regexp.MustCompile(`\.thread-heading-text h2\{[^}]*\}`).FindAllString(ChatUX032Styles, -1)
	if len(title) != 1 || !strings.Contains(title[0], "flex:0 0 auto") {
		t.Errorf("the thread title can shrink: %v", title)
	}
	for _, rule := range regexp.MustCompile(`[^{}]*thread-heading-text[^{}]*h2[^{}]*\{[^}]*\}`).FindAllString(sheet+ChatUX032ThreadStyles, -1) {
		if strings.Contains(rule, "flex-shrink:1") || strings.Contains(rule, "flex:0 1") || strings.Contains(rule, "flex:1") {
			t.Errorf("a later rule lets the thread title shrink: %q", rule)
		}
	}
	if !strings.Contains(ChatUX032Styles, ".chat-panel-head .side-heading-actions{flex:none") {
		t.Errorf("the header actions can be squeezed by a long channel name")
	}
	if !strings.Contains(ChatUX032Styles, ".chat-panel-head .thread-channel-link{flex:0 1 auto;min-width:0") &&
		!strings.Contains(ChatUX032Styles, ".chat-panel-head .chat-panel-sub,.chat-panel-head .thread-channel-link{flex:0 1 auto;min-width:0") {
		t.Errorf("the channel link is not the shrinking part of the header")
	}
}
