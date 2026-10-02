package chatui

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// The person's own emoji preferences: the skin tone they chose once, and how
// often and how recently they used each emoji. They are one of the person's
// personal Chat preferences: the page model carries them (Model.EmojiPrefs), the
// picker writes them back through Callbacks.SaveEmojiPrefs, and the server keeps
// them with the rest of the sidebar preferences (CHATEMOJI-004).

// emojiStarter is what "Frequently used" shows until the person has used emoji
// of their own.
var emojiStarter = []string{"👍", "❤️", "😂", "🎉", "🙏", "👀", "✅", "🔥"}

// emojiQuickDefaults are the one-click reactions on a message's action bar
// until the person has used three emoji of their own.
var emojiQuickDefaults = []string{"👍", "✅", "👀"}

const (
	emojiFrequentShown = 8
	emojiUsageKept     = 120
	emojiPrefsVersion  = 1
)

type emojiUse struct {
	Glyph string `json:"g"`
	Count int    `json:"n"`
	At    int64  `json:"t"` // a counter that only grows, so recency survives clocks
}

type emojiPrefs struct {
	Tone  int        `json:"tone"` // 0 is the default tone, 1 to 5 the Fitzpatrick tones
	Usage []emojiUse `json:"usage"`
	Seq   int64      `json:"seq"`
}

type emojiPrefsFile struct {
	V int `json:"v"`
	emojiPrefs
}

// bump records one use of glyph and returns the new preferences.
func (p emojiPrefs) bump(glyph string) emojiPrefs {
	glyph = strings.TrimSpace(glyph)
	if glyph == "" {
		return p
	}
	next := emojiPrefs{Tone: p.Tone, Seq: p.Seq + 1, Usage: append([]emojiUse(nil), p.Usage...)}
	found := false
	for i := range next.Usage {
		if next.Usage[i].Glyph == glyph {
			next.Usage[i].Count++
			next.Usage[i].At = next.Seq
			found = true
		}
	}
	if !found {
		next.Usage = append(next.Usage, emojiUse{Glyph: glyph, Count: 1, At: next.Seq})
	}
	if len(next.Usage) > emojiUsageKept {
		sort.SliceStable(next.Usage, func(i, j int) bool { return next.Usage[i].less(next.Usage[j]) })
		next.Usage = next.Usage[:emojiUsageKept]
	}
	return next
}

// less orders by use: most used first, then most recent.
func (u emojiUse) less(o emojiUse) bool {
	if u.Count != o.Count {
		return u.Count > o.Count
	}
	return u.At > o.At
}

// top is the person's n most used emoji, most used first.
func (p emojiPrefs) top(n int) []string {
	ranked := append([]emojiUse(nil), p.Usage...)
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].less(ranked[j]) })
	var out []string
	for _, use := range ranked {
		if len(out) == n {
			break
		}
		out = append(out, use.Glyph)
	}
	return out
}

// frequent is what the "Frequently used" row shows: the person's own most used,
// or the starter row before they have any.
func (p emojiPrefs) frequent() []string {
	if own := p.top(emojiFrequentShown); len(own) > 0 {
		return own
	}
	return append([]string(nil), emojiStarter...)
}

// quickReactions are the three one-click reactions on a message: the person's
// most used, filled up with the defaults until they have three of their own.
func (p emojiPrefs) quickReactions() []string {
	out := p.top(3)
	for _, fallback := range emojiQuickDefaults {
		if len(out) == 3 {
			break
		}
		dup := false
		for _, have := range out {
			dup = dup || have == fallback
		}
		if !dup {
			out = append(out, fallback)
		}
	}
	return out
}

// usageCounts maps each glyph to how often it was chosen, for search ranking.
func (p emojiPrefs) usageCounts() map[string]int {
	if len(p.Usage) == 0 {
		return nil
	}
	counts := make(map[string]int, len(p.Usage))
	for _, use := range p.Usage {
		counts[use.Glyph] = use.Count
	}
	return counts
}

func (p emojiPrefs) encode() string {
	data, err := json.Marshal(emojiPrefsFile{V: emojiPrefsVersion, emojiPrefs: p})
	if err != nil {
		return ""
	}
	return string(data)
}

