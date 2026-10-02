package application

import (
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// TestTodo_CHATLANG_008: with German chosen and translation on, a message whose
// language was never recorded is detected the first time a reader opens it and
// is then translated; a person's own message is never translated for them, not
// even when another reader's translation of it exists; and a text with no
// language of its own stays as written without a call.
func TestTodo_CHATLANG_008(t *testing.T) {
	rig := newChatlangRig(t)
	rig.enable(chatlang.Workspace{})
	rig.reads("bruno", "de")
	rig.reads("alice", "de") // the writer reads German too

	// The writer's own message: one eager job for the other German reader, none for the writer.
	own := rig.send("own", englishSentence)
	if rig.jobs(own, "de") != 1 {
		t.Fatalf("one translation for German is requested at send, got %d", rig.jobs(own, "de"))
	}
	rig.drain()
	if got, mark := rig.read("bruno", own); mark.State != "ready" || got.Text != "[de] "+englishSentence {
		t.Fatalf("the other German reader has no translation: %+v %+v", got, mark)
	}
	// alice wrote it: the German rendering exists and is not shown to her.
	got, mark := rig.read("alice", own)
	if got.Text != englishSentence || mark.State == "ready" && len(mark.Kinds) > 0 {
		t.Fatalf("a person's own message was translated back to them: %+v %+v", got, mark)
	}
	if mark.State != "original" && mark.State != "ready" {
		t.Fatalf("own message state %q", mark.State)
	}
	for _, kind := range mark.Kinds {
		if kind == chatrender.Translate {
			t.Fatalf("own message marked translated: %+v", mark)
		}
	}
	// Bruno writes a message too. Only alice (the other German reader) is an eager
	// reader of it: one request, none for the writer; reading it himself he has it
	// as written even once the German rendering exists.
	bruno := rig.sendAs("bruno", "bruno-own", "Please review the budget numbers with the team tomorrow.")
	if rig.jobs(bruno, "de") != 1 {
		t.Fatalf("the writer's own message was requested for them as well: %d jobs", rig.jobs(bruno, "de"))
	}
	rig.drain()
	if got, mark := rig.readAs("bruno", bruno); got.Text != "Please review the budget numbers with the team tomorrow." || len(mark.Kinds) != 0 {
		t.Fatalf("bruno's own message was translated for him: %+v %+v", got, mark)
	}
	if got, mark := rig.readAs("alice", bruno); mark.State != "ready" || got.Text != "[de] Please review the budget numbers with the team tomorrow." {
		t.Fatalf("the other German reader has no translation: %+v %+v", got, mark)
	}

	// A message from before detection existed: no recorded language, no job.
	old := rig.send("old", "Could you send the quarterly figures to the whole team today?")
	rig.mustExec(`UPDATE chat_post_revision SET source_language='und',language_confidence=0,language_spans='[]' WHERE post_id=$1`, old.ID)
	rig.mustExec(`DELETE FROM chatrender_job WHERE post_id=$1`, old.ID)
	if rig.source(old) != "und" || rig.jobs(old, "de") != 0 {
		t.Fatalf("the fixture is not an old message: %q %d", rig.source(old), rig.jobs(old, "de"))
	}
	// The first reader who needs it opens it: the language is detected and
	// recorded, a translation is requested, and the worker makes it.
	if _, mark := rig.read("bruno", old); mark.State != "pending" || rig.source(old) != "en" || rig.jobs(old, "de") != 1 {
		t.Fatalf("an old message was skipped: %+v source=%q jobs=%d", mark, rig.source(old), rig.jobs(old, "de"))
	}
	rig.drain()
	if got, mark := rig.read("bruno", old); mark.State != "ready" || got.Text != "[de] Could you send the quarterly figures to the whole team today?" {
		t.Fatalf("the old message was not translated: %+v %+v", got, mark)
	}
	// While it is on its way the selection says so and the page keeps the original it has.
	again := rig.send("old2", "Could you also send the annual figures to the whole team today?")
	rig.mustExec(`UPDATE chat_post_revision SET source_language='und',language_confidence=0,language_spans='[]' WHERE post_id=$1`, again.ID)
	rig.mustExec(`DELETE FROM chatrender_job WHERE post_id=$1`, again.ID)
	got, mark = rig.read("bruno", again)
	if mark.State != "pending" || got.Text != "" || !mark.CanShowOriginal || len(mark.Wanted) != 1 || mark.Wanted[0] != chatrender.Translate {
		t.Fatalf("a translation on its way: %+v %+v", got, mark)
	}

	// A writer's correction stands over detection.
	corrected := rig.send("fixed", "Could you review the spreadsheet with the whole team today?")
	rig.mustExec(`UPDATE chat_post_revision SET source_language='fr',language_corrected=true WHERE post_id=$1`, corrected.ID)
	rig.mustExec(`DELETE FROM chatrender_job WHERE post_id=$1`, corrected.ID)
	rig.read("bruno", corrected)
	if rig.source(corrected) != "fr" {
		t.Fatalf("detection overwrote the writer's correction: %q", rig.source(corrected))
	}

	// A text with no language of its own stays as written: no job, no call.
	rig.drain()
	calls := len(rig.engine.Calls())
	short := rig.send("ok", "ok")
	got, mark = rig.read("bruno", short)
	rig.drain()
	if got.Text != "ok" || rig.jobs(short, "de") != 0 || len(rig.engine.Calls()) != calls || rig.source(short) != "und" {
		t.Fatalf("a text with no language was sent for translation: %+v %+v jobs=%d source=%q", got, mark, rig.jobs(short, "de"), rig.source(short))
	}
}

// sendAs sends a message as another member of the conversation.
func (r *chatlangRig) sendAs(subject, key, body string) chatcore.Post {
	r.t.Helper()
	post, err := r.chat.service.SendPost(r.as(subject), chatcore.SendPostRequest{Principal: r.person(subject), TenantID: "host", ConversationID: r.room, Body: body, IdempotencyKey: key})
	if err != nil {
		r.t.Fatal(err)
	}
	return post
}

func (r *chatlangRig) readAs(subject string, post chatcore.Post) (chatrender.Rendering, chatrender.Mark) {
	return r.read(subject, post)
}

func (r *chatlangRig) mustExec(query string, args ...any) {
	r.t.Helper()
	if _, err := r.db.Exec(query, args...); err != nil {
		r.t.Fatal(err)
	}
}

func (r *chatlangRig) source(post chatcore.Post) string {
	r.t.Helper()
	var source string
	if err := r.db.QueryRow(`SELECT source_language FROM chat_post_revision WHERE post_id=$1 ORDER BY revision DESC LIMIT 1`, post.ID).Scan(&source); err != nil {
		r.t.Fatal(err)
	}
	return source
}

// TestTodo_CHATLANG_008_Structured: with the structured engine, a text whose
// recorded language the model finds to be the reader's own is not shown as a
// translation (nothing to show, one usage line, no retry), and a model that does
// not vouch for the meaning is not shown either.
func TestTodo_CHATLANG_008_Structured(t *testing.T) {
	rig := newChatlangRig(t)
	stack := newChatlangOpenAIStack(t, "host", chatlangFixtureModel)
	var err error
	rig.runtime, err = NewChatlangRuntime(rig.chat.store, rig.governance, stack.engine, ChatlangFilterScreen{Filters: rig.chat.filters}, time.Now, []string{"host"})
	if err != nil {
		t.Fatal(err)
	}
	rig.enable(chatlang.Workspace{})
	rig.reads("bruno", "de")

	// The recorded language says English; the model finds it is German already.
	stack.provider.mu.Lock()
	stack.provider.detected = "de"
	stack.provider.mu.Unlock()
	post := rig.send("same", "Bitte lesen wir den Bericht heute zusammen mit dem Team durch.")
	rig.mustExec(`UPDATE chat_post_revision SET source_language='en',language_corrected=true WHERE post_id=$1`, post.ID)
	rig.read("bruno", post)
	rig.drain()
	got, mark := rig.read("bruno", post)
	if got.Text != "Bitte lesen wir den Bericht heute zusammen mit dem Team durch." || mark.State == "ready" || stack.provider.calls() != 1 {
		t.Fatalf("a text already in the reader's language was shown as a translation: %+v %+v calls=%d", got, mark, stack.provider.calls())
	}
	if n := rig.count(`SELECT count(*) FROM chatlang_usage WHERE post_id=$1 AND outcome='discarded'`, post.ID); n != 1 {
		t.Fatalf("the call was not a usage line: %d", n)
	}

	// A model that does not vouch for the meaning: asked twice, shown never.
	stack.provider.mu.Lock()
	stack.provider.detected, stack.provider.unsure = "", true
	stack.provider.mu.Unlock()
	before := stack.provider.calls()
	doubt := rig.send("doubt", "Please do not approve the payment before the review is finished.")
	rig.read("bruno", doubt)
	rig.drain()
	if got, mark := rig.read("bruno", doubt); got.Text != "Please do not approve the payment before the review is finished." || mark.State == "ready" || rig.count(`SELECT count(*) FROM chatrender_rendering WHERE post_id=$1`, doubt.ID) != 0 || stack.provider.calls() != before+chatlangAttempts {
		t.Fatalf("a translation the model would not vouch for was kept: %+v %+v calls=%d", got, mark, stack.provider.calls()-before)
	}
}
