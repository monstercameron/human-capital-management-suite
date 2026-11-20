package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

var chatcmd003Issue3976Copy = map[string][3]string{
	"split-label": {"This line", "Diese Zeile", "هذا السطر"},
	"split-auto":  {"Split at commas", "An Kommas trennen", "التقسيم عند الفواصل"},
	"split-one":   {"Keep as one task", "Als eine Aufgabe behalten", "الإبقاء كمهمة واحدة"},
}

func init() {
	for key, value := range chatcmd003Issue3976Copy {
		chatcmd003Copy[key] = value
	}
}

// chatcmd003SplitChoice is the choice between a loosely typed to-do line read
// as several tasks and kept as the one task it was typed as. It is shown when
// the line was split, and while the choice to keep it whole is on, so it can
// be undone. The text of the line is not touched.
func chatcmd003SplitChoice(m Model, state chatcmd003Preview) ui.Node {
	if state.Draft.Card.Todo == nil {
		return nil
	}
	current := state.Settings["split"]
	split := false
	for _, change := range state.Draft.Changes {
		split = split || change.Kind == chat.Chatcmd004ChangeSplit
	}
	if !split && current != "one" {
		return nil
	}
	if current != "one" {
		current = "auto"
	}
	return chatcmd003Choice(m, "split", "split-label", current, state.Busy, "split-", nil, "auto", "one")
}
