package chatui

import (
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The composer's slash commands (CHATCMD-001, client half).
//
// A command is one entry in a registry: its name, a one-line description (a copy
// key), an example, where it is available and what it does. Typing "/" at the
// start of an empty draft lists what is available here; Enter or Tab writes the
// chosen name and a space; a command typed and sent runs and is never posted; a
// word the registry does not hold is not posted either, the composer says so.
// A line that starts with "//" is a message that starts with one slash.
//
// What is not here yet: arguments beyond free text, the preview step, a usage
// hint once a command is chosen, the closest-match suggestion, the versioned
// registration interface and the server's own registry. CHATCMD-002 to 004 add
// /poll and /todo on top of this.

// composerCommandRuntime is what a command may touch: the model it was typed
// into and a few actions of the composer. Keeping them behind this value lets a
// command be run and checked without a browser.
type composerCommandRuntime struct {
	Model Model
	// ClearDraft empties the composer; a command that consumed its line calls it.
	ClearDraft func()
	// Notice shows one quiet line above the composer; the next keystroke clears it.
	Notice func(string)
	// OpenGiphy opens the GIF picker searching for query ("" is the trending list).
	OpenGiphy func(query string)
	// OpenLocation opens the location sheet.
	OpenLocation func()
}

// composerCommand is one registered command.
type composerCommand struct {
	// Name is what follows the slash, lower case.
	Name string
	// Example is a typical line, shown as the row's tooltip.
	Example string
	// Description is the copy key of the one-line description the menu shows.
	Description string
	// Available says whether the command exists in this conversation; nil means
	// everywhere. A command that is not available is absent from the menu and
	// not run.
	Available func(Model) bool
	// Run does the command with the text after its name.
	Run func(rt composerCommandRuntime, args string)
}

// composerCommandRegistry holds the commands. It is a value built where it is
// used, never shared state.
type composerCommandRegistry struct{ commands []composerCommand }

// defaultComposerCommands is the registry of the commands that work today.
func defaultComposerCommands() composerCommandRegistry {
	return composerCommandRegistry{commands: []composerCommand{
		{Name: "poll", Example: `/poll "Where?" 1="Here" 2="There"`, Description: keyComposerAddPoll, Available: chatcmd003Available, Run: chatcmd003RegistryRun},
		{Name: "todo", Example: `/todo "Checklist" 1="Book the room" 2="Send invites @Dana by Friday"`, Description: keyComposerAddTodo, Available: chatcmd003Available, Run: chatcmd003RegistryRun},
		{Name: "giphy", Example: "/giphy cats", Description: keyComposerCommandGiphy, Run: runGiphyCommand},
		{Name: "location", Example: "/location", Description: keyComposerCommandPlace, Available: composerLocationAvailable, Run: runLocationCommand},
	}}
}

// runGiphyCommand opens the GIF picker searching for the line's text; without a
// configured key it says why nothing opened.
func runGiphyCommand(rt composerCommandRuntime, args string) {
	if strings.TrimSpace(rt.Model.GiphyAPIKey) == "" {
		rt.Notice(rt.Model.t(KeyGiphyUnavailable))
		return
	}
	rt.ClearDraft()
	rt.OpenGiphy(LimitGiphyQuery(strings.TrimSpace(args)))
}

// runLocationCommand opens the location sheet.
func runLocationCommand(rt composerCommandRuntime, _ string) {
	rt.ClearDraft()
	rt.OpenLocation()
}

// validComposerCommandName reports whether a word can be a command name: ASCII
// letters, digits, "_" and "-", starting with a letter. A path such as /usr/bin
// is therefore not a command.
func validComposerCommandName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		letter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		if !letter && (i == 0 || !(r >= '0' && r <= '9' || r == '_' || r == '-')) {
			return false
		}
	}
	return true
}

