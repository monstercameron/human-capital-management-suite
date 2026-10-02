package chat

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// This file is the tidy step of /poll and /todo that needs no model: the same
// rules run in the composer and on the server, cost nothing and always answer.
// It only does what can be undone by eye: spacing, a capital letter, a question
// mark, a list marker dropped, a repeated poll option merged. Wording is never
// rewritten. The preview lists every change and offers the text as typed.

var (
	chatcmd003ListMarker = regexp.MustCompile(`^(?:[-*•]\s+|\d{1,2}[.)]\s+)`)
	// A separator between loose poll options: a comma, a semicolon, or the word
	// "or" in the three product languages.
	chatcmd003OptionBreak = regexp.MustCompile(`(?i)\s*[,;،]\s*|\s+(?:or|oder|أو)\s+`)
	chatcmd003LeadingWord = regexp.MustCompile(`(?i)^(?:or|and|oder|und|أو|و)\s+`)
)

// chatcmd003TidyLine tidies one line of text without changing its words.
func chatcmd003TidyLine(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	text = strings.TrimSpace(strings.Join(lines, "\n"))
	text = chatcmd003ListMarker.ReplaceAllString(text, "")
	text = strings.TrimRight(text, ",;،")
	return chatcmd003Capitalise(strings.TrimSpace(text))
}

// chatcmd003Capitalise gives the first word a capital letter when the word is
// plain lower case. A mention, a link, a word with its own capitals (iPhone) or
// a script without case is left alone.
func chatcmd003Capitalise(text string) string {
	word, _, _ := strings.Cut(text, " ")
	if word == "" || strings.ContainsAny(word, "@/:.") {
		return text
	}
	letters := 0
	for _, r := range word {
		if unicode.IsLetter(r) {
			letters++
			if !unicode.IsLower(r) {
				return text
			}
		}
	}
	first, size := utf8.DecodeRuneInString(text)
	if letters == 0 || !unicode.IsLetter(first) {
		return text
	}
	return string(unicode.ToUpper(first)) + text[size:]
}

// chatcmd003Question ends a poll's question with a question mark.
func chatcmd003Question(text string) string {
	text = strings.TrimSpace(strings.TrimRight(text, ":"))
	if text == "" {
		return text
	}
	last, _ := utf8.DecodeLastRuneInString(text)
	if last == '?' || last == '؟' || last == '!' {
		return text
	}
	text = strings.TrimRight(text, ".")
	for _, r := range text {
		if unicode.Is(unicode.Arabic, r) {
			return text + "؟"
		}
	}
	return text + "?"
}

// Chatcmd003Tidy returns the draft with its card tidied and every change
// listed. Original keeps the card as it was typed.
func Chatcmd003Tidy(d Chatcmd003Draft) Chatcmd003Draft {
	card := chatcmd003Clone(d.Card)
	// What the reading already reported (a line split into tasks, an assignee
	// or a date taken out of a task) stays listed; the wording follows it.
	var changes []Chatcmd003Change
	for _, change := range d.Changes {
		if change.Kind != "" {
			changes = append(changes, change)
		}
	}
	flat := func(text string) string { return strings.Join(strings.Fields(text), " ") }
	note := func(before, after string) {
		// A change is listed only when a reader would see it: the page draws a
		// run of spaces as one, so spacing alone would be listed as "X → X".
		if flat(before) != flat(after) {
			changes = append(changes, Chatcmd003Change{Before: before, After: after})
		}
	}
	if card.Title != "" {
		title := chatcmd003TidyLine(card.Title)
		if card.Kind == "poll" {
			title = chatcmd003Question(title)
		}
		note(card.Title, title)
		card.Title = title
	}
	if card.Poll != nil {
		seen := map[string]bool{}
		kept := make([]ChannelPollOption, 0, len(card.Poll.Options))
		for _, option := range card.Poll.Options {
			text := chatcmd003TidyLine(option.Text)
			key := strings.ToLower(text)
			if text != "" && seen[key] {
				// A repeated option is merged into the first; After stays empty.
				changes = append(changes, Chatcmd003Change{Before: option.Text})
				continue
			}
			seen[key] = true
			note(option.Text, text)
			option.Text = text
			kept = append(kept, option)
		}
		card.Poll.Options = kept
	}
	if card.Todo != nil {
		for i := range card.Todo.Items {
			typed := card.Todo.Items[i].Text
			text := chatcmd003TidyLine(typed)
			note(typed, text)
			card.Todo.Items[i].Text = text
			// An assignee or a date is listed against the task as the card shows it.
			for j := range changes {
				if changes[j].Kind != "" && changes[j].Kind != Chatcmd004ChangeSplit && changes[j].Before == typed {
					changes[j].Before = text
				}
			}
		}
	}
	d.Card, d.Changes = card, changes
	return d
}

