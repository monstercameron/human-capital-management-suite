package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// agentActionButtonProps are the inputs of one button on the access pages that
// runs a client call when it is pressed.
type agentActionButtonProps struct {
	Props html.Props
	Label string
	Run   func()
}

// agentActionButton is a component so that its click handler is a hook of its
// own fiber. The access pages were drawing these buttons from plain helper
// functions, calling ui.UseEvent in a loop and under a condition; in the
// browser that is a framework panic, the page never left "Loading your agent
// access" and no card could be drawn.
func agentActionButton(props agentActionButtonProps) ui.Node {
	run := props.Run
	onClick := ui.UseEvent(func(ui.MouseEvent) {
		if run != nil {
			run()
		}
	})
	button := props.Props
	button.OnClick = onClick
	return html.Button(button, ui.Text(props.Label))
}

// agentRunButton draws one such button.
func agentRunButton(props html.Props, label string, run func()) ui.Node {
	return ui.CreateElement(agentActionButton, agentActionButtonProps{Props: props, Label: label, Run: run})
}
