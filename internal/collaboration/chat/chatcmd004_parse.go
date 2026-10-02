package chat

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Chatcmd004Member struct{ HomeTenantID, ID, Name string }

// What the reading of a to-do list reports beside the wording changes of the
// tidy step (Chatcmd003Change.Kind): a line that became several tasks, and an
// assignee or a due date taken out of a task's text.
const (
	Chatcmd004ChangeSplit    = "split"
	Chatcmd004ChangeAssignee = "assignee"
	Chatcmd004ChangeDue      = "due"
)

var (
	chatcmd004Weekdays = []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}
	// Whole words of the three product languages for the days a date can name.
	chatcmd004DayWords = map[string]string{"heute": "today", "morgen": "tomorrow", "sonntag": "sunday", "montag": "monday", "dienstag": "tuesday", "mittwoch": "wednesday", "donnerstag": "thursday", "freitag": "friday", "samstag": "saturday",
		"اليوم": "today", "غدا": "tomorrow", "غدًا": "tomorrow", "الأحد": "sunday", "الاثنين": "monday", "الثلاثاء": "tuesday", "الأربعاء": "wednesday", "الخميس": "thursday", "الجمعة": "friday", "السبت": "saturday"}
	// Forms that are words in their own right ("sat", "an hour") and therefore
	// only read as a date where a date is asked for: after "by", or in closes=.
	chatcmd004ShortWords = map[string]string{"in einer stunde": "in an hour", "hour": "in an hour", "بعد ساعة": "in an hour", "tagesende": "end of day", "نهاية اليوم": "end of day", "tonight": "end of day",
		"mon": "monday", "tue": "tuesday", "tues": "tuesday", "wed": "wednesday", "thu": "thursday", "thur": "thursday", "thurs": "thursday", "fri": "friday", "sat": "saturday", "sun": "sunday"}
	chatcmd004Months = map[string]time.Month{
		"jan": 1, "january": 1, "januar": 1, "يناير": 1, "feb": 2, "february": 2, "februar": 2, "فبراير": 2, "mar": 3, "march": 3, "märz": 3, "مارس": 3,
		"apr": 4, "april": 4, "أبريل": 4, "may": 5, "mai": 5, "مايو": 5, "jun": 6, "june": 6, "juni": 6, "يونيو": 6, "jul": 7, "july": 7, "juli": 7, "يوليو": 7,
		"aug": 8, "august": 8, "أغسطس": 8, "sep": 9, "sept": 9, "september": 9, "سبتمبر": 9, "oct": 10, "october": 10, "okt": 10, "oktober": 10, "أكتوبر": 10,
		"nov": 11, "november": 11, "نوفمبر": 11, "dec": 12, "december": 12, "dez": 12, "dezember": 12, "ديسمبر": 12}
	chatcmd004ISODate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	// A word that says a date follows. Group 1 is the word.
	chatcmd004DateLead = regexp.MustCompile(`(?i)(?:^|\s)(by|due|until|before|on|bis|am|vor|بحلول|قبل)\s+`)
)

// chatcmd004MonthDay reads a month with a day, in either order and with or
// without a year: "oct 9", "October 9th, 2026", "9 oct", "9. Oktober". Without
// a year it is the next such day, this year or the one after.
func chatcmd004MonthDay(text string, now time.Time) (time.Time, bool) {
	fields := strings.Fields(strings.NewReplacer(",", " ", ".", " ").Replace(text))
	if len(fields) < 2 || len(fields) > 3 {
		return time.Time{}, false
	}
	month, day, year := time.Month(0), 0, 0
	for _, field := range fields {
		if named, ok := chatcmd004Months[field]; ok && month == 0 {
			month = named
			continue
		}
		digits := field
		if len(digits) > 2 && digits[0] >= '0' && digits[0] <= '9' {
			// 1st, 2nd, 3rd, 9th
			digits = strings.TrimRight(field, "stndrh")
		}
		n, err := strconv.Atoi(digits)
		switch {
		case err != nil:
			return time.Time{}, false
		case day == 0 && n >= 1 && n <= 31 && len(digits) <= 2:
			day = n
		case year == 0 && n >= 2000 && n <= 2100 && digits == field:
			year = n
		default:
			return time.Time{}, false
		}
	}
	if month == 0 || day == 0 {
		return time.Time{}, false
	}
	at := func(year int) (time.Time, bool) {
		due := time.Date(year, month, day, 23, 59, 0, 0, now.Location())
		// 31 February is not a date; time.Date would make it one in March.
		return due, due.Month() == month && due.Day() == day
	}
	if year != 0 {
		return at(year)
	}
	due, ok := at(now.Year())
	if !ok || due.Before(now) {
		due, ok = at(now.Year() + 1)
	}
	return due, ok
}