// parseComposerCommand reads "/name text". It reports false for anything that
// does not start with one slash and a valid name followed by a space or the end.
func parseComposerCommand(body string) (name, args string, ok bool) {
	body = strings.TrimSpace(body)
	if len(body) < 2 || body[0] != '/' {
		return "", "", false
	}
	end := strings.IndexFunc(body, unicode.IsSpace)
	word := body[1:]
	if end >= 0 {
		word = body[1:end]
	}
	if !validComposerCommandName(word) {
		return "", "", false
	}
	if end < 0 {
		return word, "", true
	}
	return word, strings.TrimSpace(body[end:]), true
}

func (r composerCommandRegistry) lookup(m Model, name string) (composerCommand, bool) {
	for _, command := range r.commands {
		if strings.EqualFold(command.Name, name) && (command.Available == nil || command.Available(m)) {
			return command, true
		}
	}
	return composerCommand{}, false
}

// menu lists the commands available here that match what was typed after the
// slash: names that start with it first, then names that contain it.
func (r composerCommandRegistry) menu(m Model, query string) []composerCommand {
	query = strings.ToLower(query)
	var starts, contains []composerCommand
	for _, command := range r.commands {
		if command.Available != nil && !command.Available(m) {
			continue
		}
		name := strings.ToLower(command.Name)
		switch {
		case strings.HasPrefix(name, query):
			starts = append(starts, command)
		case strings.Contains(name, query):
			contains = append(contains, command)
		}
	}
	return append(starts, contains...)
}

// composerCommandOutcome is what the registry did with a line being sent.
type composerCommandOutcome int

const (
	// composerCommandText: not a command; post the returned text.
	composerCommandText composerCommandOutcome = iota
	// composerCommandRan: a command ran and consumed the line; post nothing.
	composerCommandRan
	// composerCommandUnknown: it names no command available here; post nothing.
	composerCommandUnknown
)

// dispatch runs a line being sent. It returns what happened and, for ordinary
// text, the text to post: a line starting with "//" loses one slash.
func (r composerCommandRegistry) dispatch(rt composerCommandRuntime, body string) (composerCommandOutcome, string) {
	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(trimmed, "//") {
		return composerCommandText, trimmed[1:]
	}
	name, args, ok := parseComposerCommand(trimmed)
	if !ok {
		return composerCommandText, body
	}
	command, found := r.lookup(rt.Model, name)
	if !found {
		return composerCommandUnknown, body
	}
	command.Run(rt, args)
	return composerCommandRan, body
}

// composerCommandMenu is the open command list. Query is what was typed after
// the slash.
type composerCommandMenu struct {
	Target, Query string
	Open          bool
	Active        int
}

// composerCommandToken reports the command word being typed: the text of the
// draft must start with a slash, and the caret (UTF-16 units) must end its
// first word, so a caret in the middle of a longer line, or after the first
// space, opens nothing.
func composerCommandToken(value string, caret int) (query string, ok bool) {
	units := utf16.Encode([]rune(value))
	if caret < 1 || caret > len(units) || units[0] != '/' {
		return "", false
	}
	if caret < len(units) && !unicode.IsSpace(rune(units[caret])) {
		return "", false
	}
	query = string(utf16.Decode(units[1:caret]))
	if query != "" && !validComposerCommandName(query) {
		return "", false
	}
	return query, true
}

// composerCommandMenuNext is the list after the draft or the caret changed: open
// while a command word is being typed and at least one command matches, closed
// otherwise. The highlighted row stays while the query stays.
func composerCommandMenuNext(m Model, current composerCommandMenu, target, value string, caret int) composerCommandMenu {
	query, ok := composerCommandToken(value, caret)
	if !ok || len(defaultComposerCommands().menu(m, query)) == 0 {
		return composerCommandMenu{}
	}
	next := composerCommandMenu{Target: target, Query: query, Open: true}
	if current.Open && current.Target == target && current.Query == query {
		next.Active = current.Active
	}
	return next
}

