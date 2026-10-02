package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AmbientRead is one agent that has been given "Reads every message here" in
// the open conversation by its administrator (AGENTUX-066). Agent is the
// agent's identifier; Paused is set while its conversation or daily budget is
// spent. The server sends it to every member of the conversation, so everyone
// can see who reads what they write.
type AmbientRead struct {
	Agent  string
	Paused bool
}

const (
	keyReadsHere    = "agentux066.reads_here"
	keyReadsHere1   = "agentux066.reads_here_one"
	keyReadsOptOut  = "agentux066.opt_out"
	keyReadsOptOutH = "agentux066.opt_out_help"
	keyReadsPaused  = "agentux066.paused"
	keyReadsAnd     = "agentux066.and"
	keyReadsHeading = "agentux066.heading"
)

var agentux066Copy = map[string]map[string]string{
	"en-US": {
		keyReadsHere: "{names} read messages here", keyReadsHere1: "{names} reads messages here",
		keyReadsOptOut: "Don't act on my messages", keyReadsOptOutH: "These agents will not read or act on anything you write in this conversation.",
		keyReadsPaused: "{name}: paused, daily limit reached", keyReadsAnd: " and ", keyReadsHeading: "Agents that read messages here",
	},
	"de-DE": {
		keyReadsHere: "{names} lesen hier Nachrichten", keyReadsHere1: "{names} liest hier Nachrichten",
		keyReadsOptOut: "Nicht auf meine Nachrichten reagieren", keyReadsOptOutH: "Diese Agenten lesen nichts von dem, was Sie in dieser Unterhaltung schreiben, und handeln nicht danach.",
		keyReadsPaused: "{name}: pausiert, Tageslimit erreicht", keyReadsAnd: " und ", keyReadsHeading: "Agenten, die hier Nachrichten lesen",
	},
	"ar": {
		keyReadsHere: "{names} يقرؤون الرسائل هنا", keyReadsHere1: "{names} يقرأ الرسائل هنا",
		keyReadsOptOut: "لا تتصرفوا بناءً على رسائلي", keyReadsOptOutH: "لن تقرأ هذه الوكلاء ما تكتبه في هذه المحادثة ولن تتصرف بناءً عليه.",
		keyReadsPaused: "{name}: متوقف مؤقتًا، تم بلوغ الحد اليومي", keyReadsAnd: " و", keyReadsHeading: "وكلاء يقرؤون الرسائل هنا",
	},
}

// ambientAgentName names an agent that reads messages here: its display name in
// the room's directory, else its identifier written as words.
func ambientAgentName(m Model, id string) string {
	for _, persona := range m.ResolvedPersonaMentions {
		if persona.Reference.ID == id && strings.TrimSpace(persona.Reference.Display) != "" {
			return persona.Reference.Display
		}
	}
	words := strings.FieldsFunc(id, func(r rune) bool { return r == '-' || r == '_' || r == '.' || r == ' ' })
	for i, word := range words {
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

// ambientReadNames lists the names of the agents that read here, in the order
// the server sent them, and never in a direct conversation between two people.
func ambientReadNames(m Model) []string {
	if m.selected().Kind == DirectMessage && !m.selected().Agent {
		return nil
	}
	var names []string
	for _, read := range m.AmbientReads {
		if strings.TrimSpace(read.Agent) != "" {
			names = append(names, ambientAgentName(m, read.Agent))
		}
	}
	return names
}

func ambientJoinNames(m Model, names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	and := laneText(m, agentux066Copy, keyReadsAnd)
	return strings.Join(names[:len(names)-1], ", ") + and + names[len(names)-1]
}

// ambientReadsLine is the sentence the header and the details say to every
// member: "Task Catcher and Reminder read messages here".
func ambientReadsLine(m Model) string {
	names := ambientReadNames(m)
	if len(names) == 0 {
		return ""
	}
	key := keyReadsHere
	if len(names) == 1 {
		key = keyReadsHere1
	}
	return laneTextf(m, agentux066Copy, key, map[string]string{"names": ambientJoinNames(m, names)})
}

// ambientReadsHeader is the header's part of the line: shown beside the member
// and agent counts, clipped by the line like the rest of it.
func ambientReadsHeader(m Model) ui.Node {
	line := ambientReadsLine(m)
	if line == "" {
		return nil
	}
	return html.Span(html.Props{Class: "topic-reads", Dir: "auto"}, html.Span(html.Props{Class: "topic-sep", Text: " · "}), ui.Text(line))
}

// ambientReadsSection is the details panel's part: the sentence, a line for each
// agent that is paused, and the member's own switch "Don't act on my messages",
// which the server enforces before any read.
func ambientReadsSection(m Model) ui.Node {
	line := ambientReadsLine(m)
	if line == "" {
		return nil
	}
	children := []ui.Node{html.P(html.Props{Class: "ambient-reads-line", Dir: "auto", Text: line})}
	for _, read := range m.AmbientReads {
		if read.Paused {
			children = append(children, html.P(html.Props{Class: "ambient-reads-paused", Role: "status", Dir: "auto", Text: laneTextf(m, agentux066Copy, keyReadsPaused, map[string]string{"name": ambientAgentName(m, read.Agent)})}))
		}
	}
	children = append(children,
		html.Button(html.Props{Class: "button secondary small ambient-reads-optout", Type: "button", Role: "switch", Disabled: m.Callbacks.SetAmbientOptOut == nil,
			Data: map[string]string{"action": "ambient-optout"},
			Aria: map[string]string{"checked": boolString(m.AmbientOptOut), "describedby": "ambient-reads-optout-help"}, Text: laneText(m, agentux066Copy, keyReadsOptOut)}),
		html.P(html.Props{ID: "ambient-reads-optout-help", Class: "ambient-reads-help muted", Dir: "auto", Text: laneText(m, agentux066Copy, keyReadsOptOutH)}))
	return html.Div(html.Props{Class: "ambient-reads", Aria: map[string]string{"label": laneText(m, agentux066Copy, keyReadsHeading)}}, children...)
}

// ambientReadsClick handles the member's switch. It reports whether the click
// was the switch's.
func ambientReadsClick(m Model, action string) bool {
	if action != "ambient-optout" {
		return false
	}
	if m.Callbacks.SetAmbientOptOut != nil {
		m.Callbacks.SetAmbientOptOut(!m.AmbientOptOut)
	}
	return true
}
