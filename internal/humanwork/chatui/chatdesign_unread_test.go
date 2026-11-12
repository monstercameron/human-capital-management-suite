//go:build !(js && wasm)

package chatui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func chatDesignRender(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

// #3772: a count in the conversation list is a pill. A channel's unread count
// is the neutral pill; a mention and a direct message take the accent fill.
// The number keeps saying what it counts in every locale.
func TestChatDesign_3772_UnreadCountsSitInPills(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := Model{Locale: locale, SelectedID: "elsewhere", Callbacks: Callbacks{SelectConversation: func(string) {}}}
		unreadName := m.tf(KeyUnreadCount, map[string]string{"n": m.n(3)})
		mentionName := m.tf(KeyMentionCount, map[string]string{"n": m.n(2)})
		if unreadName == m.n(3) || mentionName == m.n(2) || unreadName == mentionName {
			t.Fatalf("%s: the count names do not say what is counted: %q / %q", locale, unreadName, mentionName)
		}
		channel := chatDesignRender(t, railRow(m, Conversation{ID: "c", Name: "general", Kind: PublicChannel, Joined: true, Unread: 3}))
		if !strings.Contains(channel, `aria-label="`+unreadName+`" class="chat-badge">`) {
			t.Errorf("%s: a channel's unread count is not the neutral pill with its name: %s", locale, channel)
		}
		direct := chatDesignRender(t, railRow(m, Conversation{ID: "d", Name: "Sam Lee", Kind: DirectMessage, Joined: true, Unread: 3}))
		if !strings.Contains(direct, `aria-label="`+unreadName+`" class="chat-badge direct">`) {
			t.Errorf("%s: a direct message's unread count is not marked for the accent pill: %s", locale, direct)
		}
		mention := chatDesignRender(t, railRow(m, Conversation{ID: "m", Name: "general", Kind: PublicChannel, Joined: true, Unread: 5, Mentions: 2}))
		if !strings.Contains(mention, `aria-label="`+mentionName+`" class="chat-badge mention">`) {
			t.Errorf("%s: a mention count lost its name or its class: %s", locale, mention)
		}
		// Saved and Moderation are counts of the same list and take the same pill.
		m.SavedOpenCount = 2
		m.Moderation = ModerationState{Ready: true, Moderator: true, Open: 4}
		saved := chatDesignRender(t, chatsaveSidebar(m))
		if !regexp.MustCompile(`class="chat-row chatsave-sidebar-row"[^>]*>.*class="chat-count chatsave-count"`).MatchString(saved) {
			t.Errorf("%s: the Saved count is not a direct count of its row: %s", locale, saved)
		}
		moderation := chatDesignRender(t, chatmod005Sidebar(m))
		if !strings.Contains(moderation, `class="chat-count chatmod005-count"`) || !strings.Contains(moderation, ModerationCountLabel(locale, 4, true)) {
			t.Errorf("%s: the Moderation count lost its class or its spoken meaning: %s", locale, moderation)
		}
	}

	pill := `.chat-workspace :is(.chat-rail-row .chat-badge,.section-title .chat-badge.section-unread,.chatsave-sidebar-row>.chat-count:not([hidden])){`
	accent := `.chat-workspace :is(.chat-rail-row,.section-title) .chat-badge:is(.mention,.direct){background:var(--accent);color:var(--hcm-color-on-brand)}`
	at, accentAt := strings.Index(Stylesheet, pill), strings.Index(Stylesheet, accent)
	if at < 0 || accentAt < at {
		t.Fatalf("the pill rule and, after it, the accent rule are not in the Chat stylesheet (%d, %d)", at, accentAt)
	}
	body := Stylesheet[at+len(pill):]
	body = body[:strings.Index(body, "}")]
	for _, want := range []string{"border-radius:999px", "background:var(--chat-pill-neutral)", "color:var(--ink)", "min-inline-size:20px", "block-size:20px"} {
		if !strings.Contains(";"+body+";", ";"+want+";") {
			t.Errorf("the pill rule does not declare %s: %s", want, body)
		}
	}
	// RTL: the pill is laid out with logical properties only.
	if regexp.MustCompile(`(^|;)(margin|padding)-(left|right)|(^|;)(left|right):`).MatchString(body) {
		t.Errorf("the pill rule uses a physical side: %s", body)
	}
	// The pill is declared after every earlier look of the same counts, with at
	// least their specificity, so it is the one that paints.
	for _, earlier := range []string{`.chat-rail-row .chat-badge{`, `.chat-rail-row .chat-badge.mention{`, `.section-title .chat-badge.section-unread{`, `.chat-workspace .chat-row.selected .chat-badge{`, `.chat-workspace .chatmod005-row>.chat-count{`} {
		if last := strings.LastIndex(Stylesheet, earlier); last < 0 || last > at {
			t.Errorf("%q is missing or declared after the pill (%d > %d)", earlier, last, at)
		}
	}
	if strings.Contains(chatDesignUnreadStyles, "danger") {
		t.Error("a count in the list uses the alert colour")
	}
	if !strings.Contains(Stylesheet, `.chat-workspace{--chat-pill-neutral:color-mix(in srgb,var(--ink) 14%,var(--surface))}`) {
		t.Error("the neutral pill fill is not derived from the theme's own text and surface colours")
	}
}
