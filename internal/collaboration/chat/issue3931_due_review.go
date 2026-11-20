package chat

import (
	"regexp"
	"strings"
	"time"
)

// A due date or an owner taken out of a task's words is a guess the author can
// correct (#3931). The reading reports each one (Draft.DueReads, OwnerReads)
// with the choices that are open, and Chatcmd004Review carries the author's
// answers back into a second reading of the same text. Nothing is rewritten:
// declining a reading leaves the words in the task.

// Chatcmd004DueOption is one date a phrase can stand for. Key is "infer" (the
// reading made by default), "today", "a" or "b" (the two readings of a
// qualified weekday), or "none" (no deadline, the words stay in the task).
type Chatcmd004DueOption struct {
	Key string     `json:"key"`
	At  *time.Time `json:"at,omitempty"`
}

// Chatcmd004DueRead is a date phrase of one task and the readings it has. Task
// is the text it was found in. Default is the option that holds until the
// author chooses: "infer" for a plain phrase, "none" for a qualified weekday
// ("next Friday"), which is never settled for them.
type Chatcmd004DueRead struct {
	Item    int                   `json:"item"`
	Phrase  string                `json:"phrase"`
	Task    string                `json:"task"`
	Default string                `json:"default"`
	Options []Chatcmd004DueOption `json:"options"`
}

// Chatcmd004OwnerRead is an assignee taken out of a task. Kept is true when the
// author asked for the name to stay in the task as text.
type Chatcmd004OwnerRead struct {
	Item int    `json:"item"`
	Name string `json:"name"`
	Task string `json:"task"`
	Kept bool   `json:"kept,omitempty"`
}

// Chatcmd004Review is what the author chose about the readings, by the place of
// the task in the list: Due holds an option key, KeepOwner the tasks whose
// name stays as text.
type Chatcmd004Review struct {
	Due       map[int]string
	KeepOwner map[int]bool
}

var (
	chatcmd004DayNames = `monday|tuesday|wednesday|thursday|friday|saturday|sunday|mon|tues?|wed|thu(?:rs?)?|fri|sat|sun|montag|dienstag|mittwoch|donnerstag|freitag|samstag|sonntag|الاثنين|الثلاثاء|الأربعاء|الخميس|الجمعة|السبت|الأحد`
	// "next Friday", "nächsten Freitag", "الجمعة القادمة", with the word that
	// says a date follows when there is one. Groups: 1 lead word, 2 qualifier and
	// day (English or German), 3 day and qualifier (Arabic).
	chatcmd004Qualified = regexp.MustCompile(`(?i)(?:^|\s)(?:(by|due|until|before|on|bis|am|vor|بحلول|قبل)\s+)?(?:(?:next|coming|nächsten?|nächste|kommenden?)\s+(` + chatcmd004DayNames + `)|(` + chatcmd004DayNames + `)\s+(?:القادمة|القادم))[.,!]*$`)
)

// chatcmd004DayName is the English name of the weekday a typed word names, or
// "" when it is not a day.
func chatcmd004DayName(word string) string {
	word = strings.ToLower(strings.TrimRight(strings.TrimSpace(word), ".,!"))
	if name, ok := chatcmd004DayWords[word]; ok {
		word = name
	} else if name, ok := chatcmd004ShortWords[word]; ok {
		word = name
	}
	for _, name := range chatcmd004Weekdays {
		if word == name {
			return name
		}
	}
	return ""
}

// chatcmd004SameWeekday reports whether the phrase names the weekday of now, so
// that its reading (the next one, a week on) is a choice and not the only one.
func chatcmd004SameWeekday(phrase string, now time.Time) bool {
	return chatcmd004DayName(phrase) == chatcmd004Weekdays[now.Weekday()]
}

// chatcmd004ReadDue reads the due date of a task and the choice the author has
// made about it. read is nil when the text names no date.
func chatcmd004ReadDue(text string, now time.Time, resolve func(string, time.Time) (time.Time, error), item int, choice string) (due *time.Time, rest string, unread bool, typed string, read *Chatcmd004DueRead) {
	none := Chatcmd004DueOption{Key: "none"}
	if m := chatcmd004Qualified.FindStringSubmatchIndex(text); m != nil {
		dayWord := ""
		switch {
		case m[4] >= 0:
			dayWord = text[m[4]:m[5]]
		case m[6] >= 0:
			dayWord = text[m[6]:m[7]]
		}
		before := strings.TrimRight(strings.TrimSpace(text[:m[0]]), " ,:-")
		if name := chatcmd004DayName(dayWord); name != "" && before != "" {
			if first, err := resolve(name, now); err == nil {
				second := first.AddDate(0, 0, 7)
				read = &Chatcmd004DueRead{Item: item, Phrase: strings.TrimSpace(text[m[0]:]), Task: text, Default: "none",
					Options: []Chatcmd004DueOption{{Key: "a", At: &first}, {Key: "b", At: &second}, none}}
				switch choice {
				case "a":
					return &first, before, false, name, read
				case "b":
					return &second, before, false, name, read
				}
				return nil, text, false, "", read
			}
		}
	}
	due, rest, unread, typed = chatcmd004Due(text, now, resolve)
	if due == nil {
		return due, rest, unread, typed, nil
	}
	options := []Chatcmd004DueOption{{Key: "infer", At: due}}
	var today *time.Time
	if chatcmd004SameWeekday(typed, now) {
		day := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 0, 0, now.Location())
		today = &day
		options = append(options, Chatcmd004DueOption{Key: "today", At: today})
	}
	read = &Chatcmd004DueRead{Item: item, Phrase: typed, Task: text, Default: "infer", Options: append(options, none)}
	switch {
	case choice == "none":
		return nil, text, false, "", read
	case choice == "today" && today != nil:
		return today, rest, false, typed, read
	}
	return due, rest, unread, typed, read
}

// Chatcmd004ParseTodoReviewed reads raw as Chatcmd004ParseTodo (or its literal
// form) and applies the author's choices about the dates and owners it read.
func Chatcmd004ParseTodoReviewed(raw string, members []Chatcmd004Member, author string, now time.Time, resolveDate func(string, time.Time) (time.Time, error), literal bool, review Chatcmd004Review) (Chatcmd003Draft, error) {
	return chatcmd004ParseTodoReviewed(raw, members, author, now, resolveDate, literal, review)
}
