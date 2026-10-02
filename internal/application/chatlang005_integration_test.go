package application

import (
	"errors"
	"testing"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

// TestTodo_CHATLANG_005_Integration: a search by "written in" finds messages by
// their detected source language over the real store, only in conversations the
// searcher may read, and translation changes none of that: the language of a
// message is its original's, whatever renderings exist.
func TestTodo_CHATLANG_005_Integration(t *testing.T) {
	rig := newChatlangRig(t)
	rig.enable(chatlang.Workspace{})
	rig.reads("bruno", "de")
	english := rig.send("en", englishSentence)
	german := rig.send("de", "Bitte lesen wir den Bericht heute zusammen mit dem Team durch.")
	rig.drain()

	found := func(subject, language string) (map[string]bool, error) {
		rows, err := rig.chat.renderings.SearchMessageLanguages(rig.as(subject), rig.scope(subject), language)
		ids := map[string]bool{}
		for _, row := range rows {
			ids[row.Message] = true
		}
		return ids, err
	}
	if ids, err := found("bruno", "en"); err != nil || !ids[english.ID] || ids[german.ID] {
		t.Fatalf("written in English: %v %v", ids, err)
	}
	if ids, err := found("bruno", "de"); err != nil || !ids[german.ID] || ids[english.ID] {
		t.Fatalf("written in German: %v %v", ids, err)
	}
	// A translation into German does not make the English message German.
	if ids, _ := found("bruno", "de"); ids[english.ID] {
		t.Fatal("a translation changed the language of the original")
	}

	// A private conversation the searcher is not in finds nothing and says so.
	private, err := rig.chat.service.CreateConversation(rig.as("alice"), chatcore.CreateConversationRequest{Principal: rig.person("alice"), TenantID: "host", Kind: chatcore.PrivateChannel, Name: "closed", IdempotencyKey: "chatlang005-private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rig.chat.service.SendPost(rig.as("alice"), chatcore.SendPostRequest{Principal: rig.person("alice"), TenantID: "host", ConversationID: private.ID, Body: englishSentence + " Closed.", IdempotencyKey: "chatlang005-private-post"}); err != nil {
		t.Fatal(err)
	}
	scope := chatstore.RenderingScope{Principal: rig.person("bruno"), Tenant: "host", Conversation: private.ID}
	if _, err := rig.chat.renderings.SearchMessageLanguages(rig.as("bruno"), scope, "en"); !errors.Is(err, chatcore.ErrPermissionDenied) && !errors.Is(err, chatcore.ErrNotFound) {
		t.Fatalf("a non-member searched a private conversation: %v", err)
	}
	// The language the agent is told is the reader's.
	if got := chatlang.ReaderLanguageInstruction("de"); got == "" {
		t.Fatal("no instruction for a reader of German")
	}
}
