package application

import (
	"context"
	"strings"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

const (
	w10Heated = "You idiot, the deploy broke prod again. @dana must fix 42 files by 2026-10-01."
	w10Calm   = "Please review the deploy notes before the 14:00 meeting."
	w10Reword = "The deploy broke prod again. @dana must fix 42 files by 2026-10-01."
)

// TestTodo_CHATTONE_003_ServedAssembly drives the reword path as the server
// composes it: the worker built by composeServedChatlang with the recorded
// (fake) rewrite model beside the fixture translation engine, and the surface
// reading the reader's tone from the stored settings. A heated message in a
// channel set to Offered gets a reworded rendering, a calm one costs nothing and
// is untouched, "Members may view as written = No" gives a non-writer only the
// reworded text on every list a member's session can read, and a direct
// conversation is never reworded.
func TestTodo_CHATTONE_003_ServedAssembly(t *testing.T) {
	ctx := context.Background()
	rig := newChatlangRig(t)
	reworder, err := NewChattoneReworder(NewChattoneRewordReplay(chattoneRewordReplayFixture(t)), chattoneAllowAllPolicy{}, nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	env := func(k string) string {
		if k == EnvChatTranslationEngine {
			return "fixture"
		}
		return ""
	}
	runtime, reason, err := composeServedChatlang(ctx, chatlangServeInput{Config: ServeConfig{Profile: ServeProfileLocalDev}, Chat: rig.chat, Env: env, Tenants: []string{"host"}, Reworder: reworder})
	if err != nil || runtime == nil {
		t.Fatalf("composed: %v %q", err, reason)
	}
	rig.runtime = runtime
	setting := func(mode string, view bool) {
		t.Helper()
		if _, err := rig.chat.store.SaveRewordSetting(ctx, chatstore.RewordSetting{Tenant: "host", Mode: mode, MembersMayViewOriginal: view, UpdatedBy: "alice"}); err != nil {
			t.Fatal(err)
		}
	}
	// readUntilReady asks, lets the worker run what was asked, and reads again.
	readUntilReady := func(subject string, post chatcore.Post) (chatrender.Rendering, chatrender.Mark) {
		t.Helper()
		first, mark := rig.read(subject, post)
		if mark.State == "ready" || mark.State == "original" {
			return first, mark
		}
		rig.drain()
		return rig.read(subject, post)
	}

	// Translation is not switched on anywhere: rewording is not a translation.
	setting(chatstore.RewordModeOffered, false)
	heated := rig.send("heated", w10Heated)
	calm := rig.send("calm", w10Calm)

	got, mark := readUntilReady("bruno", heated)
	if got.Text != w10Reword || mark.State != "ready" || mark.CanShowOriginal || mark.CanShowAsWritten {
		t.Fatalf("a member reads a heated message in an Offered channel with originals hidden: %q %+v", got.Text, mark)
	}
	reworded := false
	for _, kind := range mark.Kinds {
		reworded = reworded || kind == chatrender.Reword
	}
	if !reworded || got.Producer.Provider != "openai" || got.Tone != chatrender.Reworded {
		t.Fatalf("the rendering does not say it is reworded: %+v %+v", mark.Kinds, got.Producer)
	}
	// A calm message is untouched and costs no job.
	got, mark = readUntilReady("bruno", calm)
	if got.Text != w10Calm || rig.count(`SELECT count(*) FROM chatrender_job WHERE post_id=$1`, calm.ID) != 0 {
		t.Fatalf("a calm message was reworded or queued: %q %+v", got.Text, mark)
	}
	// With originals hidden, nothing a member can list carries the original.
	listed, err := rig.chat.renderings.ListRenderings(rig.as("bruno"), rig.scope("bruno"), []string{heated.ID, calm.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range listed {
		if strings.Contains(row.Text, "idiot") {
			t.Fatalf("the original reached a member's list: %+v", row)
		}
	}
	found, err := rig.chat.renderings.SearchRenderings(rig.as("bruno"), rig.scope("bruno"), "idiot")
	if err != nil || len(found) != 0 {
		t.Fatalf("a member's search matched the original: %+v %v", found, err)
	}
	// The writer always reads what was written, with both views available.
	got, mark = rig.read("alice", heated)
	if got.Text != w10Heated || !mark.CanShowOriginal {
		t.Fatalf("the writer lost the original: %q %+v", got.Text, mark)
	}

	// Where members may view originals, a reader sees what was written until they
	// choose the reworded view; the choice makes the open conversation switch.
	setting(chatstore.RewordModeOffered, true)
	got, _ = rig.read("carla", heated)
	if got.Text != w10Heated {
		t.Fatalf("a reader who may view originals and chose nothing: %q", got.Text)
	}
	if err := rig.chat.store.SaveReaderChoice(ctx, "host", "carla", "", "reworded"); err != nil {
		t.Fatal(err)
	}
	got, mark = readUntilReady("carla", heated)
	if got.Text != w10Reword || !mark.CanShowOriginal {
		t.Fatalf("the reader's own choice of the reworded view: %q %+v", got.Text, mark)
	}

	// A direct conversation is never reworded for its reader.
	room, err := rig.chat.service.CreateConversation(rig.as("alice"), chatcore.CreateConversationRequest{Principal: rig.person("alice"), TenantID: "host", Kind: chatcore.Direct, Members: []chatcore.MemberRef{{TenantID: "host", SubjectID: "bruno"}}, IdempotencyKey: "w10-dm"})
	if err != nil {
		t.Fatal(err)
	}
	setting(chatstore.RewordModeOn, false)
	dm, err := rig.chat.service.SendPost(rig.as("alice"), chatcore.SendPostRequest{Principal: rig.person("alice"), TenantID: "host", ConversationID: room.ID, Body: w10Heated, IdempotencyKey: "w10-dm-heated"})
	if err != nil {
		t.Fatal(err)
	}
	scope := chatstore.RenderingScope{Principal: rig.person("bruno"), Tenant: "host", Conversation: room.ID}
	got, mark, err = rig.chat.renderings.ReadRenderingSelection(rig.as("bruno"), scope, dm.ID)
	if err != nil {
		t.Fatal(err)
	}
	rig.drain()
	got, mark, err = rig.chat.renderings.ReadRenderingSelection(rig.as("bruno"), scope, dm.ID)
	if err != nil || got.Text != w10Heated || rig.count(`SELECT count(*) FROM chatrender_rendering WHERE post_id=$1 AND tone='reworded'`, dm.ID) != 0 {
		t.Fatalf("a direct message was reworded for its reader: %q %+v %v", got.Text, mark, err)
	}
}
