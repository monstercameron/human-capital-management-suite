package chat

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

// The composer's slash commands are entries in one registry that the page and
// the server both read (CHATCMD-001): what a command is called, what it takes,
// where it may be used and whether it previews before it posts. The page draws
// its "/" menu from it and the server refuses what it does not allow, so the
// two cannot drift apart. A registry is a value built where it is used; it is
// never shared mutable state.

// Chatcmd001Version is the version of the registration interface. A skill or
// an integration registers against a version it knows; an entry that names a
// later one is refused rather than half understood.
const Chatcmd001Version = 1

// Chatcmd001Argument names one kind of argument a command reads.
type Chatcmd001Argument string

const (
	Chatcmd001Text     Chatcmd001Argument = "text"     // free words, or one "quoted phrase"
	Chatcmd001Numbered Chatcmd001Argument = "numbered" // 1="first" 2="second"
	Chatcmd001Named    Chatcmd001Argument = "named"    // multiple=yes
	Chatcmd001Mention  Chatcmd001Argument = "mention"  // @Name
	Chatcmd001Date     Chatcmd001Argument = "date"     // by Friday, closes=2026-10-05
)

// Chatcmd001Command is one registered command.
type Chatcmd001Command struct {
	// Name is what follows the slash: lower-case ASCII letters, digits, "_" and
	// "-", starting with a letter.
	Name string
	// Description is one line saying what the command does.
	Description string
	// Usage is the form of its arguments, shown once the command is chosen.
	Usage string
	// Example is one typical line.
	Example string
	// Arguments are the kinds of argument it reads, in the order they are typed.
	Arguments []Chatcmd001Argument
	// Named lists the named options it accepts.
	Named []string
	// Preview says the command shows what it will post and waits for Post.
	Preview bool
	// Kinds limits the conversations it works in; empty means every kind.
	Kinds []ConversationKind
	// Statuses limits the channel statuses it works in; empty means only an
	// open conversation, since a command posts or changes something.
	Statuses []chatpolicy.ChannelStatus
	// Agents says it also works in a conversation with an agent.
	Agents bool
	// Owner names who registered it: "" for the product's own commands,
	// otherwise the skill or integration.
	Owner string
}

// Chatcmd001Place is where a command is being used.
type Chatcmd001Place struct {
	Kind   ConversationKind
	Status chatpolicy.ChannelStatus
	Agent  bool
}

// AllowedIn reports whether the command may be used in place.
func (c Chatcmd001Command) AllowedIn(place Chatcmd001Place) bool {
	if place.Agent && !c.Agents {
		return false
	}
	if len(c.Kinds) > 0 {
		found := false
		for _, kind := range c.Kinds {
			found = found || kind == place.Kind
		}
		if !found {
			return false
		}
	}
	status := place.Status
	if status == "" {
		status = chatpolicy.StatusOpen
	}
	if len(c.Statuses) == 0 {
		return status == chatpolicy.StatusOpen
	}
	for _, allowed := range c.Statuses {
		if allowed == status {
			return true
		}
	}
	return false
}

// Chatcmd001Registry is an ordered set of commands with distinct names.
type Chatcmd001Registry struct{ commands []Chatcmd001Command }

// Chatcmd001Defaults is the registry of the product's own commands.
func Chatcmd001Defaults() Chatcmd001Registry {
	return Chatcmd001Registry{commands: []Chatcmd001Command{
		{Name: "poll", Description: "Ask a question and let people vote", Usage: `/poll question? option, option or option`, Example: `/poll "Where for the offsite?" 1="Lisbon" 2="Porto" 3="Remote"`,
			Arguments: []Chatcmd001Argument{Chatcmd001Text, Chatcmd001Numbered, Chatcmd001Named, Chatcmd001Date}, Named: []string{"multiple", "anonymous", "results", "closes", "add"}, Preview: true},
		{Name: "todo", Description: "Post a to-do list people can tick off", Usage: `/todo task, task @Name by Friday, task`, Example: `/todo "Launch checklist" 1="Book the room" 2="Send invites @Dana by Friday"`,
			Arguments: []Chatcmd001Argument{Chatcmd001Text, Chatcmd001Numbered, Chatcmd001Mention, Chatcmd001Date, Chatcmd001Named}, Named: []string{"tick"}, Preview: true},
		{Name: "giphy", Description: "Find a GIF to post", Usage: `/giphy what to search for`, Example: "/giphy cats", Arguments: []Chatcmd001Argument{Chatcmd001Text}, Preview: true, Agents: true},
		{Name: "location", Description: "Share a place or where you are", Usage: `/location`, Example: "/location", Preview: true, Agents: true},
	}}
}

// Chatcmd001AskName is the command that asks an agent (AGENT-027).
const Chatcmd001AskName = "ask"

// Chatcmd001AskOwner is who registers the ask command: the agent platform,
// not Chat. Chat's own list stays as Chatcmd001Defaults returns it.
const Chatcmd001AskOwner = "agents"

// Chatcmd001WithAsk returns the registry with "/ask" added through the
// versioned registration interface. "/ask @Agent question" sends the same
// message a typed mention sends, so the agent run it starts is admitted,
// limited and answered exactly as a mention is; in a direct conversation with
// an agent the name may be left out. It posts at once, with no preview, and
// works in a conversation with an agent. A registry that already holds the
// command is returned as it is.
func Chatcmd001WithAsk(registry Chatcmd001Registry) Chatcmd001Registry {
	if _, ok := registry.Lookup(Chatcmd001AskName); ok {
		return registry
	}
	added, err := registry.Register(Chatcmd001Version, Chatcmd001Command{
		Name: Chatcmd001AskName, Description: "Ask an agent in this conversation",
		Usage: `/ask @Agent your question`, Example: `/ask @Assistant How many vacation days carry over?`,
		Arguments: []Chatcmd001Argument{Chatcmd001Mention, Chatcmd001Text}, Agents: true, Owner: Chatcmd001AskOwner,
	})
	if err != nil {
		return registry
	}
	return added
}