// chatcmd003LooseLines is the text of a loosely typed command, one entry per
// line it was typed on, with settings (multiple=yes) and quoting removed.
func chatcmd003LooseLines(raw string) []string {
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		// A line without a setting is kept exactly as typed, quotes included.
		if arguments, err := Chatcmd003ParseArguments(line); err == nil && len(arguments.Named) > 0 {
			line = arguments.Text
		}
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// chatcmd003LooseOptions splits a run of loosely typed options.
func chatcmd003LooseOptions(text string) []string {
	var options []string
	for _, part := range chatcmd003OptionBreak.Split(text, -1) {
		part = chatcmd003LeadingWord.ReplaceAllString(strings.TrimSpace(part), "")
		part = strings.TrimSpace(strings.TrimRight(part, "?؟."))
		if part != "" {
			options = append(options, part)
		}
	}
	return options
}

// chatcmd003QuestionEnd cuts a line where its question ends: after a question
// mark, or at a colon, when something follows. found is false when the line
// holds neither, or nothing after it.
func chatcmd003QuestionEnd(text string) (question, rest string, found bool) {
	if i := strings.IndexAny(text, "?؟"); i >= 0 {
		_, size := utf8.DecodeRuneInString(text[i:])
		if rest := strings.TrimSpace(text[i+size:]); rest != "" {
			return strings.TrimSpace(text[:i+size]), rest, true
		}
	}
	for i := 0; i < len(text); i++ {
		// A colon inside a link (https://) or a time (10:30) ends no question.
		if text[i] != ':' || strings.HasPrefix(text[i:], "://") || (i > 0 && i+1 < len(text) && text[i-1] >= '0' && text[i-1] <= '9' && text[i+1] >= '0' && text[i+1] <= '9') {
			continue
		}
		if rest := strings.TrimSpace(text[i+1:]); rest != "" && strings.TrimSpace(text[:i]) != "" {
			return strings.TrimSpace(text[:i]), rest, true
		}
	}
	return "", "", false
}

// chatcmd003SplitLoosePoll separates the question of a loosely typed poll from
// its options. Lines, a question mark or a colon say where the question ends.
// Without one the split is a guess (the first option is taken to be as long as
// the second) and guessed is true, so the preview asks the person to check it.
func chatcmd003SplitLoosePoll(raw string) (title string, options []string, guessed bool) {
	lines := chatcmd003LooseLines(raw)
	if len(lines) == 0 {
		return "", nil, false
	}
	text := lines[0]
	if question, rest, found := chatcmd003QuestionEnd(text); found {
		// The lines after the first are options too: "Lunch? tacos, pho" and
		// then "pizza" on its own line.
		return question, append(chatcmd003LooseOptions(rest), lines[1:]...), false
	}
	if len(lines) > 1 {
		return text, lines[1:], false
	}
	parts := chatcmd003LooseOptions(text)
	if len(parts) < 2 {
		return text, nil, true
	}
	first, next := strings.Fields(parts[0]), len(strings.Fields(parts[1]))
	if len(first) <= next {
		// Nothing is left over for a question: these are all options.
		return "", parts, true
	}
	head := len(first) - next
	return strings.Join(first[:head], " "), append([]string{strings.Join(first[head:], " ")}, parts[1:]...), true
}
