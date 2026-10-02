package chat

import (
	"encoding/json"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const Chatcmd002BodyMarker = "\n<hcm-message-card-v1>\n"

// Chatcmd002Card is an attachment carried by an ordinary message. Option and
// task rows reuse the standing channel widgets' contracts.
type Chatcmd002Card struct {
	Kind       string          `json:"kind"`
	Title      string          `json:"title"`
	Poll       *Chatcmd002Poll `json:"poll,omitempty"`
	Todo       *Chatcmd002Todo `json:"todo,omitempty"`
	ClosedAt   *time.Time      `json:"closed_at,omitempty"`
	Interacted bool            `json:"interacted,omitempty"`
}
type Chatcmd002Poll struct {
	Options    []ChannelPollOption `json:"options"`
	Multiple   bool                `json:"multiple"`
	Anonymous  bool                `json:"anonymous"`
	ClosesAt   *time.Time          `json:"closes_at,omitempty"`
	Results    string              `json:"results"`
	AddOptions string              `json:"add_options"`
}
type Chatcmd002Todo struct {
	Items []Chatcmd002Task `json:"items"`
	Tick  string           `json:"tick"`
}
type Chatcmd002Task struct {
	ChannelTodoItem
	AssigneeHomeTenantID string     `json:"assignee_home_tenant_id,omitempty"`
	AssigneeID           string     `json:"assignee_id,omitempty"`
	AssigneeName         string     `json:"assignee_name,omitempty"`
	DueAt                *time.Time `json:"due_at,omitempty"`
}
type Chatcmd002View struct {
	Card      Chatcmd002Card `json:"card"`
	MyOptions []string       `json:"my_options,omitempty"`
	// Voted says the reader has cast a ballot. An anonymous poll keeps no record
	// of which option that was, so MyOptions stays empty for it.
	Voted     bool `json:"voted,omitempty"`
	CanManage bool `json:"can_manage"`
	// CanAddOption says the reader may add an option to this poll: the poll lets
	// members add them, or the reader wrote it, and it is open and has room.
	CanAddOption   bool                         `json:"can_add_option,omitempty"`
	CanTick        map[string]bool              `json:"can_tick,omitempty"`
	ResultsVisible bool                         `json:"results_visible"`
	Voters         map[string][]Chatcmd002Voter `json:"voters,omitempty"`
	// Revision is the post revision this view was read at.
	Revision uint64 `json:"revision,omitempty"`
	// Notice is set by the client only: why the reader's last press on this
	// card was not accepted ("conflict", "final", "denied", "failed").
	Notice string `json:"-"`
}

// Chatcmd002Voter identifies who chose an option of a poll that shows names.
type Chatcmd002Voter struct {
	HomeTenantID string `json:"home_tenant_id"`
	SubjectID    string `json:"subject_id"`
}

// Closed reports whether the card takes no more votes or ticks at now: its
// author closed it, or the poll's closing time has passed.
func (c Chatcmd002Card) Closed(now time.Time) bool {
	return c.ClosedAt != nil || c.Poll != nil && c.Poll.ClosesAt != nil && !now.Before(*c.Poll.ClosesAt)
}

type Chatcmd002Mutation struct {
	Operation string          `json:"operation"`
	Options   []string        `json:"options,omitempty"`
	ItemID    string          `json:"item_id,omitempty"`
	Completed bool            `json:"completed"`
	Card      *Chatcmd002Card `json:"card,omitempty"`
	// Text is the new option of an ADD_OPTION.
	Text string `json:"text,omitempty"`
}

func chatcmd002Text(text string, max int) bool {
	if strings.TrimSpace(text) == "" || len(text) > max || !utf8.ValidString(text) || strings.Contains(text, Chatcmd002BodyMarker) {
		return false
	}
	for _, c := range text {
		if unicode.IsControl(c) && c != '\n' && c != '\t' {
			return false
		}
	}
	return true
}
func (c Chatcmd002Card) Validate() error {
	if !chatcmd002Text(c.Title, 240) {
		return ErrInvalidArgument
	}
	switch c.Kind {
	case "poll":
		if c.Poll == nil || c.Todo != nil || len(c.Poll.Options) < 2 || len(c.Poll.Options) > 12 {
			return ErrInvalidArgument
		}
		if c.Poll.Results != "always" && c.Poll.Results != "after-voting" && c.Poll.Results != "after-closing" {
			return ErrInvalidArgument
		}
		if c.Poll.AddOptions != "author" && c.Poll.AddOptions != "members" {
			return ErrInvalidArgument
		}
		seen, ids := map[string]bool{}, map[string]bool{}
		for _, o := range c.Poll.Options {
			key := strings.ToLower(strings.TrimSpace(o.Text))
			if !chatcmd002Text(o.Text, 100) || seen[key] || o.Count < 0 || (o.ID != "" && ids[o.ID]) {
				return ErrInvalidArgument
			}
			seen[key] = true
			if o.ID != "" {
				ids[o.ID] = true
			}
		}
	case "todo":
		if c.Todo == nil || c.Poll != nil || len(c.Todo.Items) < 1 || len(c.Todo.Items) > 30 {
			return ErrInvalidArgument
		}
		if c.Todo.Tick != "anyone" && c.Todo.Tick != "assignee" && c.Todo.Tick != "author" {
			return ErrInvalidArgument
		}
		ids := map[string]bool{}
		for _, item := range c.Todo.Items {
			if !chatcmd002Text(item.Text, 500) || (item.ID != "" && ids[item.ID]) || (item.AssigneeID == "") != (item.AssigneeHomeTenantID == "") {
				return ErrInvalidArgument
			}
			if item.ID != "" {
				ids[item.ID] = true
			}
		}
	default:
		return ErrInvalidArgument
	}
	return nil
}
func (c Chatcmd002Card) SearchText() string {
	lines := []string{c.Title}
	if c.Poll != nil {
		for _, o := range c.Poll.Options {
			lines = append(lines, o.Text)
		}
	}
	if c.Todo != nil {
		for _, item := range c.Todo.Items {
			lines = append(lines, item.Text)
		}
	}
	return strings.Join(lines, "\n")
}
func (c Chatcmd002Card) Body() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(c)
	return c.SearchText() + Chatcmd002BodyMarker + string(b), err
}
func Chatcmd002Decode(body string) (Chatcmd002Card, bool) {
	var c Chatcmd002Card
	prefix, raw, ok := strings.Cut(body, Chatcmd002BodyMarker)
	if !ok || len(raw) > 24000 {
		return c, false
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&c) != nil || decoder.Decode(new(any)) != io.EOF || c.Validate() != nil || prefix != c.SearchText() {
		return Chatcmd002Card{}, false
	}
	return c, true
}

