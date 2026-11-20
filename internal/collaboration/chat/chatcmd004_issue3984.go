package chat

import "strings"

// Chatcmd004Unresolved is a person-like word in a task (an @ name, or a first
// word that fits two members) that was not turned into an assignee. Item is
// the task's place in the card, Token the word exactly as typed. The preview
// names it as unassigned and offers the conversation's members to pick from;
// nobody is chosen for the author.
type Chatcmd004Unresolved struct {
	Item  int    `json:"item"`
	Token string `json:"token"`
	// Owner is set when the task already has an assignee and Token is a further
	// person named in it (#3952): one task has one owner, so Token is not
	// assigned and the preview says so by name. Empty when the task has no owner.
	Owner string `json:"owner,omitempty"`
}

// chatcmd004UnresolvedToken is the word of text that an assignee lookup
// could not settle: the @ name when there is one, else the first word.
func chatcmd004UnresolvedToken(text string) string {
	if at := strings.Index(text, "@"); at >= 0 {
		end := at + 1
		for end < len(text) && !strings.ContainsRune(" \t\n,;", rune(text[end])) {
			end++
		}
		token := strings.TrimRight(text[at:end], ".:!?")
		if token == "@" {
			return ""
		}
		return token
	}
	word, _, _ := strings.Cut(text, " ")
	return word
}

// Chatcmd004AssignUnresolved takes token out of the text of task item of card
// and gives the task to the member. It reports whether it did: the task must
// still hold the word, and what is left must not be empty.
func Chatcmd004AssignUnresolved(card *Chatcmd002Card, u Chatcmd004Unresolved, m Chatcmd004Member) bool {
	if card == nil || card.Todo == nil || u.Token == "" || u.Item < 0 || u.Item >= len(card.Todo.Items) {
		return false
	}
	item := &card.Todo.Items[u.Item]
	at := strings.Index(item.Text, u.Token)
	if at < 0 {
		return false
	}
	rest := strings.TrimSpace(strings.TrimSpace(item.Text[:at]) + " " + strings.TrimSpace(item.Text[at+len(u.Token):]))
	if rest == "" {
		return false
	}
	item.AssigneeHomeTenantID, item.AssigneeID, item.AssigneeName, item.Text = m.HomeTenantID, m.ID, m.Name, rest
	return true
}
