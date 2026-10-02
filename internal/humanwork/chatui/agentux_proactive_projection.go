package chatui

import (
	"encoding/base64"
	"encoding/json"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"strings"
)

const AgentAnnouncementBodyPrefix = "<hcm_agent_announcement>\n"

// AnnouncementMessageBody stores owner attribution beside the exact text and
// source titles. Human bodies never select this renderer: the author projection
// must independently attest an agent identity.
func AnnouncementMessageBody(message AgentAnnouncementMessage) (string, error) {
	raw, err := json.Marshal(message)
	return AgentAnnouncementBodyPrefix + "v2:" + base64.RawURLEncoding.EncodeToString(raw), err
}

func agentAnnouncementProjectedBody(model Model, message Message, fallback ui.Node) ui.Node {
	if message.PersonaActor == nil || !message.PersonaActor.valid() || !strings.HasPrefix(message.Body, AgentAnnouncementBodyPrefix) {
		return fallback
	}
	announcement, ok := DecodeAnnouncementMessageBody(message.Body)
	if !ok {
		return fallback
	}
	announcement.AgentName = message.Author
	announcement.identityRendered = true
	return RenderAgentAnnouncementMessage(model, announcement)
}

// DecodeAnnouncementMessageBody accepts the original envelope for stored posts,
// and a URL-free encoding for new posts so Chat's reference scanners cannot
// turn an envelope field into a document reference or an extra preview card.
func DecodeAnnouncementMessageBody(body string) (AgentAnnouncementMessage, bool) {
	var announcement AgentAnnouncementMessage
	if !strings.HasPrefix(body, AgentAnnouncementBodyPrefix) {
		return announcement, false
	}
	raw := []byte(strings.TrimPrefix(body, AgentAnnouncementBodyPrefix))
	if strings.HasPrefix(string(raw), "v2:") {
		var err error
		raw, err = base64.RawURLEncoding.DecodeString(string(raw[3:]))
		if err != nil {
			return announcement, false
		}
	}
	err := json.Unmarshal(raw, &announcement)
	return announcement, err == nil && strings.TrimSpace(announcement.OwnerName) != "" && strings.TrimSpace(announcement.Text) != ""
}

func agentAnnouncementProjectedIdentity(model Model, message Message) Message {
	if _, ok := DecodeAnnouncementMessageBody(message.Body); !ok {
		return message
	}
	// Presence in the resolved catalog proves this author is an installed agent.
	// A human body alone, even one containing a forged name, grants no identity.
	for _, persona := range model.ResolvedPersonaMentions {
		if persona.Reference.Kind != "AGENT_MENTION" || persona.Reference.ID != message.AuthorID {
			continue
		}
		actor := PersonaActor{PersonaID: persona.Reference.ID, AgentID: persona.Reference.ID, PersonaVersion: persona.Version, Trusted: true, Icon: persona.Icon, IconRevision: persona.IconRevision}
		if persona.Actor.valid() {
			actor = *persona.Actor
			actor.Icon, actor.IconRevision = persona.Icon, persona.IconRevision
		}
		message.PersonaActor, message.Author = &actor, persona.Reference.Display
		break
	}
	return unattestedAnnouncementIdentity(model, message)
}
