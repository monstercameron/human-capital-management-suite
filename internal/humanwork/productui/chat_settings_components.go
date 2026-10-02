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
}

func ChatSettingsPage(props ChatSettingsPageProps) ui.Node {
	var reactions ui.Node
	if props.AgentReactions != nil {
		reactions = ui.CreateElement(AgentAnswerReactionSettings, *props.AgentReactions)
	}
	return html.Div(html.Props{Class: "chat-settings-page"}, props.Settings, reactions, props.Translation, props.Location, props.Voice)
}
