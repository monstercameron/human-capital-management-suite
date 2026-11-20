package chat

import (
	"testing"
	"time"
)

func TestIssue3984_UnresolvedMentionRecorded(t *testing.T) {
	members := []Chatcmd004Member{{HomeTenantID: "t", ID: "darius", Name: "Darius Bennett"}, {HomeTenantID: "t", ID: "maria", Name: "Maria Lopez"}}
	now := time.Date(2027, 3, 1, 9, 0, 0, 0, time.UTC)
	for _, token := range []string{"@Dariuss", "@Nobody", "@Ma"} {
		d, err := Chatcmd004ParseTodo("Review the budget "+token+" by 2027-03-15", members, "me", now, Chatcmd004ResolveDate)
		if err != nil || len(d.Unresolved) != 1 || d.Unresolved[0].Token != token || d.Unresolved[0].Item != 0 || d.Card.Todo.Items[0].AssigneeID != "" {
			t.Fatalf("%s: %+v %v", token, d, err)
		}
	}
	d, _ := Chatcmd004ParseTodo("Review the budget @Darius by 2027-03-15", members, "me", now, Chatcmd004ResolveDate)
	if len(d.Unresolved) != 0 || d.Card.Todo.Items[0].AssigneeID != "darius" {
		t.Fatalf("resolved mention: %+v", d)
	}
	d, _ = Chatcmd004ParseTodo("Review @Dariuss by Friday; Book room", members, "me", now, Chatcmd004ResolveDate)
	card := d.Card
	if !Chatcmd004AssignUnresolved(&card, d.Unresolved[0], members[0]) || card.Todo.Items[0].Text != "Review" || card.Todo.Items[0].AssigneeID != "darius" {
		t.Fatalf("assign: %+v", card.Todo.Items)
	}
	// A task of the name alone keeps its words.
	alone := Chatcmd002Card{Todo: &Chatcmd002Todo{Items: []Chatcmd002Task{{ChannelTodoItem: ChannelTodoItem{Text: "@Dariuss"}}}}}
	if Chatcmd004AssignUnresolved(&alone, Chatcmd004Unresolved{Token: "@Dariuss"}, members[0]) {
		t.Fatal("emptied the task")
	}
}
