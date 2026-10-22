package application

import (
	"context"
	"strings"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATTONE_002_WritersSideServed: the writer is told how others read
// their message, may send it as written only where members may read so, and a
// message sent as written is not reworded for anyone.
func TestTodo_CHATTONE_002_WritersSideServed(t *testing.T) {
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
	runtime, _, err := composeServedChatlang(ctx, chatlangServeInput{Config: ServeConfig{Profile: ServeProfileLocalDev}, Chat: rig.chat, Env: env, Tenants: []string{"host"}, Reworder: reworder})
	if err != nil || runtime == nil {
		t.Fatal(err)
	}
	rig.runtime = runtime
	setting := func(mode string, view bool) {
		t.Helper()
		if _, err := rig.chat.store.SaveRewordSetting(ctx, chatstore.RewordSetting{Tenant: "host", Mode: mode, MembersMayViewOriginal: view, UpdatedBy: "alice"}); err != nil {
			t.Fatal(err)
		}
	}
	othersSee := func(subject string, post chatcore.Post) *chatui.ReaderOthersSee {
		t.Helper()
		see, err := rig.chat.renderings.OthersSee(rig.as(subject), rig.scope(subject), post.ID)
		if err != nil {
			t.Fatal(err)
		}
		return see
	}

	// Offered, and members may read as written: others read the original by default,
	// so the writer has nothing to be told.
	setting(chatstore.RewordModeOffered, true)
	quiet := rig.send("quiet", w10Heated)
	if see := othersSee("alice", quiet); see != nil {
		t.Fatalf("others read the original by default, yet the writer is told %+v", see)
	}
	// On: the writer is told others read it reworded; the wording is made when asked.
	setting(chatstore.RewordModeOn, true)
	post := rig.send("on", w10Heated)
	first := othersSee("alice", post)
	if first == nil || first.State != "pending" || !first.CanSendAsWritten {
		t.Fatalf("the first answer: %+v", first)
	}
	rig.drain()
	ready := othersSee("alice", post)
	if ready == nil || ready.State != "ready" || ready.Text != w10Reword || !ready.CanSendAsWritten {
		t.Fatalf("the second answer: %+v", ready)
	}
	// Nobody else is told anything about someone else's message.
	if see := othersSee("bruno", post); see != nil {
		t.Fatalf("a reader was told how others read someone else's message: %+v", see)
	}
	// The writer reads what they wrote.
	if got, _ := rig.read("alice", post); got.Text != w10Heated {
		t.Fatalf("the writer reads %q", got.Text)
	}
	// Only the writer may send it as written.
	if err := rig.chat.renderings.SendAsWritten(rig.as("bruno"), rig.scope("bruno"), post.ID); err == nil {
		t.Fatal("a reader sent someone else's message as written")
	}
	if err := rig.chat.renderings.SendAsWritten(rig.as("alice"), rig.scope("alice"), post.ID); err != nil {
		t.Fatal(err)
	}
	after := othersSee("alice", post)
	if after == nil || after.State != "written" {
		t.Fatalf("after Send as written: %+v", after)
	}
	if got, mark := rig.read("bruno", post); got.Text != w10Heated || mark.State == "ready" {
		t.Fatalf("a message sent as written was reworded for a reader: %q %+v", got.Text, mark)
	}

	// Where members may not read as written, there is no sending as written.
	setting(chatstore.RewordModeOn, false)
	hidden := rig.send("hidden", w10Heated+" ")
	if see := othersSee("alice", hidden); see == nil || see.CanSendAsWritten {
		t.Fatalf("Send as written is offered where originals are hidden: %+v", see)
	}
	if err := rig.chat.renderings.SendAsWritten(rig.as("alice"), rig.scope("alice"), hidden.ID); err == nil {
		t.Fatal("a message was sent as written where members may not read originals")
	}
}

// TestTodo_CHATLANG_006_Served: the monitoring of each language pair, a reader's
// report of a wrong translation and its resolution by an administrator, and the
// "may be inaccurate" mark of a pair that passed only the lower quality mark.
func TestTodo_CHATLANG_006_Served(t *testing.T) {
	ctx := context.Background()
	rig := newChatlangRig(t)
	rig.enable(chatlang.Workspace{})
	rig.reads("bruno", "de")
	post := rig.send("one", englishSentence)
	rig.drain()
	got, mark := rig.read("bruno", post)
	if mark.State != "ready" {
		t.Fatalf("not translated: %+v", mark)
	}

	// Monitoring: one translation of English into German, counted and timed.
	stats, err := rig.chat.store.ChatlangPairStats(ctx, "host", time.Now().AddDate(0, 0, -1))
	if err != nil || len(stats) != 1 || stats[0].Source != "en" || stats[0].Target != "de" || stats[0].Translations != 1 || stats[0].Failures != 0 {
		t.Fatalf("pair monitoring: %+v %v", stats, err)
	}

	// The mark: a pair that passed only the lower mark is marked, one that passed
	// the higher mark is not.
	selection := chatui.ReaderSelection{Rendering: got, Mark: mark}
	rig.chat.renderings.ExtendReaderSelection(rig.as("bruno"), rig.scope("bruno"), post.ID, &selection)
	if selection.Inaccurate {
		t.Fatal("an unjudged pair is marked inaccurate")
	}
	rig.governance.SetGate([]chatlang.GateResult{{Pair: chatlang.GatePair{Source: "en", Target: "de"}, Cases: 10, Passed: 8, Decision: chatlang.GateOfferMarked}})
	rig.chat.renderings.ExtendReaderSelection(rig.as("bruno"), rig.scope("bruno"), post.ID, &selection)
	if !selection.Inaccurate {
		t.Fatal("a pair that passed only the lower mark is not marked inaccurate")
	}
	rig.governance.SetGate([]chatlang.GateResult{{Pair: chatlang.GatePair{Source: "en", Target: "de"}, Cases: 10, Passed: 10, Decision: chatlang.GateOffer}})
	selection.Inaccurate = false
	rig.chat.renderings.ExtendReaderSelection(rig.as("bruno"), rig.scope("bruno"), post.ID, &selection)
	if selection.Inaccurate {
		t.Fatal("a pair that passed the higher mark is marked inaccurate")
	}

	// A reader's report: the pair and the suggestion reach the administrator, a
	// non-administrator sees nothing of the queue, and only an administrator closes it.
	if err := rig.chat.renderings.ReportRendering(rig.as("bruno"), rig.scope("bruno"), got, "Bitte pruefen Sie das Wort Quartal"); err != nil {
		t.Fatal(err)
	}
	view, err := rig.governance.View(rig.as("alice"), rig.room)
	if err != nil || len(view.Reports) != 1 || view.Reports[0].State != "open" || view.Reports[0].Source != "en" || view.Reports[0].Target != "de" || !strings.Contains(view.Reports[0].Suggestion, "Quartal") {
		t.Fatalf("the administrator's view of the queue: %+v %v", view.Reports, err)
	}
	if view.Reports[0].Message != "" || view.Reports[0].Conversation != "" || len(view.Pairs) != 1 {
		t.Fatalf("a report names its message, or the monitoring is missing: %+v", view)
	}
	if other, err := rig.governance.View(rig.as("carla"), rig.room); err == nil || len(other.Reports) != 0 || len(other.Pairs) != 0 {
		t.Fatalf("a reader read the administration: %+v %v", other, err)
	}
	id := view.Reports[0].ID
	if err := rig.governance.ResolveReport(rig.as("carla"), id, "resolved", ""); err == nil {
		t.Fatal("a reader resolved a report")
	}
	if err := rig.governance.ResolveReport(rig.as("alice"), id, "bogus", ""); err == nil {
		t.Fatal("an unknown resolution was accepted")
	}
	if err := rig.governance.ResolveReport(rig.as("alice"), "no-such-report", "resolved", ""); err == nil {
		t.Fatal("a report that does not exist was resolved")
	}
	if err := rig.governance.ResolveReport(rig.as("alice"), id, "resolved", "Added Quartal to the glossary"); err != nil {
		t.Fatal(err)
	}
	view, err = rig.governance.View(rig.as("alice"), rig.room)
	if err != nil || len(view.Reports) != 1 || view.Reports[0].State != "resolved" || view.Reports[0].ResolvedBy != "alice" || view.Reports[0].Note == "" {
		t.Fatalf("after resolving: %+v %v", view.Reports, err)
	}
}
