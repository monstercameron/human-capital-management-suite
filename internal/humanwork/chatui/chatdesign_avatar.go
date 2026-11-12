package chatui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// chatDesignAvatarHues is the size of the person-avatar palette (#3770).
const chatDesignAvatarHues = 8

// AvatarHue is the palette slot, 1 to 8, a person's initials are drawn on. It
// is taken from the person id with FNV-1a, never from the name, so renaming a
// person or two people sharing a name does not move or merge their colours.
// An empty id answers 0: the neutral avatar. The server render and the browser
// client both call this function, so a row never changes colour on hydration.
func AvatarHue(id string) int {
	id = strings.TrimSpace(id)
	if id == "" {
		return 0
	}
	hash := uint32(2166136261)
	for index := 0; index < len(id); index++ {
		hash ^= uint32(id[index])
		hash *= 16777619
	}
	return int(hash%chatDesignAvatarHues) + 1
}

// personAvatarFor draws one person: the initials on that person's palette
// colour, in a round slot, with the portrait over them when there is one. The
// round slot is the person shape; an agent keeps the bordered square
// (agentAvatar), so the two are told apart before the glyph is read.
func personAvatarFor(id, name, class, photoURL string) ui.Node {
	children := []ui.Node{ui.Text(initials(name))}
	if url := chatPhotoURL(photoURL); url != "" {
		children = append(children, html.Img(html.Props{Class: "chat-avatar-photo", Src: url, Loading: "lazy", Width: "64", Height: "64", OnError: chatPhotoErrorHandler(), OnLoad: chatPhotoLoadHandler(), Raw: map[string]any{"alt": "", "decoding": "async"}}))
	}
	return html.Span(html.Props{Class: class, Aria: map[string]string{"hidden": "true"}, Data: map[string]string{"avatar-hue": strconv.Itoa(AvatarHue(id))}}, children...)
}

// chatDesignAvatarStyles draws a person (#3770): the round slot, and the
// palette pair of the slot the person id picked. The palette itself is a set
// of shell tokens (--hcm-avatar-N-bg and --hcm-avatar-N-ink, light and dark,
// declared with the product theme), in order teal, blue, violet, magenta, red,
// amber, olive, slate. Slot 0, a person without an id, keeps the neutral
// avatar. Print keeps the neutral avatar too: the palette is identity on
// screen, not information.
const chatDesignAvatarStyles = `.chat-workspace [data-avatar-hue]{border-radius:50%}` +
	`.chat-workspace [data-avatar-hue="1"]{background:var(--hcm-avatar-1-bg);color:var(--hcm-avatar-1-ink)}.chat-workspace [data-avatar-hue="2"]{background:var(--hcm-avatar-2-bg);color:var(--hcm-avatar-2-ink)}.chat-workspace [data-avatar-hue="3"]{background:var(--hcm-avatar-3-bg);color:var(--hcm-avatar-3-ink)}.chat-workspace [data-avatar-hue="4"]{background:var(--hcm-avatar-4-bg);color:var(--hcm-avatar-4-ink)}.chat-workspace [data-avatar-hue="5"]{background:var(--hcm-avatar-5-bg);color:var(--hcm-avatar-5-ink)}.chat-workspace [data-avatar-hue="6"]{background:var(--hcm-avatar-6-bg);color:var(--hcm-avatar-6-ink)}.chat-workspace [data-avatar-hue="7"]{background:var(--hcm-avatar-7-bg);color:var(--hcm-avatar-7-ink)}.chat-workspace [data-avatar-hue="8"]{background:var(--hcm-avatar-8-bg);color:var(--hcm-avatar-8-ink)}` +
	`@media print{.chat-workspace [data-avatar-hue]{background:var(--soft);color:var(--accent)}}`
