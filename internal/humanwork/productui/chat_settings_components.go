package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type ChatSettingsPageProps struct {
	Settings ui.Node
}

func ChatSettingsPage(props ChatSettingsPageProps) ui.Node {
	return html.Div(html.Props{Class: "chat-settings-page"}, props.Settings)
}
