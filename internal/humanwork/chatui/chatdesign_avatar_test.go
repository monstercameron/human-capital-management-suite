//go:build !(js && wasm)

package chatui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

// #3770: the colour comes from the person id. It is stable, it stays inside the
// palette, it ignores the name, and the ids of a team spread over every slot.
func TestChatDesign_3770_AvatarHueIsStablePerPersonID(t *testing.T) {
	if got := AvatarHue(""); got != 0 {
		t.Fatalf("an unknown person got slot %d, want the neutral 0", got)
	}
	if got := AvatarHue("  "); got != 0 {
		t.Fatalf("a blank id got slot %d, want the neutral 0", got)
	}
	// Pinned so the server and an older cached client can never disagree
	// silently: these are FNV-1a(id) mod 8, plus one.
	for id, want := range map[string]int{"report-b": 1, "alex": 6, "u-1": 3, "u-2": 8, "a": 5, "b": 6} {
		if got := AvatarHue(id); got != want {
			t.Errorf("AvatarHue(%q) = %d, want %d", id, got, want)
		}
		if AvatarHue(id) != AvatarHue(" "+id+" ") {
			t.Errorf("AvatarHue(%q) changes with surrounding space", id)
		}
	}
	seen := map[int]bool{}
	for index := 0; index < 64; index++ {
		slot := AvatarHue("person-" + strconv.Itoa(index))
		if slot < 1 || slot > chatDesignAvatarHues {
			t.Fatalf("slot %d is outside the palette", slot)
		}
		seen[slot] = true
	}
	if len(seen) != chatDesignAvatarHues {
		t.Fatalf("64 people used %d of %d colours", len(seen), chatDesignAvatarHues)
	}
	first, err := ui.RenderToString(personAvatar(Model{}, "u-1", "Sam Lee", "avatar"))
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := ui.RenderToString(personAvatar(Model{}, "u-1", "Samira Okafor", "avatar"))
	if err != nil {
		t.Fatal(err)
	}
	namesake, err := ui.RenderToString(personAvatar(Model{}, "u-2", "Sam Lee", "avatar"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, `data-avatar-hue="3"`) || !strings.Contains(renamed, `data-avatar-hue="3"`) {
		t.Fatalf("renaming a person moved the colour: %s / %s", first, renamed)
	}
	if !strings.Contains(namesake, `data-avatar-hue="8"`) {
		t.Fatalf("two people with one name share a colour: %s", namesake)
	}
}

// The portrait still wins, and an agent is never drawn in the person shape.
func TestChatDesign_3770_PortraitWinsAndAgentsKeepTheirOwnShape(t *testing.T) {
	photo, err := ui.RenderToString(personAvatar(Model{PhotoURLs: map[string]string{"u-1": "/photos/u-1"}}, "u-1", "Sam Lee", "avatar"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(photo, `<img`) || !strings.Contains(photo, `src="/photos/u-1"`) || !strings.Contains(photo, `>SL<`) {
		t.Fatalf("the portrait, or the initials under it, were lost: %s", photo)
	}
	message, err := ui.RenderToString(chatux047MessageAvatar(Model{}, Message{AuthorID: "u-2", Author: "Sam Lee"}, "avatar"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(message, `class="avatar" data-avatar-hue="8"`) {
		t.Fatalf("a message author is not drawn on the colour of the author id: %s", message)
	}
	unknown, err := ui.RenderToString(personAvatarWithPhoto("Sam Lee", "avatar", ""))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unknown, `data-avatar-hue="0"`) {
		t.Fatalf("a person without an id is not the neutral person avatar: %s", unknown)
	}
	agent, err := ui.RenderToString(agentAvatar("Policy Helper", "avatar agent-dm-avatar", "", agenticon.Generate(agenticon.Input{Name: "Policy Helper"})))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(agent, "data-avatar-hue") || !strings.Contains(agent, "agent-dm-avatar") {
		t.Fatalf("an agent is drawn as a person: %s", agent)
	}
	for _, want := range []string{
		`.chat-workspace [data-avatar-hue]{border-radius:50%}`,
		`.chat-workspace [data-avatar-hue="1"]{background:var(--hcm-avatar-1-bg);color:var(--hcm-avatar-1-ink)}`,
		`.chat-workspace [data-avatar-hue="8"]{background:var(--hcm-avatar-8-bg);color:var(--hcm-avatar-8-ink)}`,
		`@media print{.chat-workspace [data-avatar-hue]{background:var(--soft);color:var(--accent)}}`,
	} {
		if !strings.Contains(Stylesheet, want) {
			t.Errorf("Chat stylesheet is missing %q", want)
		}
	}
	// The person shape must not reach the agent: no rule rounds .agent-dm-avatar.
	if regexp.MustCompile(`agent-dm-avatar[^{}]*\{[^}]*border-radius:50%`).MatchString(Stylesheet) {
		t.Error("an agent avatar is round, the person shape")
	}
}
