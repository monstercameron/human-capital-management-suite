package chatui

import (
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/emojiset"
)

// The picker's grid is laid out in plain numbers so that only the rows near the
// viewport need to be in the page. Every row has a fixed height, so where a row
// sits, which rows are visible and how far to scroll to reveal one are sums, not
// measurements.

const (
	emojiCols          = 8
	emojiCellDesktop   = 36.0 // px
	emojiCellTouch     = 44.0
	emojiHeadDesktop   = 28.0
	emojiHeadTouch     = 32.0
	emojiOverscanRows  = 3
	emojiDefaultViewH  = 252.0 // the grid's height before it has reported its own
	emojiGroupFrequent = -1    // the "Frequently used" section
	emojiGroupResults  = -2    // search results: no sections
)

// emojiSeq is the sequence of emoji a person can move through, in reading order.
// In browse mode it is the frequently used emoji followed by every emoji in
// Unicode's order; while searching it is the results, best first.
type emojiSeq struct {
	frequent []string
	results  []int
	query    bool
	entries  int // emoji in the set (0 until the data has loaded)
	// skipLast leaves the set's last group out (the Flags group, on a platform with
	// no flag glyphs); entries then ends where that group starts.
	skipLast bool
}

func (s emojiSeq) len() int {
	if s.query {
		return len(s.results)
	}
	return len(s.frequent) + s.entries
}

// emojiRow is one row of the grid: a heading or up to emojiCols emoji.
type emojiRow struct {
	Group int  // a group index, emojiGroupFrequent or emojiGroupResults
	Head  bool // a heading row (no cells)
	First int  // position in the sequence of the row's first emoji
	Count int
	Y, H  float64
}

type emojiGeometry struct{ Cell, Head float64 }

func emojiMetrics(touch bool) emojiGeometry {
	if touch {
		return emojiGeometry{emojiCellTouch, emojiHeadTouch}
	}
	return emojiGeometry{emojiCellDesktop, emojiHeadDesktop}
}

// emojiRows lays the sequence out. set is nil until the emoji data has loaded;
// then only the frequently used row is shown.
func emojiRows(seq emojiSeq, set *emojiset.Set, m emojiGeometry) ([]emojiRow, float64) {
	var rows []emojiRow
	y := 0.0
	add := func(row emojiRow) {
		row.Y = y
		if row.Head {
			row.H = m.Head
		} else {
			row.H = m.Cell
		}
		y += row.H
		rows = append(rows, row)
	}
	section := func(group, first, count int, heading bool) {
		if count == 0 {
			return
		}
		if heading {
			add(emojiRow{Group: group, Head: true, First: first})
		}
		for from := 0; from < count; from += emojiCols {
			add(emojiRow{Group: group, First: first + from, Count: min(emojiCols, count-from)})
		}
	}
	if seq.query {
		section(emojiGroupResults, 0, len(seq.results), false)
		return rows, y
	}
	section(emojiGroupFrequent, 0, len(seq.frequent), true)
	if set != nil {
		for gi, group := range set.Groups {
			if seq.skipLast && gi == len(set.Groups)-1 {
				break
			}
			section(gi, len(seq.frequent)+group.First, group.Count, true)
		}
	}
	return rows, y
}

// emojiWindow is the inclusive range of rows to put in the page for a grid
// scrolled to scroll with a viewport viewH tall: those showing, plus a few rows
// either side so a quick scroll never shows blank space.
func emojiWindow(rows []emojiRow, scroll, viewH float64, m emojiGeometry) (first, last int) {
	if len(rows) == 0 {
		return 0, -1
	}
	margin := m.Cell * emojiOverscanRows
	top, bottom := scroll-margin, scroll+viewH+margin
	first, last = len(rows), -1
	for i, row := range rows {
		if row.Y+row.H < top {
			continue
		}
		if row.Y > bottom {
			break
		}
		if i < first {
			first = i
		}
		last = i
	}
	if last < 0 {
		return 0, -1
	}
	return first, last
}

// emojiRowOfPos finds the row holding sequence position pos.
func emojiRowOfPos(rows []emojiRow, pos int) int {
	for i, row := range rows {
		if !row.Head && pos >= row.First && pos < row.First+row.Count {
			return i
		}
	}
	return -1
}

// emojiMove is where the highlight goes for an arrow or page key. Left and right
// follow the reading direction, so they are swapped in a right-to-left layout;
// up and down keep the column, or the end of a short last row.
func emojiMove(rows []emojiRow, total, active int, key string, rtl bool, pageRows int) int {
	if total == 0 {
		return 0
	}
	active = max(0, min(active, total-1))
	switch key {
	case "ArrowRight", "ArrowLeft":
		step := 1
		if (key == "ArrowLeft") != rtl {
			step = -1
		}
		return max(0, min(active+step, total-1))
	case "ArrowDown", "ArrowUp", "PageDown", "PageUp":
		step := 1
		if key == "ArrowUp" || key == "PageUp" {
			step = -1
		}
		if key == "PageDown" || key == "PageUp" {
			step *= max(1, pageRows)
		}
		at := emojiRowOfPos(rows, active)
		if at < 0 {
			return active
		}
		col := active - rows[at].First
		// Walk over heading rows to the next emoji row, as many as step asks.
		target := at
		remaining := step
		for remaining != 0 {
			dir := 1
			if remaining < 0 {
				dir = -1
			}
			next := target + dir
			for next >= 0 && next < len(rows) && rows[next].Head {
				next += dir
			}
			if next < 0 || next >= len(rows) {
				break
			}
			target = next
			remaining -= dir
		}
		if target == at {
			if step < 0 && at >= 0 {
				return rows[at].First
			}
			if step > 0 {
				return rows[at].First + rows[at].Count - 1
			}
			return active
		}
		return rows[target].First + min(col, rows[target].Count-1)
	}
	return active
}

// emojiScrollFor is the scroll position that reveals row i. The sticky heading
// covers the top stickyH pixels of the grid, so a row is revealed below it, or
// with its own section heading when it is the first row under one.
func emojiScrollFor(rows []emojiRow, i int, scroll, viewH, stickyH float64) float64 {
	if i < 0 || i >= len(rows) {
		return scroll
	}
	top, bottom := rows[i].Y-stickyH, rows[i].Y+rows[i].H
	if i > 0 && rows[i-1].Head {
		top = rows[i-1].Y
	}
	if top < scroll {
		return max(0, top)
	}
	if bottom > scroll+viewH {
		return bottom - viewH
	}
	return scroll
}

// emojiRowAtScroll is the row at the top of the grid at this scroll position.
func emojiRowAtScroll(rows []emojiRow, scroll float64) int {
	for i, row := range rows {
		if row.Y+row.H > scroll {
			return i
		}
	}
	return len(rows) - 1
}

// emojiHeadRow finds the heading row of a group, or -1.
func emojiHeadRow(rows []emojiRow, group int) int {
	for i, row := range rows {
		if row.Head && row.Group == group {
			return i
		}
	}
	return -1
}