// Chatcmd001ValidName reports whether a word can be a command name. A path
// such as /usr/bin is therefore not a command.
func Chatcmd001ValidName(name string) bool {
	if name == "" || len(name) > 32 {
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

// Register adds a command through the versioned interface and returns the new
// registry; the receiver is not changed. A name already taken is refused, so
// an integration cannot replace one of the product's commands.
func (r Chatcmd001Registry) Register(version int, command Chatcmd001Command) (Chatcmd001Registry, error) {
	if version != Chatcmd001Version {
		return r, fmt.Errorf("%w: command registration version %d", ErrInvalidArgument, version)
	}
	command.Name = strings.ToLower(command.Name)
	if !Chatcmd001ValidName(command.Name) || strings.TrimSpace(command.Description) == "" || strings.ContainsAny(command.Description+command.Usage+command.Example, "\r\n") || len(command.Description) > 120 || len(command.Usage) > 200 || len(command.Example) > 200 {
		return r, ErrInvalidArgument
	}
	if _, taken := r.Lookup(command.Name); taken {
		return r, ErrConflict
	}
	next := Chatcmd001Registry{commands: append(append([]Chatcmd001Command(nil), r.commands...), command)}
	return next, nil
}

// Commands lists every registered command, in order.
func (r Chatcmd001Registry) Commands() []Chatcmd001Command {
	return append([]Chatcmd001Command(nil), r.commands...)
}

// Lookup finds a command by name, whatever its case.
func (r Chatcmd001Registry) Lookup(name string) (Chatcmd001Command, bool) {
	for _, command := range r.commands {
		if strings.EqualFold(command.Name, name) {
			return command, true
		}
	}
	return Chatcmd001Command{}, false
}

// Available lists the commands that may be used in place, in registry order.
func (r Chatcmd001Registry) Available(place Chatcmd001Place) []Chatcmd001Command {
	var out []Chatcmd001Command
	for _, command := range r.commands {
		if command.AllowedIn(place) {
			out = append(out, command)
		}
	}
	return out
}

// Chatcmd001Line is a composer line as the registry reads it.
type Chatcmd001Line struct {
	// Command is false for ordinary text; Text is then what to post. A line that
	// starts with "//" is text that starts with one slash.
	Command bool
	Text    string
	// Name and Raw are the command word and everything after it.
	Name, Raw string
	// Arguments is Raw parsed by the shared grammar; Unparsed is true when it
	// could not be (an unclosed quote, a repeated number).
	Arguments Chatcmd003Arguments
	Unparsed  bool
}

// Chatcmd001ParseLine reads one composer line. The page and the server call
// this same function, so they cannot disagree about what was typed.
func Chatcmd001ParseLine(line string) Chatcmd001Line {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "//") {
		return Chatcmd001Line{Text: trimmed[1:]}
	}
	if len(trimmed) < 2 || trimmed[0] != '/' {
		return Chatcmd001Line{Text: line}
	}
	word, rest := trimmed[1:], ""
	if end := strings.IndexFunc(trimmed, unicode.IsSpace); end >= 0 {
		word, rest = trimmed[1:end], strings.TrimSpace(trimmed[end:])
	}
	if !Chatcmd001ValidName(word) {
		return Chatcmd001Line{Text: line}
	}
	out := Chatcmd001Line{Command: true, Name: strings.ToLower(word), Raw: rest}
	arguments, err := Chatcmd003ParseArguments(rest)
	out.Arguments, out.Unparsed = arguments, err != nil
	return out
}

// Chatcmd001RenderLine writes a command and its arguments back as a line that
// parses to the same arguments.
func Chatcmd001RenderLine(name string, arguments Chatcmd003Arguments) string {
	if rendered := Chatcmd003RenderArguments(arguments); rendered != "" {
		return "/" + name + " " + rendered
	}
	return "/" + name
}

// Check is the server's answer to a command typed in place: the command does
// not exist (ErrNotFound), is not allowed here (ErrPermissionDenied), or its
// arguments do not parse (ErrInvalidArgument). The arguments are untrusted
// text and are only ever read as data.
func (r Chatcmd001Registry) Check(place Chatcmd001Place, line Chatcmd001Line) error {
	if !line.Command {
		return ErrInvalidArgument
	}
	command, ok := r.Lookup(line.Name)
	if !ok {
		return ErrNotFound
	}
	if !command.AllowedIn(place) {
		return ErrPermissionDenied
	}
	if line.Unparsed {
		return ErrInvalidArgument
	}
	return nil
}

// Closest is the registered name nearest to a mistyped one among the commands
// allowed in place, or "" when nothing is near enough to suggest.
func (r Chatcmd001Registry) Closest(place Chatcmd001Place, typed string) string {
	typed = strings.ToLower(typed)
	best, bestDistance := "", 3
	for _, command := range r.Available(place) {
		distance := chatcmd001Distance(typed, command.Name)
		switch {
		case strings.HasPrefix(command.Name, typed) || strings.HasPrefix(typed, command.Name):
			distance = min(distance, 1)
		case len(typed) >= 2 && strings.HasPrefix(command.Name, typed[:2]):
			// The same first two letters count for one slip: /gif is /giphy.
			distance--
		}
		if distance < bestDistance {
			best, bestDistance = command.Name, distance
		}
	}
	return best
}

// chatcmd001Distance is the edit distance between two short ASCII words.
func chatcmd001Distance(a, b string) int {
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current := make([]int, len(b)+1)
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous = current
	}
	return previous[len(b)]
}
