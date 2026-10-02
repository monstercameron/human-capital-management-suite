package chatui

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/emojiset"
)

// emojiPickerState is everything about the one open emoji picker that is not in
// the DOM: the composer's picker and the reaction picker are the same component
// and never open together. It lives in localUI, like the mention menu's state, so
// a handler always reads its own latest write.
type emojiPickerState struct {
	Open bool
	// Target is the composer's field id, or the message id for a reaction.
	Target   string
	Reaction bool
	// Touch and Sheet are read once when the picker opens: touch uses the larger
	// cells and a phone-width viewport uses a sheet across the bottom.
	Touch, Sheet bool
	Query        string
	// Active is the highlighted position in the sequence (see emojiSeq). The best
	// match is highlighted as the person types, so typing then Enter inserts it.
	Active int
	// Hover is the 1-based position of the emoji under the pointer (0 for none);
	// the footer names it.
	Hover    int
	Scroll   float64
	ViewH    float64
	ToneOpen bool
	// ScrollSet asks the next render to move the grid to Scroll.
	ScrollSet bool
}

func emojiOpenState(target string, reaction, touch, sheet bool) emojiPickerState {
	return emojiPickerState{Open: true, Target: target, Reaction: reaction, Touch: touch, Sheet: sheet, ViewH: emojiDefaultViewH}
}

// emojiStateFor is the state to draw for a picker: the stored one when it is for
// this target, a fresh one otherwise (a reaction picker opens through the
// message model, so its state starts the first time it is drawn).
func emojiStateFor(stored emojiPickerState, target string, reaction bool) emojiPickerState {
	if stored.Open && stored.Target == target && stored.Reaction == reaction {
		return stored
	}
	return emojiOpenState(target, reaction, false, false)
}

// emojiView is the picker's content for one state: the sequence, its rows and
// the data behind them.
type emojiView struct {
	ix      *emojiset.Index
	set     *emojiset.Set
	prefs   emojiPrefs
	seq     emojiSeq
	rows    []emojiRow
	total   float64
	metrics emojiGeometry
}

func buildEmojiView(st emojiPickerState, ix *emojiset.Index, prefs emojiPrefs) emojiView {
	v := emojiView{ix: ix, prefs: prefs, metrics: emojiMetrics(st.Touch || st.Sheet)}
	noFlags := !emojiFlagsDrawn()
	flagsGroup := -1
	if ix != nil {
		v.set = ix.Set
		v.seq.entries = len(ix.Set.Entries)
		// A platform with no flag glyphs gets no Flags group (CHATBUG-043).
		if last, ok := emojiFlagsGroup(ix.Set); ok && noFlags {
			flagsGroup = last
			v.seq.entries = ix.Set.Groups[last].First
			v.seq.skipLast = true
		}
	}
	if q := strings.TrimSpace(st.Query); q != "" {
		v.seq.query = true
		if ix != nil {
			v.seq.results = ix.Search(q, prefs.usageCounts())
			if flagsGroup >= 0 {
				kept := v.seq.results[:0:0]
				for _, i := range v.seq.results {
					if ix.Set.Entries[i].Group != flagsGroup {
						kept = append(kept, i)
					}
				}
				v.seq.results = kept
			}
		}
	} else {
		v.seq.frequent = prefs.frequent()
		if noFlags {
			v.seq.frequent = emojiWithoutFlags(v.seq.frequent)
			if len(v.seq.frequent) == 0 {
				v.seq.frequent = append([]string(nil), emojiStarter...)
			}
		}
	}
	v.rows, v.total = emojiRows(v.seq, v.set, v.metrics)
	return v
}

// emojiCell is one emoji as the picker offers it.
type emojiCell struct {
	Glyph string // in the person's skin tone when it has one
	Entry int    // index in the set, or -1 for a glyph the set does not hold
	Name  string // the reader's language; empty until the data has loaded
	Code  string // ":thumbs_up:"
}