type Chatcmd003Draft struct {
	Card     Chatcmd002Card     `json:"card"`
	Original Chatcmd002Card     `json:"original"`
	Raw      string             `json:"raw"`
	Issues   []string           `json:"issues,omitempty"`
	Changes  []Chatcmd003Change `json:"changes,omitempty"`
}

// Chatcmd003Change is one thing the preview changed in what was typed. Kind is
// empty for wording (Before became After; an empty After is a repeated option
// merged away) and otherwise names what was read out of a task or a line
// (Chatcmd004ChangeSplit, Chatcmd004ChangeAssignee, Chatcmd004ChangeDue).
type Chatcmd003Change struct {
	Before, After string
	Kind          string `json:",omitempty"`
}

func Chatcmd003ParsePoll(raw string, now time.Time, resolveDate func(string, time.Time) (time.Time, error)) (Chatcmd003Draft, error) {
	d := Chatcmd003Draft{Raw: raw, Card: Chatcmd002Card{Kind: "poll", Poll: &Chatcmd002Poll{Results: "always", AddOptions: "author"}}}
	a, err := Chatcmd003ParseArguments(raw)
	if err != nil {
		return d, err
	}
	d.Card.Title = a.Text
	if a.Explicit {
		for _, text := range a.Items {
			d.Card.Poll.Options = append(d.Card.Poll.Options, ChannelPollOption{Text: text})
		}
	} else {
		title, options, guessed := chatcmd003SplitLoosePoll(raw)
		d.Card.Title = title
		for _, text := range options {
			d.Card.Poll.Options = append(d.Card.Poll.Options, ChannelPollOption{Text: text})
		}
		// Only a split that had to guess where the question ends is put to the
		// person before it may be posted.
		if guessed {
			d.Issues = append(d.Issues, "separate-question")
		}
	}
	for key, value := range a.Named {
		switch key {
		case "multiple", "anonymous":
			if value != "yes" && value != "no" {
				return d, ErrInvalidArgument
			}
			if key == "multiple" {
				d.Card.Poll.Multiple = value == "yes"
			} else {
				d.Card.Poll.Anonymous = value == "yes"
			}
		case "results":
			d.Card.Poll.Results = value
		case "add":
			d.Card.Poll.AddOptions = value
		case "closes":
			if value == "never" {
				continue
			}
			if resolveDate == nil {
				return d, ErrInvalidArgument
			}
			due, e := resolveDate(value, now)
			if e != nil {
				d.Issues = append(d.Issues, "date")
				continue
			}
			d.Card.Poll.ClosesAt = &due
		default:
			return d, ErrInvalidArgument
		}
	}
	d.Original = chatcmd003Clone(d.Card)
	return d, nil
}
func chatcmd003Clone(c Chatcmd002Card) Chatcmd002Card {
	b, _ := json.Marshal(c)
	var out Chatcmd002Card
	_ = json.Unmarshal(b, &out)
	return out
}

// Chatcmd002MaxOptions is the most options a poll holds.
const Chatcmd002MaxOptions = 12

// AddOption is the poll with one more option at its end. It refuses a card
// that is not a poll, a poll that already holds twelve, and text that is empty,
// too long, or the same (ignoring case and spacing) as an option it has.
func (c Chatcmd002Card) AddOption(id, text string) (Chatcmd002Card, error) {
	if c.Poll == nil || len(c.Poll.Options) >= Chatcmd002MaxOptions || id == "" || !chatcmd002Text(text, 100) {
		return c, ErrInvalidArgument
	}
	key := strings.ToLower(strings.TrimSpace(text))
	for _, option := range c.Poll.Options {
		if strings.ToLower(strings.TrimSpace(option.Text)) == key {
			return c, ErrConflict
		}
	}
	poll := *c.Poll
	poll.Options = append(append([]ChannelPollOption(nil), poll.Options...), ChannelPollOption{ID: id, Text: strings.TrimSpace(text)})
	c.Poll = &poll
	return c, nil
}

// CanAddOption reports whether a reader may add an option to this poll at now:
// it is open, has room, and either lets members add options or the reader
// wrote it.
func (c Chatcmd002Card) CanAddOption(now time.Time, author bool) bool {
	return c.Poll != nil && !c.Closed(now) && len(c.Poll.Options) < Chatcmd002MaxOptions && (c.Poll.AddOptions == "members" || author)
}