// Chatcmd004ResolveDate only resolves unambiguous dates. The caller supplies
// the conversation/author zone in now; numeric local dates need confirmation.
func Chatcmd004ResolveDate(text string, now time.Time) (time.Time, error) {
	text = strings.TrimSpace(text)
	if due, err := time.Parse(time.RFC3339, text); err == nil {
		return due, nil
	}
	text = strings.ToLower(text)
	if word, ok := chatcmd004DayWords[text]; ok {
		text = word
	} else if word, ok := chatcmd004ShortWords[text]; ok {
		text = word
	}
	if text == "in an hour" {
		return now.Add(time.Hour), nil
	}
	day := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 0, 0, now.Location())
	if text == "today" || text == "end of day" {
		return day, nil
	}
	if text == "tomorrow" {
		return day.AddDate(0, 0, 1), nil
	}
	for weekday, name := range chatcmd004Weekdays {
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
	if due, ok := chatcmd004MonthDay(text, now); ok {
		return due, nil
	}
	return time.Time{}, ErrInvalidArgument
}

// chatcmd004Standalone reports whether a phrase reads as a date with no word
// before it to say so: today, tomorrow, the full name of a weekday, a month
// with a day, or a date written year-month-day.
func chatcmd004Standalone(phrase string, now time.Time) bool {
	phrase = strings.ToLower(phrase)
	if word, ok := chatcmd004DayWords[phrase]; ok {
		phrase = word
	}
	if phrase == "today" || phrase == "tomorrow" || chatcmd004ISODate.MatchString(phrase) {
		return true
	}
	for _, name := range chatcmd004Weekdays {
		if phrase == name {
			return true
		}
	}
	_, ok := chatcmd004MonthDay(phrase, now)
	return ok
}

// chatcmd004Due takes a due date out of a task: what follows "by" (or "due",
// "until", "on" and their German and Arabic forms), or a date standing alone at
// the end or the start of the task. rest is the task without it. unread is
// true when "by" is followed by figures that are not a date this can read
// (03/04 is March or April), which the preview asks about.
func chatcmd004Due(text string, now time.Time, resolve func(string, time.Time) (time.Time, error)) (due *time.Time, rest string, unread bool) {
	rest = text
	if spans := chatcmd004DateLead.FindAllStringSubmatchIndex(text, -1); len(spans) > 0 {
		last := spans[len(spans)-1]
		value := strings.TrimRight(strings.TrimSpace(text[last[1]:]), ".,!")
		before := strings.TrimSpace(text[:last[2]])
		if at, err := resolve(value, now); err == nil && before != "" {
			return &at, before, false
		}
		switch strings.ToLower(text[last[2]:last[3]]) {
		case "by", "bis", "بحلول":
			unread = strings.ContainsAny(value, "0123456789")
		}
	}
	words := strings.Fields(text)
	for n := min(3, len(words)-1); n >= 1; n-- {
		tail := strings.TrimRight(strings.Join(words[len(words)-n:], " "), ".,!")
		if chatcmd004Standalone(tail, now) {
			if at, err := resolve(tail, now); err == nil {
				return &at, strings.TrimRight(strings.Join(words[:len(words)-n], " "), " ,:-"), false
			}
		}
		head := strings.TrimRight(strings.Join(words[:n], " "), ".,:-")
		if chatcmd004Standalone(head, now) {
			if at, err := resolve(head, now); err == nil {
				return &at, strings.Join(words[n:], " "), false
			}
		}
	}
	return nil, text, unread
}

