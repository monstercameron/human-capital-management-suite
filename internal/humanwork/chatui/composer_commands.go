package chatui

import (
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
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
// What a command is called, what it takes and where it may be used come from
// the registry the server enforces as well (chat.Chatcmd001Defaults); this
// file adds what only the page knows: how to run it and whether this page can.
// Once a command is chosen the list gives way to its usage (/poll and /todo
// draw their preview instead, chatcmd003_preview.go), and a word the registry
// does not hold is answered with the nearest command and a way to send the
// line as text. The reply box of a thread reads its line through the same
// registry (composerCommandsFor).

// composerCommandRuntime is what a command may touch: the model it was typed
// into and a few actions of the composer. Keeping them behind this value lets a
// command be run and checked without a browser.
type composerCommandRuntime struct {
	Model Model
	// Target is the composer the line was typed in: "chat-composer" or
	// "thread-composer". Empty means the conversation's composer.
	Target string
	// ClearDraft empties the composer; a command that consumed its line calls it.
	ClearDraft func()
	// Notice shows one quiet line above the composer; the next keystroke clears it.
	Notice func(string)
	// OpenGiphy opens the GIF picker searching for query ("" is the trending list).
	OpenGiphy func(query string)
	// OpenLocation opens the location sheet.
	OpenLocation func()
	// Unknown shows that name is not a command here and offers suggestion, the
	// nearest command, when there is one. Without it Notice says the same.
	Unknown func(name, suggestion string)
}

// composerCommand is one registered command.
type composerCommand struct {
	// Name is what follows the slash, lower case.
	Name string
	// Example is a typical line, shown as the row's tooltip.
	Example string
	// Description is the copy key of the one-line description the menu shows.
	Description string
	// Text is the key of that description in this feature's own copy table,
	// for commands whose description lives there.
	Text string
	// Summary is the registry's own description, used when no translated one
	// exists (a command a skill or an integration registered).
	Summary string
	// Usage is the form of the command's arguments, shown once it is chosen.
	Usage string
	// Preview says Enter shows what will be posted and waits for Post.
	Preview bool
	// Available says whether the command exists in this conversation; nil means
	// everywhere. A command that is not available is absent from the menu and
	// not run.
	Available func(Model) bool
	// Run does the command with the text after its name.
	Run func(rt composerCommandRuntime, args string)
	// Check, when set, makes the command one that is sent as typed (the server
	// reads it, as it reads "/ask"): it says whether the line may be sent, and
	// has told the person why when it may not. Run is not used then.
	Check func(rt composerCommandRuntime, args string) bool
}

// composerCommandRegistry holds the commands. It is a value built where it is
// used, never shared state.
type composerCommandRegistry struct{ commands []composerCommand }

// composerCommandPlace is where the composer is, as the registry asks it.
func composerCommandPlace(m Model) chat.Chatcmd001Place {
	c := m.selected()
	place := chat.Chatcmd001Place{Agent: c.Agent}
	switch c.Kind {
	case PublicChannel:
		place.Kind = chat.PublicChannel
	case PrivateChannel:
		place.Kind = chat.PrivateChannel
	case GroupChat:
		place.Kind = chat.Group
	default:
		place.Kind = chat.Direct
	}
	if view, ok := m.ChannelStatuses[m.SelectedID]; ok {
		place.Status = view.Status.Status
	}
	return place
}

// defaultComposerCommands is the registry of the commands this page can run:
// every command of the shared registry that the page has a way to carry out,
// in the registry's order.
func defaultComposerCommands() composerCommandRegistry {
	page := map[string]composerCommand{
		"poll":     {Text: "cmd-poll", Available: chatcmd003Available, Run: chatcmd003RegistryRun},
		"todo":     {Text: "cmd-todo", Available: chatcmd003Available, Run: chatcmd003RegistryRun},
		"giphy":    {Description: keyComposerCommandGiphy, Run: runGiphyCommand},
		"location": {Description: keyComposerCommandPlace, Available: composerLocationAvailable, Run: runLocationCommand},
		"ask":      {Text: "cmd-ask", Available: agent027AskAvailable, Check: agent027AskCheck},
	}
	var commands []composerCommand
	for _, shared := range chat.Chatcmd001WithAsk(chat.Chatcmd001Defaults()).Commands() {
		command, ok := page[shared.Name]
		if !ok {
			continue
		}
		shared, here := shared, command.Available
		command.Name, command.Example, command.Usage, command.Summary, command.Preview = shared.Name, shared.Example, shared.Usage, shared.Description, shared.Preview
		// Available here means the registry allows it in this conversation
		// and this page can run it.
		command.Available = func(m Model) bool {
			return shared.AllowedIn(composerCommandPlace(m)) && (here == nil || here(m))
		}
		commands = append(commands, command)
	}
	return composerCommandRegistry{commands: commands}
}

// composerCommandsFor is the registry of one composer. A reply box has no
// location control of its own, so /location belongs to the conversation's
// composer only; every other command works in both.
func composerCommandsFor(target string) composerCommandRegistry {
	registry := defaultComposerCommands()
	if target != "thread-composer" {
		return registry
	}
	var kept []composerCommand
	for _, command := range registry.commands {
		if command.Name != "location" {
			kept = append(kept, command)
		}
	}
	return composerCommandRegistry{commands: kept}
}

// composerCommandDescription is the one line the menu shows for a command.
func composerCommandDescription(m Model, command composerCommand) string {
	if command.Text != "" {
		if text := chatcmd003Text(m, command.Text); text != "" {
			return text
		}
	}
	if command.Description != "" {
		if text := composerText(m, command.Description); text != "" {
			return text
		}
	}
	return command.Summary
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
	if command.Check != nil {
		if command.Check(rt, args) {
			return composerCommandText, body
		}
		return composerCommandRan, body
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
	// Chosen is the command the draft starts with once its name is complete
	// ("/poll " and on): the list is closed and its usage is shown instead.
	// Invalid marks arguments the shared grammar cannot read.
	Chosen  string
	Invalid bool
	// Unknown is a word that was sent as a command and is not one here, and
	// Suggest the nearest command, if any.
	Unknown, Suggest string
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
	if !ok || len(composerCommandsFor(target).menu(m, query)) == 0 {
		return composerCommandChosen(m, target, value)
	}
	next := composerCommandMenu{Target: target, Query: query, Open: true}
	if current.Open && current.Target == target && current.Query == query {
		next.Active = current.Active
	}
	return next
}

// composerCommandChosen is the list's state once the draft holds a complete
// command name followed by a space: closed, naming the command whose usage is
// shown, and saying whether what follows it can be read.
func composerCommandChosen(m Model, target, value string) composerCommandMenu {
	line := chat.Chatcmd001ParseLine(value)
	if !line.Command || !strings.ContainsAny(strings.TrimLeft(value, " \t\n"), " \t\n") {
		return composerCommandMenu{}
	}
	command, ok := composerCommandsFor(target).lookup(m, line.Name)
	if !ok {
		return composerCommandMenu{}
	}
	return composerCommandMenu{Target: target, Chosen: command.Name, Invalid: line.Unparsed && command.Preview && command.Name != "giphy"}
}

// composerCommandShown is the names of the commands the list of target shows
// in state, in the order it shows them; none when the list is closed.
func composerCommandShown(m Model, state composerCommandMenu, target string) []string {
	if !state.Open || state.Target != target {
		return nil
	}
	var names []string
	for _, command := range composerCommandsFor(target).menu(m, state.Query) {
		names = append(names, command.Name)
	}
	return names
}

// commandMenuSet makes next the list's state. The list is drawn in the page
// before the state is kept: the page finding of 2026-10-02 was a list that
// appeared a second after the "/" in a conversation that was still loading,
// because it waited for the next render. The render that follows draws the
// same list from the state.
func commandMenuSet(m Model, local localStore, target string, next composerCommandMenu) {
	commandMenuShow(target, composerCommandShown(m, next, target), next.Active)
	if next != local.get().commandMenu {
		local.update(func(u *localUI) { u.commandMenu = next })
	}
}

// commandMenuTrack follows the composer: it opens, filters or closes the list as
// the draft changes, and keeps the poll or to-do preview drawn from the draft
// (chatcmd003_preview.go).
func commandMenuTrack(m Model, local localStore, target string) {
	next := composerCommandMenu{}
	if value, caret, ok := composerSelection(target); ok {
		next = composerCommandMenuNext(m, local.get().commandMenu, target, value, caret)
	}
	commandMenuSet(m, local, target, next)
	chatcmd003Track(m, local, target)
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
	next, handled := composerCommandMenuMove(state, key, len(composerCommandsFor(target).menu(m, state.Query)))
	if handled {
		commandMenuSet(m, local, target, next)
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
	commandMenuShow(state.Target, nil, 0)
	local.update(func(u *localUI) { u.commandMenu = composerCommandMenu{} })
	if !state.Open {
		return
	}
	options := composerCommandsFor(state.Target).menu(m, state.Query)
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
// place and style; the field keeps focus and owns the keys. Under it stands the
// line that takes the list's place while it is closed.
//
// The list is always in the page, with every command this composer can run:
// which rows show, their order and the highlighted one are attributes, so the
// keystroke that opens or narrows the list can set them at once
// (commandMenuShow) and the render that follows only confirms them. seq is the
// count of local state changes; it is written into data-open so that every
// change of that state writes the attribute again, whatever a handler set on
// the element in between.
func composerCommandMenuView(m Model, state composerCommandMenu, target string, seq uint64) ui.Node {
	shown := composerCommandShown(m, state, target)
	active := state.Active
	if active < 0 || active >= len(shown) {
		active = 0
	}
	label := composerText(m, keyComposerCommands)
	rows := []ui.Node{html.P(html.Props{Class: "mention-heading", Aria: map[string]string{"hidden": "true"}, Text: label})}
	for _, command := range composerCommandsFor(target).menu(m, "") {
		order := -1
		for i, name := range shown {
			if name == command.Name {
				order = i
			}
		}
		// The row's place among the shown rows is data-order, which the
		// stylesheet turns into its order: the page takes no inline styles. A
		// row is an option of the list, not a button of the composer: the
		// field keeps focus and the rows are never tab stops.
		props := html.Props{Class: "mention-option command-option", Role: "option", TabIndex: -1, Title: s24CommandExample(m, command), Hidden: order < 0,
			Data: map[string]string{"action": "command-pick", "extra": itoa(order + 1), "command": command.Name, "order": itoa(max(order, 0))},
			Aria: map[string]string{"selected": boolString(order >= 0 && order == active)}}
		if order >= 0 {
			props.ID = target + "-command-" + itoa(order+1)
			if order == active {
				props.Class += " active"
			}
		}
		cells := []ui.Node{
			html.Span(html.Props{Class: "mention-name", Dir: "ltr", Text: "/" + command.Name}),
			html.Span(html.Props{Class: "mention-detail", Dir: "auto", Text: composerCommandDescription(m, command)})}
		// CHATBUG-028: what the command takes stands on its row.
		if args := chatbug059CommandArguments(m, command); args != "" {
			cells = append(cells, html.Span(html.Props{Class: "command-args", Dir: "ltr", Text: args}))
		}
		rows = append(rows, html.WithKey(html.Div(props, cells...), "command:"+command.Name))
	}
	// Closed, the list is out of the page's layout and is no listbox: a reader
	// of the page's roles finds one only while there is one to use.
	role := ""
	if len(shown) > 0 {
		role = "listbox"
	}
	list := html.Div(html.Props{ID: target + "-commands", Class: "command-menu", Role: role, Dir: agentReplyDirection(m.Locale),
		Data: map[string]string{"open": boolString(len(shown) > 0) + ":" + itoa(int(seq))}, Aria: map[string]string{"label": label}},
		append(rows, composerCommandKeys(m))...)
	line := html.Div(html.Props{Class: "command-line-none"})
	if state.Target == target && !state.Open {
		line = composerCommandLine(m, state, target)
	}
	return html.Div(html.Props{Class: "command-menu-slot"}, list, line)
}

// composerCommandKeys is the list's hint line. It names the keys that do
// something in this list and no others (CHATBUG-056: the people list's "Tab
// details" was printed here, and Tab shows no details for a command).
func composerCommandKeys(m Model) ui.Node {
	var keys []ui.Node
	for _, key := range strings.Split(chatcmd003Text(m, "cmd-keys"), " · ") {
		keys = append(keys, html.Span(html.Props{Text: key}))
	}
	return html.P(html.Props{Class: "mention-hint kbd-hint", Aria: map[string]string{"hidden": "true"}}, keys...)
}

// composerCommandLine is what stands in the list's place while it is closed:
// the usage of the command the draft starts with, or the answer to a word that
// is not a command here.
func composerCommandLine(m Model, state composerCommandMenu, target string) ui.Node {
	if state.Unknown != "" {
		text := chatcmd002Fill(chatcmd003Text(m, "cmd-unknown"), "name", state.Unknown)
		actions := []ui.Node{}
		if state.Suggest != "" {
			text += " " + chatcmd002Fill(chatcmd003Text(m, "cmd-suggest"), "name", state.Suggest)
			actions = append(actions, html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": "command-use", "id": target, "extra": state.Suggest}, Text: chatcmd002Fill(chatcmd003Text(m, "cmd-use"), "name", state.Suggest)}))
		}
		actions = append(actions, html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": "command-as-text", "id": target}, Text: chatcmd003Text(m, "cmd-as-text")}))
		return html.Div(html.Props{Class: "command-line command-unknown", Role: "status", Dir: agentReplyDirection(m.Locale), Aria: map[string]string{"live": "polite"}},
			html.Span(html.Props{Class: "command-line-text", Dir: "auto", Text: text}), html.Span(html.Props{Class: "command-line-actions"}, actions...))
	}
	command, ok := composerCommandsFor(target).lookup(m, state.Chosen)
	// /poll and /todo draw their preview above the composer from the first
	// character after the command; the preview says how to write one and what
	// it cannot read, so no second line stands under it.
	if state.Chosen == "" || !ok || command.Name == "poll" || command.Name == "todo" {
		return html.Div(html.Props{Class: "command-line-none"})
	}
	parts := []ui.Node{
		html.Span(html.Props{Class: "command-line-label", Text: chatcmd003Text(m, "cmd-usage")}),
		html.Tag("code", html.Props{Class: "command-line-usage", Dir: "ltr", Text: command.Usage}),
	}
	if state.Invalid {
		parts = append(parts, html.Span(html.Props{Class: "command-line-invalid", Role: "alert", Text: chatcmd003Text(m, "cmd-invalid")}))
	}
	return html.Div(html.Props{ID: target + "-command-usage", Class: "command-line command-usage", Dir: agentReplyDirection(m.Locale), Title: chatcmd003Text(m, "cmd-example") + ": " + s24CommandExample(m, command)}, parts...)
}

// composerCommandAction handles the buttons of the unknown-command line: take
// the suggested command, or send the line as the text it is. It reports
// whether the action was one of them.
func composerCommandAction(local localStore, action, target, name string) bool {
	if action != "command-use" && action != "command-as-text" {
		return false
	}
	value, _, ok := composerSelection(target)
	local.update(func(u *localUI) { u.commandMenu = composerCommandMenu{} })
	if !ok {
		return true
	}
	if action == "command-use" {
		updated, caret := composerCommandReplace(value, name)
		replaceComposerText(target, updated, caret)
		return true
	}
	// A second slash makes the line text; then it is sent the way Send sends.
	replaceComposerText(target, "/"+strings.TrimLeft(value, " \t\n"), len(utf16.Encode([]rune(value)))+1)
	commandMenuSubmit(target)
	return true
}

// composerCommandReplace swaps the command word of a draft for name and
// returns the caret after the name and its space.
func composerCommandReplace(value, name string) (string, int) {
	trimmed := strings.TrimLeft(value, " \t\n")
	rest := ""
	if end := strings.IndexFunc(trimmed, unicode.IsSpace); end >= 0 {
		rest = strings.TrimLeft(trimmed[end:], " \t\n")
	}
	head := "/" + name + " "
	return head + rest, len(utf16.Encode([]rune(head)))
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
	outcome, text := composerCommandsFor(rt.Target).dispatch(rt, body)
	switch outcome {
	case composerCommandRan:
		return "", true
	case composerCommandUnknown:
		name, _, _ := parseComposerCommand(body)
		if rt.Unknown != nil {
			rt.Unknown(name, composerCommandClosest(rt.Model, rt.Target, name))
			return "", true
		}
		rt.Notice(composerUnknownNotice(rt.Model, name))
		return "", true
	}
	return text, false
}

// composerCommandClosest is the nearest command the composer named by target
// offers here to a mistyped name, or "".
func composerCommandClosest(m Model, target, typed string) string {
	closest := chat.Chatcmd001WithAsk(chat.Chatcmd001Defaults()).Closest(composerCommandPlace(m), typed)
	if _, ok := composerCommandsFor(target).lookup(m, closest); !ok {
		return ""
	}
	return closest
}

// newComposerCommandRuntime is what a command run from a composer may touch:
// it empties that composer and its draft, opens the GIF picker and the
// location sheet, and shows its one quiet line above the composer. target is
// the conversation's composer or the reply box of the open thread.
func newComposerCommandRuntime(m Model, local localStore, giphy *giphyPickerViews, drafts *browserDrafts, target string) composerCommandRuntime {
	return composerCommandRuntime{
		Model:  m,
		Target: target,
		ClearDraft: func() {
			setDOMValue(target, "")
			if target == "thread-composer" {
				if m.Callbacks.ThreadDraftChanged != nil {
					m.Callbacks.ThreadDraftChanged(m.ThreadParentID, "")
				}
			} else {
				drafts.set(m.SelectedID, "")
				if m.Callbacks.DraftChanged != nil {
					m.Callbacks.DraftChanged(m.SelectedID, "")
				}
			}
			commandMenuShow(target, nil, 0)
			local.update(func(u *localUI) { u.commandMenu = composerCommandMenu{} })
		},
		Notice: func(text string) { local.update(func(u *localUI) { u.composerNotice = text }) },
		Unknown: func(name, suggestion string) {
			local.update(func(u *localUI) {
				u.commandMenu = composerCommandMenu{Target: target, Unknown: name, Suggest: suggestion}
			})
		},
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
				if options := composerCommandsFor(target).menu(m, state.Query); state.Active >= 0 && state.Active < len(options) && strings.EqualFold(options[state.Active].Name, state.Query) {
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
