package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type ChatSettingsPageProps struct {
	Settings       ui.Node
	AgentReactions *AgentAnswerReactionSettingsProps
	// Translation is the workspace's translation settings and glossary
	// (CHATLANG-007): administration, so it lives here and not in one channel's
	// details.
	Translation ui.Node
	// Location is the workspace's location sharing settings (CHATMAP-006).
	Location ui.Node
	// Voice is the workspace's voice message switch and engine (CHATVOICE-005).
	Voice ui.Node
	// Reactions is the workspace's agent reaction switch (AGENTUX-052).
	Reactions ui.Node
	// Audit is the workspace's audit log of who did what in Chat (#3780).
	Audit ui.Node
	// RetentionPreview is the read-only preview of what the saved retention
	// policy would remove (#3778).
	RetentionPreview ui.Node
	// Barriers is the workspace's information barriers between teams (#3783).
	Barriers ui.Node
	// Holds is the workspace's legal holds on conversations (#3779).
	Holds ui.Node
}

func ChatSettingsPage(props ChatSettingsPageProps) ui.Node {
	var reactions ui.Node
	if props.AgentReactions != nil {
		reactions = ui.CreateElement(AgentAnswerReactionSettings, *props.AgentReactions)
	}
	return html.Div(html.Props{Class: "chat-settings-page"}, props.Settings, props.RetentionPreview, reactions, props.Translation, props.Location, props.Voice, props.Reactions, props.Barriers, props.Holds, props.Audit)
}
