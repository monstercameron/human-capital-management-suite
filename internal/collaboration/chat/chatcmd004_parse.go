package chat

import (
	"regexp"
	"strings"
	"time"
)

type Chatcmd004Member struct{ HomeTenantID, ID, Name string }

// Chatcmd004ResolveDate only resolves unambiguous dates. The caller supplies
// the conversation/author zone in now; numeric local dates need confirmation.
func Chatcmd004ResolveDate(text string, now time.Time) (time.Time, error) {
	text = strings.TrimSpace(text)
	if due, err := time.Parse(time.RFC3339, text); err == nil {
		return due, nil
	}
	text = strings.ToLower(text)
	aliases := map[string]string{"heute": "today", "morgen": "tomorrow", "in einer stunde": "in an hour", "tagesende": "end of day", "sonntag": "sunday", "montag": "monday", "dienstag": "tuesday", "mittwoch": "wednesday", "donnerstag": "thursday", "freitag": "friday", "samstag": "saturday", "اليوم": "today", "غدا": "tomorrow", "غدًا": "tomorrow", "بعد ساعة": "in an hour", "نهاية اليوم": "end of day", "الأحد": "sunday", "الاثنين": "monday", "الثلاثاء": "tuesday", "الأربعاء": "wednesday", "الخميس": "thursday", "الجمعة": "friday", "السبت": "saturday"}
	if alias, ok := aliases[text]; ok {
		text = alias
	}
	if text == "in an hour" || text == "hour" {
		return now.Add(time.Hour), nil
	}
	day := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 0, 0, now.Location())
	if text == "today" || text == "end of day" {
		return day, nil
	}
	if text == "tomorrow" {
		return day.AddDate(0, 0, 1), nil
	}
	for weekday, name := range []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"} {
		if text == name {
			days := (weekday - int(now.Weekday()) + 7) % 7
			if days == 0 {
				days = 7
			}
			return day.AddDate(0, 0, days), nil
		}
	}
	if due, err := time.ParseInLocation("2006-01-02", text, now.Location()); err == nil {
		return due.Add(23*time.Hour + 59*time.Minute), nil
	}
	if due, err := time.Parse(time.RFC3339, text); err == nil {
		return due, nil
	}
	return time.Time{}, ErrInvalidArgument
}

func Chatcmd004ParseTodo(raw string, members []Chatcmd004Member, author string, now time.Time, resolveDate func(string, time.Time) (time.Time, error)) (Chatcmd003Draft, error) {
	d := Chatcmd003Draft{Raw: raw, Card: Chatcmd002Card{Kind: "todo", Todo: &Chatcmd002Todo{Tick: "anyone"}}}
	a, err := Chatcmd003ParseArguments(raw)
	if err != nil {
		return d, err
	}
	texts := a.Items
	if !a.Explicit {
		texts = chatcmd003LooseItems(raw)
	} else {
		d.Card.Title = a.Text
	}
	if tick, ok := a.Named["tick"]; ok {
		d.Card.Todo.Tick = tick
	}
	for key := range a.Named {
		if key != "tick" {
			return d, ErrInvalidArgument
		}
	}
	if len(texts) > 30 {
		return d, ErrInvalidArgument
	}
	for _, text := range texts {
		item := Chatcmd002Task{ChannelTodoItem: ChannelTodoItem{Text: text}}
		// Numbered lines have no title unless one was quoted explicitly.
		item.Text = regexp.MustCompile(`^\s*\d+[.)]\s+`).ReplaceAllString(item.Text, "")
		index, separator := -1, 0
		for _, span := range regexp.MustCompile(`(?i)( by | bis | بحلول )`).FindAllStringIndex(item.Text, -1) {
			index, separator = span[0], span[1]-span[0]
		}
		if index >= 0 {
			value := strings.TrimSpace(item.Text[index+separator:])
			if resolveDate != nil {
				due, e := resolveDate(value, now)
				if e == nil {
					item.DueAt = &due
					item.Text = strings.TrimSpace(item.Text[:index])
					if due.Before(now) {
						d.Issues = append(d.Issues, "past-date")
					}
				} else {
					d.Issues = append(d.Issues, "date")
				}
			}
		}
		// An explicit mention, an exact leading member name, or I/I'll assigns.
		candidate, start, end := "", 0, 0
		if at := strings.Index(item.Text, "@"); at >= 0 {
			for _, m := range members {
				rest := item.Text[at+1:]
				if len(rest) >= len(m.Name) && strings.EqualFold(rest[:len(m.Name)], m.Name) && (len(rest) == len(m.Name) || rest[len(m.Name)] == ' ') {
					if len(m.Name) > len(candidate) {
						candidate = m.Name
						start = at
						end = at + 1 + len(m.Name)
					}
				}
			}
			if candidate == "" {
				end = at + 1
				for end < len(item.Text) && !strings.ContainsRune(" \t\n,;", rune(item.Text[end])) {
					end++
				}
				candidate = item.Text[at+1 : end]
				start = at
			}
		} else {
			word, _, _ := strings.Cut(item.Text, " ")
			if strings.EqualFold(word, "I") || strings.EqualFold(word, "I'll") || strings.EqualFold(word, "I’ll") || strings.EqualFold(word, "ich") || word == "أنا" {
				candidate = "self"
				end = len(word)
			} else {
				candidate = word
				end = len(word)
			}
		}
		var matches []Chatcmd004Member
		for _, m := range members {
			first, _, _ := strings.Cut(m.Name, " ")
			if (candidate == "self" && m.ID == author) || strings.EqualFold(candidate, m.Name) || strings.EqualFold(candidate, first) {
				matches = append(matches, m)
			}
		}
		if len(matches) == 1 {
			item.AssigneeHomeTenantID, item.AssigneeID, item.AssigneeName = matches[0].HomeTenantID, matches[0].ID, matches[0].Name
			item.Text = strings.TrimSpace(item.Text[:start] + item.Text[end:])
		} else if len(matches) > 1 || strings.Contains(item.Text, "@") {
			d.Issues = append(d.Issues, "assignee")
		}
		d.Card.Todo.Items = append(d.Card.Todo.Items, item)
	}
	d.Original = chatcmd003Clone(d.Card)
	return d, nil
}
