//go:build !(js && wasm)

package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

// An agent with an uploaded image wears it over its generated icon, hidden
// from assistive technology; without one the avatar is the icon alone.
func TestAgentUX069_AvatarShowsUpload(t *testing.T) {
	icon := agenticon.Generate(agenticon.Input{Name: "Policy Helper"})
	markup, err := ui.RenderToString(agentAvatar("Policy Helper", "avatar", "/api/agent-controls/icons/image?persona_id=policy&revision=2", icon))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `src="/api/agent-controls/icons/image?persona_id=policy&amp;revision=2"`) && !strings.Contains(markup, `src="/api/agent-controls/icons/image?persona_id=policy&revision=2"`) {
		t.Fatal("uploaded image not drawn", markup)
	}
	if !strings.Contains(markup, `aria-hidden="true"`) || !strings.Contains(markup, "<svg") {
		t.Fatal("generated icon fallback lost", markup)
	}
	plain, err := ui.RenderToString(agentAvatar("Policy Helper", "avatar", "", icon))
	if err != nil || strings.Contains(plain, "<img") {
		t.Fatal("generated icon gained an image", plain, err)
	}
	pending, err := ui.RenderToString(agentAvatar("Policy Helper", "avatar", "/image", agenticon.Value{}))
	if err != nil || !strings.Contains(pending, "agent-icon-pending") || strings.Contains(pending, "<img") {
		t.Fatal("unknown agent drew a picture", pending, err)
	}
}

// The uploaded image is found wherever the model carries the agent, and a
// missing one resolves to nothing rather than another agent's picture.
func TestAgentUX069_AvatarImageLookup(t *testing.T) {
	m := Model{Conversations: []Conversation{{ID: "dm", Kind: DirectMessage, Agent: true, AgentID: "agent:policy", Name: "Policy Helper", IconImageURL: "/image/policy"}}}
	if got := conversationAgentIconImage(m, m.Conversations[0]); got != "/image/policy" {
		t.Fatal("direct conversation image", got)
	}
	if got := agentIconImageFor(m, []string{"agent:other"}, "Other", ""); got != "" {
		t.Fatal("another agent's picture borrowed", got)
	}
	if got := agentIconImageFor(m, []string{"agent:policy"}, "Policy Helper", "/image/held"); got != "/image/held" {
		t.Fatal("held image ignored", got)
	}
}
