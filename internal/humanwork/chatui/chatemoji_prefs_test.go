package chatui

import (
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"
)

// emojiPrefsHostClock runs the picker's preference writes against a clock the
// test moves and a timer it fires by hand.
type emojiPrefsHostClock struct {
	now    time.Time
	timers []func()
	waits  []time.Duration
}

func (c *emojiPrefsHostClock) install() {
	chatEmojiHost.clock = func() time.Time { return c.now }
	chatEmojiHost.after = func(wait time.Duration, run func()) {
		c.waits = append(c.waits, wait)
		c.timers = append(c.timers, run)
	}
}

func (c *emojiPrefsHostClock) advance(d time.Duration) { c.now = c.now.Add(d) }

func (c *emojiPrefsHostClock) fire() {
	timers := c.timers
	c.timers = nil
	for _, run := range timers {
		run()
	}
}

func emojiBindModel(model Model) { chatEmojiHost.bind(model, localStore{box: &localUI{}}) }

// A person's skin tone and most-used emoji are stored by the server with their
// other Chat preferences: loaded with the page model, written back at most once
// every few seconds, and shared by every device they use.
func TestTodo_CHATEMOJI_004(t *testing.T) {
	t.Run("the server's copy arrives with the page model", func(t *testing.T) {
		stored := emojiPrefs{}.bump("🏆").bump("🏆").bump("🎮")
		stored.Tone = 4
		m := emojiModel("en-US")
		m.EmojiPrefs = stored.encode()
		withEmojiHost(t, m, func(*localUI) {
			emojiBindModel(m)
			if chatEmojiHost.prefs.Tone != 4 || strings.Join(chatEmojiHost.prefs.frequent(), " ") != "🏆 🎮" {
				t.Fatalf("loaded %+v", chatEmojiHost.prefs)
			}
			if got := strings.Join(chatEmojiQuickReactions(), " "); got != "🏆 🎮 👍" {
				t.Fatalf("quick reactions = %q, want the person's own then the defaults", got)
			}
			// A render of the same copy changes nothing, and a damaged copy is not believed.
			emojiBindModel(m)
			bad := m
			bad.EmojiPrefs = `{"v":9,"tone":2}`
			emojiBindModel(bad)
			if chatEmojiHost.prefs.Tone != 4 {
				t.Fatalf("a damaged copy changed the tone to %d", chatEmojiHost.prefs.Tone)
			}
		})
	})

	t.Run("writes are at most one every few seconds, and none is lost", func(t *testing.T) {
		var written []string
		m := emojiModel("en-US")
		m.Callbacks.SaveEmojiPrefs = func(encoded string) { written = append(written, encoded) }
		withEmojiHost(t, m, func(*localUI) {
			clock := &emojiPrefsHostClock{now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
			emojiBindModel(m)
			clock.install()
			// The first choice after a quiet spell is written at once.
			chatEmojiHost.record("🔥")
			if len(written) != 1 || decodeEmojiPrefs(written[0]).top(1)[0] != "🔥" {
				t.Fatalf("first choice: %v", written)
			}
			// A burst inside the interval is one trailing write, booked once.
			for i := 0; i < 5; i++ {
				clock.advance(500 * time.Millisecond)
				chatEmojiHost.record("🎉")
			}
			chatEmojiToneSet(2)
			if len(written) != 1 || len(clock.timers) != 1 {
				t.Fatalf("burst: %d writes, %d timers, want 1 and 1", len(written), len(clock.timers))
			}
			if want := emojiSaveEvery - 500*time.Millisecond; clock.waits[0] != want {
				t.Fatalf("trailing write waits %v, want %v", clock.waits[0], want)
			}
			clock.advance(clock.waits[0])
			clock.fire()
			if len(written) != 2 {
				t.Fatalf("after the interval: %d writes", len(written))
			}
			last := decodeEmojiPrefs(written[1])
			if last.Tone != 2 || last.top(2)[0] != "🎉" || last.Usage[0].Count+last.Usage[1].Count != 6 {
				t.Fatalf("the trailing write lost choices: %+v", last)
			}
			// Nothing changed, nothing is written.
			clock.advance(time.Minute)
			chatEmojiHost.flushPrefs()
			if len(written) != 2 {
				t.Fatalf("an unchanged copy was written again: %d", len(written))
			}
			// After the interval a new choice is written at once again.
			chatEmojiHost.record("👀")
			if len(written) != 3 {
				t.Fatalf("a choice after a quiet spell: %d writes", len(written))
			}
			// Putting the page away writes what is waiting.
			chatEmojiHost.record("🙏")
			chatEmojiHost.flushPrefs()
			if len(written) != 4 || decodeEmojiPrefs(written[3]).Seq < 5 {
				t.Fatalf("flush on hide: %d writes", len(written))
			}
		})
	})

	t.Run("a copy that arrives later joins what this page already holds", func(t *testing.T) {
		m := emojiModel("en-US")
		m.Callbacks.SaveEmojiPrefs = func(string) {}
		withEmojiHost(t, m, func(*localUI) {
			emojiBindModel(m)
			clock := &emojiPrefsHostClock{now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
			clock.install()
			chatEmojiHost.record("🔥") // written
			clock.advance(time.Second)
			chatEmojiToneSet(5) // waits for the interval: not on the server yet
			// Another device's choices arrive with a render of the model.
			other := emojiPrefs{Tone: 1}.bump("🏆").bump("🏆").bump("🔥").bump("🔥").bump("🔥")
			m.EmojiPrefs = other.encode()
			emojiBindModel(m)
			got := chatEmojiHost.prefs
			if got.Tone != 5 {
				t.Errorf("the tone chosen here was replaced by the server's: %d", got.Tone)
			}
			counts := got.usageCounts()
			if counts["🔥"] != 3 || counts["🏆"] != 2 {
				t.Errorf("merged counts %v, want each emoji's larger count", counts)
			}
			// Without an unsaved change here, the server's tone wins.
			chatEmojiHost.dirty = false
			other.Tone = 2
			m.EmojiPrefs = other.encode()
			emojiBindModel(m)
			if chatEmojiHost.prefs.Tone != 2 {
				t.Errorf("another device's tone was ignored: %d", chatEmojiHost.prefs.Tone)
			}
		})
	})

	t.Run("another person on the same page starts clean, and no callback keeps choices on the page", func(t *testing.T) {
		m := emojiModel("en-US")
		withEmojiHost(t, m, func(*localUI) {
			emojiBindModel(m)
			chatEmojiHost.record("🔥") // no save callback
			if !chatEmojiHost.dirty || chatEmojiHost.prefs.top(1)[0] != "🔥" {
				t.Fatal("without a callback the choice is not held for the page")
			}
			next := m
			next.CurrentUser = "someone-else"
			emojiBindModel(next)
			if len(chatEmojiHost.prefs.Usage) != 0 || chatEmojiHost.dirty {
				t.Fatalf("the next person inherited %+v", chatEmojiHost.prefs)
			}
		})
	})

	t.Run("the gate", func(t *testing.T) {
		var g emojiSaveGate
		start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		if now, _ := g.request(start); !now {
			t.Fatal("the first change waited")
		}
		if now, wait := g.request(start.Add(time.Second)); now || wait != emojiSaveEvery-time.Second {
			t.Fatalf("second change: now=%v wait=%v", now, wait)
		}
		if now, wait := g.request(start.Add(2 * time.Second)); now || wait != 0 {
			t.Fatalf("a change while one is booked: now=%v wait=%v", now, wait)
		}
		g.fired(start.Add(emojiSaveEvery))
		if now, _ := g.request(start.Add(emojiSaveEvery + time.Second)); now {
			t.Fatal("a change one second after a write was written at once")
		}
	})
}

// The picker draws the loaded choices: the skin-tone control shows the tone, the
// Frequently used row is the person's own, and a message's one-click reactions
// are their three most used.
func TestTodo_CHATEMOJI_004_Browser(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		stored := emojiPrefs{}.bump("🏆").bump("🏆").bump("🏆").bump("🎮").bump("🎮").bump("🌳")
		stored.Tone = 3
		m.EmojiPrefs = stored.encode()
		m.PickerID = "question"
		m.Callbacks.OpenPicker = func(string) {}
		m.Callbacks.ReactWith = func(string, string) {}
		withEmojiHost(t, m, func(*localUI) {
			page := chatPolishMarkup(t, Build(m), width, theme)
			quick := emojiNodes(t, page, func(n *xhtml.Node) bool { return n.Data == "button" && emojiHasClass(n, "quick-react") })
			var glyphs []string
			for _, n := range quick {
				glyphs = append(glyphs, chatPolishAttr(n, "data-emoji"))
			}
			if len(glyphs) < 3 || strings.Join(glyphs[:3], " ") != "🏆 🎮 🌳" {
				t.Fatalf("the message bar offers %v, want the person's three most used", glyphs)
			}
			pickers := emojiNodes(t, page, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-emoji-picker") == "reaction" })
			if len(pickers) != 1 {
				t.Fatalf("%d reaction pickers", len(pickers))
			}
			reaction := chatPolishMarkup(t, chatEmojiReactionLayer(m, localUI{}), width, theme)
			cells := emojiCellGlyphs(t, reaction)
			if len(cells) < 3 || strings.Join(cells[:3], " ") != "🏆 🎮 🌳" {
				t.Fatalf("Frequently used starts %v, want the person's own", cells[:min(3, len(cells))])
			}
			tone := emojiNodes(t, reaction, func(n *xhtml.Node) bool { return emojiHasClass(n, "emoji-pop-tone") })
			if len(tone) != 1 || !strings.Contains(emojiText(tone[0]), "🏽") {
				t.Fatalf("the skin-tone control does not show tone 3: %v", tone)
			}
		})
	})
}
