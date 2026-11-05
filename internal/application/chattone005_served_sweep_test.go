package application

import (
	"strings"
	"testing"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

// TestTodo_CHATTONE_005: a reader who may not view a message as written still
// gets it from the list, the stream and search; a direct message reads empty.
// (The list, stream, search, link, saved and forward reads are swept by
// TestChattoneRewordNoOriginalLeak_Integration; this test pins what it does
// not.) Through the served composition with originals hidden: a pinned preview
// carries the stand-in before the rewording is ready and the reworded text
// after; the per-message read reports "not ready" while it is produced; a
// direct conversation is delivered as written with no rewording wanted and
// nothing queued.
func TestTodo_CHATTONE_005(t *testing.T) {
	rig, setting := chattoneY2Rig(t, chattoneRewordReplayFixture(t))
	setting(chatstore.RewordModeOffered, false)
	heated := rig.send("chattone005-heated", w10Heated)
	if _, err := rig.chat.service.PinPost(rig.as("alice"), chatcore.PinPostRequest{Principal: rig.person("alice"), Pin: chatcore.Pin{TenantID: "host", ConversationID: rig.room, PostID: heated.ID}}); err != nil {
		t.Fatal(err)
	}
	pinned := func(subject string) string {
		t.Helper()
		pins, err := rig.chat.core.ListPins(rig.as(subject), chatcore.ListPinsRequest{Principal: rig.person(subject), TenantID: "host", ConversationID: rig.room})
		if err != nil || len(pins) != 1 || pins[0].Post == nil {
			t.Fatalf("the pinned preview: %+v %v", pins, err)
		}
		return pins[0].Post.Body
	}

	// While the rewording is produced: the per-message read says "not ready",
	// and the list and the pinned preview carry a stand-in, never the original
	// and never nothing.
	selection, mark := rig.read("bruno", heated)
	if mark.State != "pending" || mark.CanShowOriginal {
		t.Fatalf("the per-message read while produced: %q %+v", selection.Text, mark)
	}
	wanted := false
	for _, kind := range mark.Wanted {
		wanted = wanted || kind == chatrender.Reword
	}
	if !wanted {
		t.Fatalf("the not-ready mark names no rewording: %+v", mark)
	}
	for id, body := range chattoneY2Bodies(t, rig, "bruno") {
		if body == "" || strings.Contains(body, "idiot") {
			t.Fatalf("the member's list before ready: %s %q", id, body)
		}
	}
	if body := pinned("bruno"); body == "" || strings.Contains(body, "idiot") {
		t.Fatalf("the pinned preview before ready: %q", body)
	}

	// Once ready, the pinned preview carries the reworded text too.
	rig.drain()
	if body := pinned("bruno"); body != w10Reword {
		t.Fatalf("the pinned preview when ready: %q", body)
	}
	if body := pinned("alice"); body != w10Heated {
		t.Fatalf("the writer's pinned preview: %q", body)
	}

	// A direct conversation is delivered as written with nothing queued: no
	// rewording wanted on the mark, no job, no rendering.
	room, err := rig.chat.service.CreateConversation(rig.as("alice"), chatcore.CreateConversationRequest{Principal: rig.person("alice"), TenantID: "host", Kind: chatcore.Direct, Members: []chatcore.MemberRef{{TenantID: "host", SubjectID: "bruno"}}, IdempotencyKey: "chattone005-dm"})
	if err != nil {
		t.Fatal(err)
	}
	dm, err := rig.chat.service.SendPost(rig.as("alice"), chatcore.SendPostRequest{Principal: rig.person("alice"), TenantID: "host", ConversationID: room.ID, Body: w10Heated, IdempotencyKey: "chattone005-dm-heated"})
	if err != nil {
		t.Fatal(err)
	}
	scope := chatstore.RenderingScope{Principal: rig.person("bruno"), Tenant: "host", Conversation: room.ID}
	got, mark, err := rig.chat.renderings.ReadRenderingSelection(rig.as("bruno"), scope, dm.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range mark.Wanted {
		if kind == chatrender.Reword {
			t.Fatalf("a direct message wants a rewording: %+v", mark)
		}
	}
	if got.Text != w10Heated {
		t.Fatalf("a direct message was not delivered as written: %q %+v", got.Text, mark)
	}
	if n := rig.count(`SELECT count(*) FROM chatrender_job WHERE post_id=$1`, dm.ID); n != 0 {
		t.Fatalf("a direct message queued %d rendering jobs", n)
	}
	if n := rig.count(`SELECT count(*) FROM chatrender_rendering WHERE post_id=$1 AND tone='reworded'`, dm.ID); n != 0 {
		t.Fatalf("a direct message produced %d reworded renderings", n)
	}
}
