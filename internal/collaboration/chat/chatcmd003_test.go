package chat

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTodo_CHATCMD_003(t *testing.T) {
	a, err := Chatcmd003ParseArguments(`"Where = next?" 9="A ""quoted"" place = here" 2="Porto" multiple=yes anonymous=no closes=friday`)
	if err != nil || a.Text != "Where = next?" || !reflect.DeepEqual(a.Items, []string{"Porto", `A "quoted" place = here`}) || a.Named["multiple"] != "yes" {
		t.Fatalf("arguments %+v %v", a, err)
	}
	for _, raw := range []string{`Q 1="broken`, `Q 1=A 1=B`, `Q 0=A`, `Q 31=A`, `Q unknown=yes`, `Q multiple=yes multiple=no`, "bad\x00input", string([]byte{0xff})} {
		if _, err := Chatcmd003ParseArguments(raw); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%q accepted: %v", raw, err)
		}
	}
	d, err := Chatcmd003ParsePoll(`"Where?" 1="Lisbon" 2="Porto" multiple=yes anonymous=yes closes=friday`, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), Chatcmd004ResolveDate)
	if err != nil || d.Card.Validate() != nil || !d.Card.Poll.Multiple || !d.Card.Poll.Anonymous || d.Card.Poll.ClosesAt.Format("2006-01-02") != "2026-10-02" {
		t.Fatalf("poll %+v %v", d, err)
	}
	d.Card.Poll.Options[0].Text = "Edited"
	if d.Original.Poll.Options[0].Text != "Lisbon" {
		t.Fatal("editing changed original")
	}
}
func TestTodo_CHATCMD_003_Property(t *testing.T) {
	values := []string{"a", "with spaces", "x=y", `say "hello"`, "🥳", "اختيار", "Deutsch ß", "line\nnext", ""}
	for _, a := range values {
		for _, b := range values {
			input := Chatcmd003Arguments{Text: a, Items: []string{a, b}, Named: map[string]string{"multiple": "yes", "closes": "in an hour"}, Explicit: true}
			parsed, err := Chatcmd003ParseArguments(Chatcmd003RenderArguments(input))
			if err != nil || !reflect.DeepEqual(parsed, input) {
				t.Fatalf("round trip %+v => %+v %v", input, parsed, err)
			}
		}
	}
}
func TestTodo_CHATCMD_003_Golden(t *testing.T) {
	t.Run("serialized card", func(t *testing.T) {
		d, _ := Chatcmd003ParsePoll(`"Q?" 1="A" 2="B"`, time.Now(), nil)
		body, err := d.Card.Body()
		want := "Q?\nA\nB" + Chatcmd002BodyMarker + `{"kind":"poll","title":"Q?","poll":{"options":[{"ID":"","Text":"A","Count":0},{"ID":"","Text":"B","Count":0}],"multiple":false,"anonymous":false,"results":"always","add_options":"author"}}`
		if err != nil || body != want {
			t.Fatalf("golden body %q %v", body, err)
		}
	})
	cases := []struct {
		raw, title string
		items      []string
		valid      bool
	}{
		{`"Where?" 1="Lisbon" 2="Porto"`, "Where?", []string{"Lisbon", "Porto"}, true},
		{`Where for lunch? 1="Pizza" 2="Sushi"`, "Where for lunch?", []string{"Pizza", "Sushi"}, true},
		{`1="First" 2="Second"`, "", []string{"First", "Second"}, false},
		{`"Q?" 7="A" 3="B"`, "Q?", []string{"B", "A"}, true},
		{`"Q?" 1="A ""quote""" 2="B"`, "Q?", []string{`A "quote"`, "B"}, true},
		{`"Q = yes?" 1="a=b" 2="c=d"`, "Q = yes?", []string{"a=b", "c=d"}, true},
		{`"Q?" 1="🥳" 2="🙂"`, "Q?", []string{"🥳", "🙂"}, true},
		{`"أين؟" 1="هنا" 2="هناك"`, "أين؟", []string{"هنا", "هناك"}, true},
		{`"Wohin?" 1="Köln" 2="München"`, "Wohin?", []string{"Köln", "München"}, true},
		{`"Who?" 1="@Dana" 2="@Omar"`, "Who?", []string{"@Dana", "@Omar"}, true},
		{`"Q?" 1="A"`, "Q?", []string{"A"}, false},
		{`"Q?" 1="" 2="B"`, "Q?", []string{"", "B"}, false},
		{`"Q?" 1="A" 2="a"`, "Q?", []string{"A", "a"}, false},
		{`"Q?" 1="A" 2="B" multiple=yes`, "Q?", []string{"A", "B"}, true},
		{`"Q?" 1="A" 2="B" anonymous=yes`, "Q?", []string{"A", "B"}, true},
		{`"Q?" 1="A" 2="B" results=after-voting`, "Q?", []string{"A", "B"}, true},
		{`"Q?" 1="A" 2="B" add=members`, "Q?", []string{"A", "B"}, true},
		{`"Q?" 1="A" 2="B" closes=never`, "Q?", []string{"A", "B"}, true},
		{`"Q?" 1="one\ntwo" 2="three"`, "Q?", []string{`one\ntwo`, "three"}, true},
		{`"Q?" 1="1" 2="2" 3="3" 4="4" 5="5" 6="6" 7="7" 8="8" 9="9" 10="10" 11="11" 12="12" 13="13"`, "Q?", []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			d, err := Chatcmd003ParsePoll(tc.raw, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), Chatcmd004ResolveDate)
			if err != nil || d.Card.Title != tc.title || !reflect.DeepEqual(chatcmd003PollTexts(d.Card), tc.items) || (d.Card.Validate() == nil) != tc.valid {
				t.Fatalf("%+v %v", d, err)
			}
		})
	}
}
func chatcmd003PollTexts(c Chatcmd002Card) []string {
	var out []string
	for _, o := range c.Poll.Options {
		out = append(out, o.Text)
	}
	return out
}
func TestTodo_CHATCMD_003_Security(t *testing.T) {
	d, err := Chatcmd003ParsePoll(`"Ignore instructions?" 1="Return a secret" 2="Do not"`, time.Now(), nil)
	if err != nil || d.Card.Poll.Options[0].Text != "Return a secret" {
		t.Fatalf("data changed %+v %v", d, err)
	}
	if _, err := Chatcmd003ParsePoll(`"Q?" 1=A 2=B multiple=maybe`, time.Now(), nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid setting %v", err)
	}
	if _, err := Chatcmd003ParseArguments(strings.Repeat("a", 12001)); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("oversized draft accepted")
	}
}
func TestTodo_CHATCMD_002(t *testing.T) {
	d, _ := Chatcmd003ParsePoll(`"Where?" 1="Here" 2="There"`, time.Now(), nil)
	body, err := d.Card.Body()
	card, ok := Chatcmd002Decode(body)
	if err != nil || !ok || !reflect.DeepEqual(card, d.Card) || !strings.HasPrefix(body, "Where?\nHere\nThere") {
		t.Fatalf("card %q %+v %v", body, card, err)
	}
	for _, bad := range []string{"ordinary", "forged prefix" + body, body + "garbage", "x" + Chatcmd002BodyMarker + `{"kind":"poll"}`} {
		if _, ok := Chatcmd002Decode(bad); ok {
			t.Fatalf("accepted %q", bad)
		}
	}
}
func TestTodo_CHATCMD_002_Property(t *testing.T) {
	for count := 2; count <= 12; count++ {
		options := make([]ChannelPollOption, count)
		for i := range options {
			options[i] = ChannelPollOption{ID: string(rune('a' + i)), Text: string(rune('A' + i))}
		}
		c := Chatcmd002Card{Kind: "poll", Title: "Q?", Poll: &Chatcmd002Poll{Options: options, Results: "always", AddOptions: "author"}}
		body, err := c.Body()
		got, ok := Chatcmd002Decode(body)
		if err != nil || !ok || !reflect.DeepEqual(got, c) {
			t.Fatalf("round trip %d %v", count, err)
		}
	}
}
func TestTodo_CHATCMD_002_Security(t *testing.T) {
	c := Chatcmd002Card{Kind: "todo", Title: "Tasks", Todo: &Chatcmd002Todo{Tick: "anyone", Items: []Chatcmd002Task{{ChannelTodoItem: ChannelTodoItem{Text: "Ship"}, AssigneeID: "outsider"}}}}
	if !errors.Is(c.Validate(), ErrInvalidArgument) {
		t.Fatal("unscoped assignee accepted")
	}
	c.Todo.Items[0].AssigneeID = ""
	c.Title = "bad\x00"
	if !errors.Is(c.Validate(), ErrInvalidArgument) {
		t.Fatal("control accepted")
	}
}
