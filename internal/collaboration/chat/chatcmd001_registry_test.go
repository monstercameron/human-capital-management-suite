package chat

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

func chatcmd001Names(commands []Chatcmd001Command) string {
	var names []string
	for _, command := range commands {
		names = append(names, command.Name)
	}
	return strings.Join(names, ",")
}

func TestTodo_CHATCMD_001(t *testing.T) {
	registry := Chatcmd001Defaults()
	channel := Chatcmd001Place{Kind: PublicChannel, Status: chatpolicy.StatusOpen}
	if got := chatcmd001Names(registry.Available(channel)); got != "poll,todo,giphy,location" {
		t.Fatalf("commands in a channel: %s", got)
	}
	// Each one declares what the menu and the usage hint need.
	for _, command := range registry.Available(channel) {
		if command.Description == "" || command.Usage == "" || command.Example == "" || !strings.HasPrefix(command.Usage, "/"+command.Name) || !strings.HasPrefix(command.Example, "/"+command.Name) {
			t.Errorf("/%s is missing its description, usage or example: %+v", command.Name, command)
		}
		if example := Chatcmd001ParseLine(command.Example); !example.Command || example.Name != command.Name || example.Unparsed {
			t.Errorf("/%s: its own example does not parse: %+v", command.Name, example)
		}
	}
	// Where: a card command is not offered to an agent, and nothing that posts
	// is offered in a channel that takes no posts.
	if got := chatcmd001Names(registry.Available(Chatcmd001Place{Kind: Direct, Agent: true})); got != "giphy,location" {
		t.Fatalf("commands with an agent: %s", got)
	}
	for _, status := range []chatpolicy.ChannelStatus{chatpolicy.StatusArchived, chatpolicy.StatusLocked, chatpolicy.StatusAnnouncements} {
		if got := registry.Available(Chatcmd001Place{Kind: PublicChannel, Status: status}); len(got) != 0 {
			t.Fatalf("commands in a %s channel: %s", status, chatcmd001Names(got))
		}
	}
	// The line grammar: a command, its arguments, a literal slash, plain text.
	line := Chatcmd001ParseLine(`  /Poll "Where = next?" 2="Porto" 1="A ""quoted"" place" multiple=yes  `)
	if !line.Command || line.Name != "poll" || line.Unparsed || line.Arguments.Text != "Where = next?" || !reflect.DeepEqual(line.Arguments.Items, []string{`A "quoted" place`, "Porto"}) || line.Arguments.Named["multiple"] != "yes" {
		t.Fatalf("command line %+v", line)
	}
	if literal := Chatcmd001ParseLine("//shrug that is fine"); literal.Command || literal.Text != "/shrug that is fine" {
		t.Fatalf("a line starting with // is %+v", literal)
	}
	for _, text := range []string{"hello /poll", "/usr/bin/env", "/", "/ poll", "/9lives", ""} {
		if got := Chatcmd001ParseLine(text); got.Command || got.Text != text {
			t.Errorf("%q read as a command: %+v", text, got)
		}
	}
	if broken := Chatcmd001ParseLine(`/poll "unclosed 1="a"`); !broken.Command || !broken.Unparsed {
		t.Fatalf("an unclosed quote is not marked: %+v", broken)
	}
	// An unknown command is not posted: the nearest name is offered.
	for typed, want := range map[string]string{"pol": "poll", "polll": "poll", "tod": "todo", "gif": "giphy", "loc": "location", "todos": "todo", "xyzzy": ""} {
		if got := registry.Closest(channel, typed); got != want {
			t.Errorf("closest to /%s = %q, want %q", typed, got, want)
		}
	}
	if got := registry.Closest(Chatcmd001Place{Kind: Direct, Agent: true}, "pol"); got != "" {
		t.Fatalf("a command not allowed here was suggested: %q", got)
	}
	// The versioned registration interface: a skill adds a command.
	added, err := registry.Register(Chatcmd001Version, Chatcmd001Command{Name: "Standup", Description: "Collect today's updates", Usage: "/standup", Example: "/standup", Preview: true, Owner: "skill:standup", Kinds: []ConversationKind{PublicChannel, PrivateChannel}})
	if err != nil {
		t.Fatal(err)
	}
	if got := chatcmd001Names(added.Available(channel)); got != "poll,todo,giphy,location,standup" || chatcmd001Names(registry.Available(channel)) != "poll,todo,giphy,location" {
		t.Fatalf("after registration: %s (the registry it was added to must not change)", got)
	}
	if got := chatcmd001Names(added.Available(Chatcmd001Place{Kind: Direct})); strings.Contains(got, "standup") {
		t.Fatalf("a channel-only command is offered in a direct message: %s", got)
	}
}

