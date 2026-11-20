package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// #3926: a relative closing time ("In an hour") is fixed when it is chosen, so
// editing the draft does not move it, and a draft that sat until the time it
// shows is out of date is refreshed and confirmed instead of posted quietly.

func issue3926Fixture(t *testing.T) (Model, localStore, *[]chat.Chatcmd002Card) {
	t.Helper()
	m := chatcmd003UIFixture()
	var sent []chat.Chatcmd002Card
	m.Chatcmd002.Post = func(_, _ string, c chat.Chatcmd002Card, done func(error)) { sent = append(sent, c); done(nil) }
	local := localStore{box: &localUI{}}
	if !chatcmd003Follow(m, local, "chat-composer", "/poll Which time works? Morning or Afternoon") {
		t.Fatal("no preview")
	}
	if !chatcmd003Action(m, local, "chatcmd003-set", "closes", "hour") {
		t.Fatal("closes was not taken")
	}
	return m, local, &sent
}

// issue3926Age moves the closing time the person chose back by d, as if the
// draft had been sitting that long.
func issue3926Age(t *testing.T, m Model, local localStore, d time.Duration) time.Time {
	t.Helper()
	state := local.get().chatcmd003
	pin, err := time.Parse(time.RFC3339Nano, state.Settings["closes-at"])
	if err != nil {
		t.Fatalf("the chosen closing time is not kept: %v", state.Settings)
	}
	aged := pin.Add(-d)
	local.update(func(u *localUI) { u.chatcmd003.Settings["closes-at"] = aged.Format(time.RFC3339Nano) })
	local.update(func(u *localUI) {
		u.chatcmd003 = chatcmd003Draw(m, u.chatcmd003, "poll", u.chatcmd003.Draft.Raw)
	})
	return aged
}

func TestIssue3926_EditingDoesNotMoveARelativeDeadline(t *testing.T) {
	m, local, _ := issue3926Fixture(t)
	aged := issue3926Age(t, m, local, 90*time.Second)
	chatcmd003Follow(m, local, "chat-composer", "/poll Which time works? Morning or Afternoon!")
	got := local.get().chatcmd003.Draft.Card.Poll.ClosesAt
	if got == nil || !got.Equal(aged) {
		t.Fatalf("an edit moved the closing time from %v to %v", aged, got)
	}
	if shown := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003)); !strings.Contains(shown, "Closes "+chatcmd002Stamp(m, aged)) {
		t.Fatalf("the preview does not show the chosen time %s: %s", chatcmd002Stamp(m, aged), shown)
	}
}

func TestIssue3926_PostSendsTheTimeThePreviewShows(t *testing.T) {
	m, local, sent := issue3926Fixture(t)
	shown := *local.get().chatcmd003.Draft.Card.Poll.ClosesAt
	chatcmd003Post(m, local)
	if len(*sent) != 1 || (*sent)[0].Poll.ClosesAt == nil || !(*sent)[0].Poll.ClosesAt.Equal(shown) {
		t.Fatalf("sent %+v, the preview showed %v", *sent, shown)
	}
}

func TestIssue3926_StaleDeadlineIsRefreshedAndConfirmedNotPostedQuietly(t *testing.T) {
	m, local, sent := issue3926Fixture(t)
	issue3926Age(t, m, local, 40*time.Minute)
	stale := *local.get().chatcmd003.Draft.Card.Poll.ClosesAt
	chatcmd003Post(m, local)
	if len(*sent) != 0 {
		t.Fatalf("a closing time 40 minutes out of date was posted: %+v", *sent)
	}
	state := local.get().chatcmd003
	fresh := state.Draft.Card.Poll.ClosesAt
	if !state.Open || fresh == nil || !fresh.After(stale.Add(30*time.Minute)) {
		t.Fatalf("the preview was not refreshed: open %v closes %v (was %v)", state.Open, fresh, stale)
	}
	shown := renderNode(t, chatcmd003PreviewView(m, state))
	if !strings.Contains(shown, `id="chatcmd003-error"`) || !strings.Contains(shown, "closing time was updated") || !strings.Contains(shown, "Closes "+chatcmd002Stamp(m, *fresh)) {
		t.Fatalf("the person is not told the time moved: %s", shown)
	}
	// Pressing Post again sends exactly what is now shown.
	chatcmd003Post(m, local)
	if len(*sent) != 1 || !(*sent)[0].Poll.ClosesAt.Equal(*fresh) {
		t.Fatalf("second press sent %+v, shown %v", *sent, fresh)
	}
}

func TestIssue3926_DatePresetThatRolledOverIsConfirmed(t *testing.T) {
	m, local, sent := issue3926Fixture(t)
	chatcmd003Action(m, local, "chatcmd003-set", "closes", "today")
	issue3926Age(t, m, local, 24*time.Hour)
	chatcmd003Post(m, local)
	if len(*sent) != 0 {
		t.Fatalf("an End of day that moved to another day was posted quietly: %+v", *sent)
	}
	end := local.get().chatcmd003.Draft.Card.Poll.ClosesAt
	now := time.Now().In(chatcmd003Location(m))
	if end == nil || end.In(now.Location()).Format("2006-01-02") != now.Format("2006-01-02") {
		t.Fatalf("End of day was not refreshed to today: %v", end)
	}
}

func TestIssue3926_UpdatedNoticeCopyInThreeLanguages(t *testing.T) {
	copy, ok := chatcmd003Copy["closes-moved"]
	if !ok || copy[0] == "" || copy[1] == "" || copy[2] == "" || copy[0] == copy[1] || copy[0] == copy[2] {
		t.Fatalf("copy %v", copy)
	}
}
