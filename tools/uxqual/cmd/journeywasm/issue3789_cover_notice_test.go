package main

import "testing"

func TestIssue3789_CoverNoticesCarryNoTextAndNameTheCoveredPerson(t *testing.T) {
	reply := chatNotificationsReply{Items: []chatNotificationJSON{
		{ID: "c1", Kind: "cover", ConversationID: "room", ConversationName: "ops", PostID: "c1", AuthorID: "dana-id", CoveringID: "bob-id", Body: "private figures"},
		{ID: "c2", Kind: "cover", ConversationID: "room", PostID: "c2", AuthorID: "dana-id", CoveringID: "0123456789abcdef-4567"},
		{ID: "m1", Kind: "mention", ConversationID: "room", PostID: "m1", AuthorID: "dana-id", Body: "look?"},
	}}
	names := map[string]string{"dana-id": "Dana Cole", "bob-id": "Bob Ray", "0123456789abcdef-4567": "0123456789abcdef-4567"}
	items := chatNotificationItems(reply, func(id string) string { return names[id] })
	if len(items) != 3 {
		t.Fatalf("items=%+v", items)
	}
	if a := items[0]; a.Kind != "cover" || a.Body != "" || a.CoveringName != "Bob Ray" || a.CoveringID != "bob-id" || a.AuthorName != "Dana Cole" {
		t.Fatalf("cover = %+v", a)
	}
	if b := items[1]; b.CoveringName != "" {
		t.Fatalf("an identifier was used as a name: %+v", b)
	}
	// Ordinary mentions keep their preview.
	if c := items[2]; c.Body != "look?" || c.CoveringName != "" {
		t.Fatalf("mention = %+v", c)
	}
}