// commandMenuTrack follows the composer: it opens, filters or closes the list as
// the draft changes.
func commandMenuTrack(m Model, local localStore, target string) {
	current := local.get().commandMenu
	next := composerCommandMenu{}
	if value, caret, ok := composerSelection(target); ok {
		next = composerCommandMenuNext(m, current, target, value, caret)
	}
	if next != current {
		local.update(func(u *localUI) { u.commandMenu = next })
	}
}

// composerCommandMenuMove is the list's answer to Arrow keys and Escape.
// Enter and Tab are the composer's key action (composerPickCommand).
func composerCommandMenuMove(state composerCommandMenu, key string, count int) (composerCommandMenu, bool) {
	if !state.Open {
		return state, false
	}
	switch key {
	case "ArrowDown", "ArrowUp":
		if count > 0 {
			delta := 1
			if key == "ArrowUp" {
				delta = -1
			}
			state.Active = nextMention(state.Active, delta, count)
		}
		return state, true
	case "Escape":
		// Closes the list and leaves the text as it is.
		return composerCommandMenu{}, true
	}
	return state, false
}

// commandMenuKey routes the list's keys while it is open and reports whether the
// key was the list's.
func commandMenuKey(m Model, local localStore, key, target string) bool {
	state := local.get().commandMenu
	if !state.Open || state.Target != target {
		return false
	}
	next, handled := composerCommandMenuMove(state, key, len(defaultComposerCommands().menu(m, state.Query)))
	if handled && next != state {
		local.update(func(u *localUI) { u.commandMenu = next })
	}
	return handled
}

// composerCommandApply is the draft after a command is chosen: the word being
// typed becomes "/name " and the caret lands after the space.
func composerCommandApply(value string, caret int, name string) (string, int) {
	units := utf16.Encode([]rune(value))
	end := caret
	if end < len(units) && units[end] == ' ' {
		// A space already follows the word; it becomes the one after the name.
		end++
	}
	return composerInsertAt(value, "/"+name+" ", 0, end)
}

// commandMenuPick writes the chosen command into the composer it was typed in,
// re-reading the field so the replacement lands on the word it holds now.
func commandMenuPick(m Model, local localStore, index int) {
	state := local.get().commandMenu
	local.update(func(u *localUI) { u.commandMenu = composerCommandMenu{} })
	if !state.Open {
		return
	}
	options := defaultComposerCommands().menu(m, state.Query)
	if index < 0 || index >= len(options) {
		return
	}
	value, caret, ok := composerSelection(state.Target)
	if !ok {
		return
	}
	if query, found := composerCommandToken(value, caret); !found || query != state.Query {
		return
	}
	updated, next := composerCommandApply(value, caret, options[index].Name)
	replaceComposerText(state.Target, updated, next)
}

// composerCommandMenuView is the list above the composer, in the mention list's
// place and style; the field keeps focus and owns the keys.
func composerCommandMenuView(m Model, state composerCommandMenu, target string) ui.Node {
	if !state.Open || state.Target != target {
		return html.Div(html.Props{Class: "command-menu-slot"})
	}
	options := defaultComposerCommands().menu(m, state.Query)
	if len(options) == 0 {
		return html.Div(html.Props{Class: "command-menu-slot"})
	}
	active := state.Active
	if active < 0 || active >= len(options) {
		active = 0
	}
	label := composerText(m, keyComposerCommands)
	rows := []ui.Node{html.P(html.Props{Class: "mention-heading", Aria: map[string]string{"hidden": "true"}, Text: label})}
	for i, command := range options {
		class := "mention-option command-option"
		if i == active {
			class += " active"
		}
		rows = append(rows, html.WithKey(html.Button(html.Props{ID: target + "-command-" + itoa(i+1), Class: class, Type: "button", Role: "option", TabIndex: -1, Title: command.Example,
			Data: map[string]string{"action": "command-pick", "id": target, "extra": itoa(i + 1)},
			Aria: map[string]string{"selected": boolString(i == active)}},
			html.Span(html.Props{Class: "mention-name", Dir: "ltr", Text: "/" + command.Name}),
			html.Span(html.Props{Class: "mention-detail", Dir: "auto", Text: composerText(m, command.Description)})), "command:"+command.Name))
	}
	return html.Div(html.Props{ID: target + "-commands", Class: "mention-menu command-menu", Role: "listbox", Dir: agentReplyDirection(m.Locale), Aria: map[string]string{"label": label}},
		append(rows, agentMentionKeyboardHint(m.Locale, false))...)
}