func (v emojiView) cell(pos int) (emojiCell, bool) {
	if pos < 0 || pos >= v.seq.len() {
		return emojiCell{}, false
	}
	cell := emojiCell{Entry: -1}
	switch {
	case v.seq.query:
		cell.Entry = v.seq.results[pos]
	case pos < len(v.seq.frequent):
		cell.Glyph = v.seq.frequent[pos]
		if v.ix != nil {
			if i, ok := v.ix.Lookup(cell.Glyph); ok {
				cell.Entry = i
			}
		}
	default:
		cell.Entry = pos - len(v.seq.frequent)
	}
	if cell.Entry >= 0 && v.set != nil && cell.Glyph == "" {
		cell.Glyph = v.set.Entries[cell.Entry].For(v.prefs.Tone)
	}
	if cell.Entry >= 0 && v.ix != nil {
		cell.Name = v.ix.Name(cell.Entry)
		cell.Code = v.ix.Shortcode(cell.Entry)
	}
	return cell, cell.Glyph != ""
}

// emojiPageRows is how many rows a page key moves.
func emojiPageRows(st emojiPickerState, v emojiView) int {
	viewH := st.ViewH
	if viewH <= 0 {
		viewH = emojiDefaultViewH
	}
	return max(1, int(viewH/v.metrics.Cell)-1)
}

// emojiNavigate moves the highlight for an arrow or page key.
func emojiNavigate(st emojiPickerState, v emojiView, key string, rtl bool) emojiPickerState {
	st.Active = emojiMove(v.rows, v.seq.len(), st.Active, key, rtl, emojiPageRows(st, v))
	st.Hover = 0
	return emojiReveal(st, v)
}

// emojiReveal scrolls the grid, at the next render, so the highlight is in view.
func emojiReveal(st emojiPickerState, v emojiView) emojiPickerState {
	viewH := st.ViewH
	if viewH <= 0 {
		viewH = emojiDefaultViewH
	}
	if at := emojiRowOfPos(v.rows, st.Active); at >= 0 {
		if scroll := emojiScrollFor(v.rows, at, st.Scroll, viewH, v.metrics.Head); scroll != st.Scroll {
			st.Scroll, st.ScrollSet = scroll, true
		}
	}
	return st
}

// emojiQuery is the state after the search field changed: the best match is
// highlighted and the grid starts again from the top.
func emojiQuery(st emojiPickerState, query string) emojiPickerState {
	if st.Query == query {
		return st
	}
	st.Query = query
	st.Active, st.Hover, st.ToneOpen = 0, 0, false
	st.Scroll, st.ScrollSet = 0, true
	return st
}

// emojiCategory is the state after a category tab: the grid scrolls to the
// category and its first emoji is highlighted.
func emojiCategory(st emojiPickerState, v emojiView, group int) emojiPickerState {
	if v.seq.query {
		return st
	}
	head := emojiHeadRow(v.rows, group)
	if head < 0 {
		return st
	}
	st.Scroll, st.ScrollSet = v.rows[head].Y, true
	st.Hover = 0
	if head+1 < len(v.rows) {
		st.Active = v.rows[head+1].First
	}
	return st
}

// emojiScrolled records where the grid was scrolled to. It reports whether the
// set of rows in the page changed, which is the only time a render is needed.
func emojiScrolled(st emojiPickerState, v emojiView, scroll, viewH float64) (emojiPickerState, bool) {
	oldFirst, oldLast := emojiWindow(v.rows, st.Scroll, max(st.ViewH, emojiDefaultViewH), v.metrics)
	next := st
	next.Scroll, next.ViewH = scroll, viewH
	first, last := emojiWindow(v.rows, scroll, max(viewH, emojiDefaultViewH), v.metrics)
	changed := first != oldFirst || last != oldLast
	// The marked tab follows the top row; a change of category is a render too.
	if !changed && !v.seq.query {
		before, after := emojiRowAtScroll(v.rows, st.Scroll), emojiRowAtScroll(v.rows, scroll)
		if before >= 0 && after >= 0 && before < len(v.rows) && after < len(v.rows) && v.rows[before].Group != v.rows[after].Group {
			changed = true
		}
	}
	return next, changed
}

// emojiTone is the state after a skin tone was chosen.
func emojiTone(st emojiPickerState) emojiPickerState {
	st.ToneOpen = false
	return st
}
