package chat

import "strings"

// chatcmd004OtherMention is the next person named with @ in the text left of a
// task after its assignee was taken out (#3952): the full name of a member when
// one follows the sign, else the word as typed. An @ inside a word (an email
// address) is no mention. It is empty when the task names nobody else.
func chatcmd004OtherMention(text string, members []Chatcmd004Member, owner Chatcmd004Member) string {
	ownerFirst, _, _ := strings.Cut(owner.Name, " ")
	for at := 0; at < len(text); at++ {
		if text[at] != '@' || (at > 0 && !strings.ContainsRune(" \t\n(,;", rune(text[at-1]))) {
			continue
		}
		tail := text[at+1:]
		best := ""
		for _, m := range members {
			if len(m.Name) > len(best) && len(tail) >= len(m.Name) && strings.EqualFold(tail[:len(m.Name)], m.Name) && (len(tail) == len(m.Name) || strings.ContainsRune(" \t\n,;.:!?", rune(tail[len(m.Name)]))) {
				best = tail[:len(m.Name)]
			}
		}
		if best != "" {
			if !strings.EqualFold(best, owner.Name) {
				return "@" + best
			}
			continue
		}
		end := 0
		for end < len(tail) && !strings.ContainsRune(" \t\n,;", rune(tail[end])) {
			end++
		}
		// The owner named again is not another person.
		if token := strings.TrimRight(tail[:end], ".:!?"); token != "" && !strings.EqualFold(token, ownerFirst) && !strings.EqualFold(token, owner.Name) {
			return "@" + token
		}
	}
	return ""
}