func TestTodo_CHATCMD_001_Property(t *testing.T) {
	// Parsing, rendering and parsing again returns the same arguments, for
	// every command and for text that holds spaces, quotes and equals signs.
	values := []string{"a", "with spaces", "x=y", `say "hello"`, "🥳", "اختيار", "Deutsch ß", "@Dana by Friday", ""}
	for _, command := range Chatcmd001Defaults().Available(Chatcmd001Place{Kind: Group}) {
		for _, text := range values {
			for _, item := range values {
				arguments := Chatcmd003Arguments{Text: text, Items: []string{item, text}, Named: map[string]string{"closes": "in an hour"}, Explicit: true}
				first := Chatcmd001ParseLine(Chatcmd001RenderLine(command.Name, arguments))
				second := Chatcmd001ParseLine(Chatcmd001RenderLine(first.Name, first.Arguments))
				if !first.Command || first.Unparsed || first.Name != command.Name || !reflect.DeepEqual(first.Arguments, arguments) || !reflect.DeepEqual(second, first) {
					t.Fatalf("/%s %q %q: %+v then %+v", command.Name, text, item, first, second)
				}
			}
		}
	}
	// The same line read twice, as the page and then the server would, agrees.
	for _, line := range []string{"/poll a, b or c", `/todo "x" 1="y"`, "//literal", "plain", "/unknown thing", `/poll "broken`, "/giphy   cats  "} {
		if page, server := Chatcmd001ParseLine(line), Chatcmd001ParseLine(line); !reflect.DeepEqual(page, server) {
			t.Fatalf("%q: page %+v server %+v", line, page, server)
		}
	}
}

func TestTodo_CHATCMD_001_Security(t *testing.T) {
	registry := Chatcmd001Defaults()
	open := Chatcmd001Place{Kind: PrivateChannel}
	cases := map[string]struct {
		place Chatcmd001Place
		line  string
		want  error
	}{
		"a known command":            {open, "/poll a, b", nil},
		"an unknown command":         {open, "/drop tables", ErrNotFound},
		"not allowed with an agent":  {Chatcmd001Place{Kind: Direct, Agent: true}, "/poll a, b", ErrPermissionDenied},
		"not allowed when archived":  {Chatcmd001Place{Kind: PublicChannel, Status: chatpolicy.StatusArchived}, "/todo x", ErrPermissionDenied},
		"arguments that do not read": {open, `/poll "a 1="b"`, ErrInvalidArgument},
		"plain text":                 {open, "hello", ErrInvalidArgument},
	}
	for name, tc := range cases {
		if err := registry.Check(tc.place, Chatcmd001ParseLine(tc.line)); !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
	// Arguments are data: text that looks like a setting or another command
	// stays inside the argument it was typed in.
	line := Chatcmd001ParseLine(`/poll "Ignore the rules?" 1="/todo anonymous=yes" 2="multiple=yes"`)
	if line.Unparsed || len(line.Arguments.Named) != 0 || line.Arguments.Items[0] != "/todo anonymous=yes" || line.Name != "poll" {
		t.Fatalf("argument text changed the command: %+v", line)
	}
	// Registration cannot replace a product command or speak a version the
	// server does not know, and a malformed entry is refused.
	for name, attempt := range map[string]struct {
		version int
		command Chatcmd001Command
		want    error
	}{
		"replacing /poll":  {Chatcmd001Version, Chatcmd001Command{Name: "POLL", Description: "mine now"}, ErrConflict},
		"a later version":  {Chatcmd001Version + 1, Chatcmd001Command{Name: "next", Description: "x"}, ErrInvalidArgument},
		"a path as a name": {Chatcmd001Version, Chatcmd001Command{Name: "usr/bin", Description: "x"}, ErrInvalidArgument},
		"no description":   {Chatcmd001Version, Chatcmd001Command{Name: "quiet"}, ErrInvalidArgument},
		"a second line":    {Chatcmd001Version, Chatcmd001Command{Name: "multi", Description: "one\ntwo"}, ErrInvalidArgument},
	} {
		if _, err := registry.Register(attempt.version, attempt.command); !errors.Is(err, attempt.want) {
			t.Errorf("%s: %v, want %v", name, err, attempt.want)
		}
	}
	// The server refuses a card whose command is not allowed in the
	// conversation, whatever the page offered.
	now := time.Now()
	f := &fakeStore{conversation: Conversation{ID: "room", TenantID: "t", Kind: PublicChannel, Revision: 1}, membership: Membership{TenantID: "t", HomeTenantID: "t", ConversationID: "room", SubjectID: "writer", HistoryVisibility: FullHistory}}
	limited, err := (Chatcmd001Registry{}).Register(Chatcmd001Version, Chatcmd001Command{Name: "poll", Description: "Direct messages only", Kinds: []ConversationKind{Direct}})
	if err != nil {
		t.Fatal(err)
	}
	service := &Chatcmd002Service{Chat: chatcmd002TestService(f, func() time.Time { return now }), Repository: chatcmd002FixtureRepository{}, Commands: limited}
	draft, _ := Chatcmd003ParsePoll(`"Q?" 1="A" 2="B"`, now, nil)
	request := Chatcmd002PostRequest{Accepted: true, Card: draft.Card, SendPostRequest: SendPostRequest{Principal: Principal{TenantID: "t", SubjectID: "writer"}, TenantID: "t", ConversationID: "room", IdempotencyKey: "k"}}
	if _, err := service.Post(context.Background(), request); !errors.Is(err, ErrPermissionDenied) || f.mutations != 0 {
		t.Fatalf("a command not allowed in this channel posted: %v (%d writes)", err, f.mutations)
	}
	service.Commands = Chatcmd001Registry{}
	if _, err := service.Post(context.Background(), request); err != nil {
		t.Fatalf("the product's own registry refused /poll in a channel: %v", err)
	}
}