// commandMenuFieldAria points the composer at the open command list.
func commandMenuFieldAria(state composerCommandMenu, target string, aria map[string]string) map[string]string {
	if !state.Open || state.Target != target {
		return aria
	}
	out := map[string]string{}
	for k, v := range aria {
		out[k] = v
	}
	out["autocomplete"] = "list"
	out["controls"] = target + "-commands"
	out["activedescendant"] = target + "-command-" + itoa(state.Active+1)
	return out
}

// composerUnknownNotice is the quiet line shown for a "/word" the registry does
// not hold here: it names the word and says how to send it as text.
func composerUnknownNotice(m Model, name string) string {
	return strings.ReplaceAll(composerText(m, keyComposerCommandUnknown), "{name}", name)
}

// composerSendCommand runs the registry over a line being sent from the
// composer. consumed is true when a command ran or the word names none here (the
// composer has said so): nothing is posted then. Otherwise text is what to post,
// which loses one slash when the line started with "//".
func composerSendCommand(rt composerCommandRuntime, body string) (text string, consumed bool) {
	outcome, text := defaultComposerCommands().dispatch(rt, body)
	switch outcome {
	case composerCommandRan:
		return "", true
	case composerCommandUnknown:
		name, _, _ := parseComposerCommand(body)
		rt.Notice(composerUnknownNotice(rt.Model, name))
		return "", true
	}
	return text, false
}

// newComposerCommandRuntime is what a command run from the chat composer may
// touch: it empties the composer and its draft, opens the GIF picker and the
// location sheet, and shows its one quiet line above the composer.
func newComposerCommandRuntime(m Model, local localStore, giphy *giphyPickerViews, drafts *browserDrafts, target string) composerCommandRuntime {
	return composerCommandRuntime{
		Model: m,
		ClearDraft: func() {
			setDOMValue(target, "")
			drafts.set(m.SelectedID, "")
			if m.Callbacks.DraftChanged != nil {
				m.Callbacks.DraftChanged(m.SelectedID, "")
			}
			local.update(func(u *localUI) { u.commandMenu = composerCommandMenu{} })
		},
		Notice:       func(text string) { local.update(func(u *localUI) { u.composerNotice = text }) },
		OpenGiphy:    func(query string) { giphy.openSearch(target, m.GiphyAPIKey, query) },
		OpenLocation: openComposerLocation,
	}
}

// composerCommandKey gives the open "/" list its keys from the composer's key
// handler and reports whether the key was the list's. Arrow keys move, Escape
// closes, and Tab or Enter write the highlighted command into the draft. Enter
// on a command typed out in full is left alone, so "/location" and Enter sends
// it at once.
func composerCommandKey(e ui.KeyboardEvent, m Model, local localStore, target string) bool {
	state := local.get().commandMenu
	if !state.Open || state.Target != target || shiftHeld(e) || composerIsComposing(e) {
		return false
	}
	key := e.GetKey()
	if key == "Enter" || key == "Tab" {
		if key == "Enter" {
			if value, _, ok := composerSelection(target); ok && strings.EqualFold(strings.TrimSpace(value), "/"+state.Query) {
				if options := defaultComposerCommands().menu(m, state.Query); state.Active >= 0 && state.Active < len(options) && strings.EqualFold(options[state.Active].Name, state.Query) {
					return false
				}
			}
		}
		e.PreventDefault()
		e.StopPropagation()
		commandMenuPick(m, local, state.Active)
		return true
	}
	if commandMenuKey(m, local, key, target) {
		e.PreventDefault()
		e.StopPropagation()
		return true
	}
	return false
}
