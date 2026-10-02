package chatsearch

import (
	"strings"
	"time"
)

type Filters struct {
	Conversation, Person                                           string
	Kind                                                           Kind
	Before, After, On                                              string
	File, Link, Reactions, Threads, MentionsMe, Agent, Mine, Voice bool
}

func (f Filters) Empty() bool { return f == (Filters{}) }

type Parsed struct {
	Text    string
	Filters Filters
	Chips   []string
}

// Tokens supports quoted names, for example from:"Full Name". Invalid known
// operators fail closed instead of silently widening the caller's search.
func Tokens(text string) ([]string, error) {
	var tokens []string
	var b strings.Builder
	quoted := false
	for _, ch := range text {
		if ch == '"' {
			quoted = !quoted
			continue
		}
		if (ch == ' ' || ch == '\n' || ch == '\t') && !quoted {
			if b.Len() > 0 {
				tokens = append(tokens, b.String())
				b.Reset()
			}
			continue
		}
		b.WriteRune(ch)
	}
	if quoted {
		return nil, ErrInvalid
	}
	if b.Len() > 0 {
		tokens = append(tokens, b.String())
	}
	return tokens, nil
}
func Parse(query string) (Parsed, error) {
	var p Parsed
	tokens, err := Tokens(query)
	if err != nil {
		return p, err
	}
	var words []string
	for _, token := range tokens {
		key, value, operator := strings.Cut(token, ":")
		if !operator {
			words = append(words, token)
			continue
		}
		f := Filters{}
		known := true
		switch strings.ToLower(key) {
		case "in":
			f.Conversation = strings.TrimPrefix(value, "#")
		case "from":
			if value == "agent" {
				f.Agent = true
			} else {
				f.Person = strings.TrimPrefix(value, "@")
			}
		case "kind":
			f.Kind = Kind(value)
		case "before":
			f.Before = value
		case "after":
			f.After = value
		case "on":
			f.On = value
		case "has":
			switch value {
			case "file":
				f.File = true
			case "link":
				f.Link = true
			case "reactions":
				f.Reactions = true
			case "voice":
				f.Voice = true
			default:
				return p, ErrInvalid
			}
		case "is":
			switch value {
			case "thread":
				f.Threads = true
			case "mine":
				f.Mine = true
			default:
				return p, ErrInvalid
			}
		case "mentions":
			if value != "me" {
				return p, ErrInvalid
			}
			f.MentionsMe = true
		default:
			known = false
		}
		if !known {
			words = append(words, token)
			continue
		}
		if value == "" {
			return p, ErrInvalid
		}
		p.Filters, err = Combine(p.Filters, f)
		if err != nil {
			return p, err
		}
		p.Chips = append(p.Chips, token)
	}
	p.Text = strings.Join(words, " ")
	return p, nil
}
func Combine(a, b Filters) (Filters, error) {
	pairs := [][2]*string{{&a.Conversation, &b.Conversation}, {&a.Person, &b.Person}, {&a.Before, &b.Before}, {&a.After, &b.After}, {&a.On, &b.On}}
	for _, p := range pairs {
		if *p[0] != "" && *p[1] != "" && *p[0] != *p[1] {
			return Filters{}, ErrInvalid
		}
		if *p[1] != "" {
			*p[0] = *p[1]
		}
	}
	if a.Kind != "" && b.Kind != "" && a.Kind != b.Kind {
		return Filters{}, ErrInvalid
	}
	if b.Kind != "" {
		a.Kind = b.Kind
	}
	a.File = a.File || b.File
	a.Link = a.Link || b.Link
	a.Reactions = a.Reactions || b.Reactions
	a.Threads = a.Threads || b.Threads
	a.MentionsMe = a.MentionsMe || b.MentionsMe
	a.Agent = a.Agent || b.Agent
	a.Mine = a.Mine || b.Mine
	a.Voice = a.Voice || b.Voice
	for _, v := range []string{a.Before, a.After, a.On} {
		if v != "" {
			if _, err := time.Parse("2006-01-02", v); err != nil {
				return Filters{}, ErrInvalid
			}
		}
	}
	if a.Before != "" && a.After != "" && a.Before <= a.After {
		return Filters{}, ErrInvalid
	}
	return a, nil
}
func Match(row Row, q Request) bool {
	f := q.Filters
	if f.Conversation != "" && row.Target.ConversationID != f.Conversation || f.Person != "" && row.AuthorID != f.Person || f.Kind != "" && row.Kind != f.Kind || f.File && !row.HasFile || f.Link && !row.HasLink || f.Reactions && !row.HasReactions || f.Threads && !row.InThread || f.MentionsMe && !row.MentionsMe || f.Agent && !row.ByAgent || f.Mine && row.OwnerID != q.Actor.PersonID && row.AuthorID != q.Actor.PersonID || f.Voice && !row.HasVoice {
		return false
	}
	day := row.At.UTC().Format("2006-01-02")
	if f.Before != "" && day >= f.Before || f.After != "" && day <= f.After || f.On != "" && day != f.On {
		return false
	}
	for _, word := range strings.Fields(q.Query) {
		if word == "*" {
			continue
		}
		if !strings.Contains(strings.ToLower(row.Text), strings.ToLower(word)) {
			return false
		}
	}
	return true
}

// RemoveChip rebuilds a typed query without the selected operator. It retains
// quoting, so removing one chip cannot reinterpret a name as another filter.
func RemoveChip(query, chip string) (string, error) {
	tokens, e := Tokens(query)
	if e != nil {
		return "", e
	}
	out := []string{}
	removed := false
	for _, token := range tokens {
		if !removed && token == chip {
			removed = true
			continue
		}
		if strings.Contains(token, " ") {
			key, value, ok := strings.Cut(token, ":")
			if ok {
				token = key + `:"` + value + `"`
			} else {
				token = `"` + token + `"`
			}
		}
		out = append(out, token)
	}
	return strings.Join(out, " "), nil
}