// chatcmd004Assignee finds the one member a task names: a mention, or a first
// word that is a member's name, or "I" for the author. rest is the task
// without the name. A mention or a name that fits no member, or more than one,
// is left in the text and reported as ambiguous: nobody is guessed.
func chatcmd004Assignee(text string, members []Chatcmd004Member, author string) (assignee *Chatcmd004Member, rest string, ambiguous bool) {
	candidate, start, end, self := "", 0, 0, false
	at := strings.Index(text, "@")
	if at >= 0 {
		tail := text[at+1:]
		for _, m := range members {
			// The longest full name that follows the sign, so "@Dana Smith" is
			// not read as a Dana followed by a word.
			if len(m.Name) > len(candidate) && len(tail) >= len(m.Name) && strings.EqualFold(tail[:len(m.Name)], m.Name) && (len(tail) == len(m.Name) || strings.ContainsRune(" \t\n,;.:!?", rune(tail[len(m.Name)]))) {
				candidate, start, end = m.Name, at, at+1+len(m.Name)
			}
		}
		if candidate == "" {
			end = at + 1
			for end < len(text) && !strings.ContainsRune(" \t\n,;", rune(text[end])) {
				end++
			}
			candidate, start = text[at+1:end], at
		}
	} else {
		word, _, _ := strings.Cut(text, " ")
		candidate, end = word, len(word)
		self = strings.EqualFold(word, "I") || strings.EqualFold(word, "I'll") || strings.EqualFold(word, "I’ll") || strings.EqualFold(word, "ich") || word == "أنا"
	}
	var matches []Chatcmd004Member
	for _, m := range members {
		first, _, _ := strings.Cut(m.Name, " ")
		if (self && m.ID == author) || (!self && candidate != "" && (strings.EqualFold(candidate, m.Name) || strings.EqualFold(candidate, first))) {
			matches = append(matches, m)
		}
	}
	rest = strings.TrimSpace(strings.TrimSpace(text[:start]) + " " + strings.TrimSpace(text[end:]))
	switch {
	case len(matches) == 1 && rest != "":
		return &matches[0], rest, false
	case len(matches) == 1:
		// A name and nothing else is a task with no words; it stays as typed.
		return nil, text, false
	case len(matches) > 1 || at >= 0:
		return nil, text, true
	}
	return nil, text, false
}

// chatcmd004LooseTasks is the tasks of a loosely typed list: one for each line
// and for each part between semicolons. One line without a semicolon is split
// at its commas instead ("book the room, send invites"). A line that became
// several tasks is reported, so the preview can say it was split.
func chatcmd004LooseTasks(raw string) (tasks []string, changes []Chatcmd003Change) {
	lines := chatcmd003LooseLines(raw)
	for _, line := range lines {
		var parts []string
		for _, part := range strings.FieldsFunc(line, func(r rune) bool { return r == ';' || r == '؛' }) {
			if part = strings.TrimSpace(part); part != "" {
				parts = append(parts, part)
			}
		}
		if len(lines) == 1 && len(parts) == 1 {
			parts = chatcmd003LooseItems(strings.ReplaceAll(parts[0], "،", ","))
		}
		if len(parts) > 1 {
			changes = append(changes, Chatcmd003Change{Kind: Chatcmd004ChangeSplit, Before: line, After: strconv.Itoa(len(parts))})
		}
		tasks = append(tasks, parts...)
	}
	return tasks, changes
}

func Chatcmd004ParseTodo(raw string, members []Chatcmd004Member, author string, now time.Time, resolveDate func(string, time.Time) (time.Time, error)) (Chatcmd003Draft, error) {
	d := Chatcmd003Draft{Raw: raw, Card: Chatcmd002Card{Kind: "todo", Todo: &Chatcmd002Todo{Tick: "anyone"}}}
	a, err := Chatcmd003ParseArguments(raw)
	if err != nil {
		return d, err
	}
	texts := a.Items
	if !a.Explicit {
		// A setting typed among loose tasks (tick=author) is not a task.
		texts, d.Changes = chatcmd004LooseTasks(raw)
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
		// Numbered lines have no title unless one was quoted explicitly.
		text = chatcmd003ListMarker.ReplaceAllString(strings.TrimSpace(text), "")
		item := Chatcmd002Task{ChannelTodoItem: ChannelTodoItem{Text: text}}
		// Each task is read on its own, so a name or a date belongs to the task
		// that carries it and to no other.
		assignee, rest, ambiguous := chatcmd004Assignee(item.Text, members, author)
		if assignee != nil {
			item.AssigneeHomeTenantID, item.AssigneeID, item.AssigneeName, item.Text = assignee.HomeTenantID, assignee.ID, assignee.Name, rest
		} else if ambiguous {
			d.Issues = append(d.Issues, "assignee")
		}
		if resolveDate != nil {
			due, rest, unread := chatcmd004Due(item.Text, now, resolveDate)
			if due != nil {
				item.DueAt, item.Text = due, rest
				if due.Before(now) {
					d.Issues = append(d.Issues, "past-date")
				}
			} else if unread {
				d.Issues = append(d.Issues, "date")
			}
		}
		if assignee != nil {
			d.Changes = append(d.Changes, Chatcmd003Change{Kind: Chatcmd004ChangeAssignee, Before: item.Text, After: assignee.Name})
		}
		if item.DueAt != nil {
			d.Changes = append(d.Changes, Chatcmd003Change{Kind: Chatcmd004ChangeDue, Before: item.Text, After: item.DueAt.In(now.Location()).Format("2006-01-02")})
		}
		d.Card.Todo.Items = append(d.Card.Todo.Items, item)
	}
	d.Original = chatcmd003Clone(d.Card)
	return d, nil
}