// decodeEmojiPrefs reads stored preferences and refuses anything that is not
// exactly what encode wrote, so a damaged or foreign value is ignored rather
// than shown.
func decodeEmojiPrefs(raw string) emojiPrefs {
	p, _ := parseEmojiPrefs(raw)
	return p
}

// parseEmojiPrefs is decodeEmojiPrefs that also says whether the value was one
// this code wrote (false: empty, damaged or from another version).
func parseEmojiPrefs(raw string) (emojiPrefs, bool) {
	var file emojiPrefsFile
	if raw == "" || len(raw) > 64<<10 || json.Unmarshal([]byte(raw), &file) != nil || file.V != emojiPrefsVersion {
		return emojiPrefs{}, false
	}
	p := emojiPrefs{Tone: file.Tone, Seq: file.Seq}
	if p.Tone < 0 || p.Tone > 5 {
		p.Tone = 0
	}
	seen := map[string]bool{}
	for _, use := range file.Usage {
		if use.Glyph == "" || len(use.Glyph) > 64 || use.Count < 1 || seen[use.Glyph] || len(p.Usage) >= emojiUsageKept {
			continue
		}
		seen[use.Glyph] = true
		p.Usage = append(p.Usage, use)
		if use.At > p.Seq {
			p.Seq = use.At
		}
	}
	return p, true
}

// emojiPrefsKey names one person in one tenant, so another person signing in on
// the same page never inherits the first one's choices.
func emojiPrefsKey(tenant, principal string) string {
	return tenant + ":" + principal
}

// emojiSaveEvery is how rarely the person's emoji choices are written to the
// server: a burst of reactions or insertions is one write.
const emojiSaveEvery = 5 * time.Second

// emojiSaveGate spaces the writes. The first change after a quiet spell is
// written at once; changes inside the next emojiSaveEvery share one trailing
// write that fires when the interval is up.
type emojiSaveGate struct {
	last    time.Time
	pending bool
}

// request says what to do about a change made at now: write it now, or book one
// trailing write after wait (zero when one is already booked).
func (g *emojiSaveGate) request(now time.Time) (writeNow bool, wait time.Duration) {
	if g.pending {
		return false, 0
	}
	if g.last.IsZero() || now.Sub(g.last) >= emojiSaveEvery {
		g.last = now
		return true, 0
	}
	g.pending = true
	return false, g.last.Add(emojiSaveEvery).Sub(now)
}

// fired records that the trailing write ran at now.
func (g *emojiSaveGate) fired(now time.Time) {
	g.pending = false
	g.last = now
}

// mergeEmojiPrefs is the page's preferences once the server's copy has arrived
// (it was read after the page loaded, or another device changed it). Use only
// grows, so each emoji keeps its larger count and later use; the skin tone is the
// one chosen on this page when it was changed here and not yet saved, else the
// server's.
func mergeEmojiPrefs(server, local emojiPrefs, localDirty bool) emojiPrefs {
	out := emojiPrefs{Tone: server.Tone, Seq: max(server.Seq, local.Seq)}
	if localDirty {
		out.Tone = local.Tone
	}
	index := map[string]int{}
	for _, list := range [][]emojiUse{server.Usage, local.Usage} {
		for _, use := range list {
			if i, ok := index[use.Glyph]; ok {
				out.Usage[i].Count = max(out.Usage[i].Count, use.Count)
				out.Usage[i].At = max(out.Usage[i].At, use.At)
				continue
			}
			index[use.Glyph] = len(out.Usage)
			out.Usage = append(out.Usage, use)
		}
	}
	if len(out.Usage) > emojiUsageKept {
		sort.SliceStable(out.Usage, func(i, j int) bool { return out.Usage[i].less(out.Usage[j]) })
		out.Usage = out.Usage[:emojiUsageKept]
	}
	return out
}

// MergeEmojiPrefs joins two encoded copies of a person's emoji choices, the one
// the server holds and the one being written, keeping the written skin tone and
// every emoji's larger use. The sidebar write uses it when another device wrote
// first, so neither device's recent emoji are lost.
func MergeEmojiPrefs(server, written string) string {
	if server == "" {
		return written
	}
	if written == "" {
		return server
	}
	return mergeEmojiPrefs(decodeEmojiPrefs(server), decodeEmojiPrefs(written), true).encode()
}
