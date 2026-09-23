package chatui

import (
	"math"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const virtualTimelineThreshold = 160
const virtualTimelineMaxRows = 96
const virtualTimelineOverscan = 8
const virtualNearBottom = 120

type virtualTimelineProps struct {
	model    Model
	handlers handlers
}
type virtualPosition struct {
	room, anchor        string
	offset, top, height float64
	bottom              bool
	revision            int
}
type virtualCache struct {
	room            string
	heights         map[string]float64
	prelude, footer float64
	lastIDs         string
}
type virtualFocus struct {
	rowID, elementID string
	start, end       int
	hasSelection     bool
}
type virtualLayout struct {
	start, end                    int
	before, after, desired, total float64
	active                        bool
}

func estimatedMessageHeight(msg Message, day, unread bool) float64 {
	lines := min(8, 1+len([]rune(msg.Body))/68)
	height := float64(60 + lines*20)
	if len(msg.Attachments) > 0 {
		height += float64(min(2, len(msg.Attachments)) * 170)
	}
	if day {
		height += 42
	}
	if unread {
		height += 32
	}
	return height
}

func messageHeights(m Model, messages []Message, measured map[string]float64) []float64 {
	result := make([]float64, len(messages))
	prevDay := ""
	for i, msg := range messages {
		day := dayKey(msg.SentAt)
		if h := measured[msg.ID]; h > 0 {
			result[i] = h
		} else {
			result[i] = estimatedMessageHeight(msg, day != "" && day != prevDay, msg.ID == m.UnreadFromID)
		}
		prevDay = day
	}
	return result
}

func virtualPrefix(heights []float64) []float64 {
	prefix := make([]float64, len(heights)+1)
	for i, h := range heights {
		prefix[i+1] = prefix[i] + math.Max(1, h)
	}
	return prefix
}

func virtualIndex(prefix []float64, top float64) int {
	lo, hi := 0, len(prefix)-1
	for lo < hi {
		mid := (lo + hi) / 2
		if prefix[mid+1] <= top {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

func virtualWindow(m Model, messages []Message, cache *virtualCache, pos virtualPosition) virtualLayout {
	n := len(messages)
	if n <= virtualTimelineThreshold || m.State != StateReady || m.SelectedID == "" {
		return virtualLayout{end: n}
	}
	heights := messageHeights(m, messages, cache.heights)
	prefix := virtualPrefix(heights)
	viewport := pos.height
	if viewport < 320 {
		viewport = 720
	}
	base := cache.prelude
	if base <= 0 {
		if m.HasOlder {
			base = 42
		} else {
			base = 128
		}
	}
	total := base + prefix[n]
	if m.HasNewer {
		if cache.footer > 0 {
			total += cache.footer
		} else {
			total += 42
		}
	}
	top := pos.top
	if pos.room != m.SelectedID || pos.bottom {
		top = math.Max(0, total-viewport)
	} else if pos.anchor != "" {
		for i, msg := range messages {
			if msg.ID == pos.anchor {
				top = base + prefix[i] + pos.offset
				break
			}
		}
	}
	top = math.Max(0, math.Min(top, math.Max(0, total-viewport)))
	first := virtualIndex(prefix, math.Max(0, top-base))
	last := virtualIndex(prefix, math.Max(0, top-base)+viewport) + 1
	start := max(0, first-virtualTimelineOverscan)
	end := min(n, max(last+virtualTimelineOverscan, start+1))
	if end-start > virtualTimelineMaxRows {
		end = start + virtualTimelineMaxRows
	}
	if pos.room != m.SelectedID || pos.bottom {
		end = n
		start = max(0, min(first-virtualTimelineOverscan, n-1))
		if end-start > virtualTimelineMaxRows {
			start = end - virtualTimelineMaxRows
		}
	}
	return virtualLayout{start: start, end: end, before: prefix[start], after: prefix[n] - prefix[end], desired: top, total: total, active: true}
}

func virtualAnchor(m Model, messages []Message, cache *virtualCache, top, height float64) virtualPosition {
	heights := messageHeights(m, messages, cache.heights)
	prefix := virtualPrefix(heights)
	base := cache.prelude
	if base <= 0 {
		if m.HasOlder {
			base = 42
		} else {
			base = 128
		}
	}
	index := virtualIndex(prefix, math.Max(0, top-base))
	anchor, offset := "", 0.0
	if index < len(messages) {
		anchor = messages[index].ID
		offset = math.Max(0, top-base-prefix[index])
	}
	total := base + prefix[len(messages)]
	if m.HasNewer {
		if cache.footer > 0 {
			total += cache.footer
		} else {
			total += 42
		}
	}
	return virtualPosition{room: m.SelectedID, anchor: anchor, offset: offset, top: top, height: height, bottom: total-top-height < virtualNearBottom}
}

func virtualPositionNeedsRender(old, next virtualPosition) bool {
	return old.room != next.room || old.anchor != next.anchor || old.bottom != next.bottom || math.Abs(old.height-next.height) > 2
}

func virtualTimelineList(props virtualTimelineProps) ui.Node {
	m := props.model
	pos := ui.UseState(virtualPosition{bottom: true})
	positionRef := ui.UseRef(pos.Get())
	cacheRef := ui.UseRef(&virtualCache{room: m.SelectedID, heights: map[string]float64{}})
	cache := cacheRef.Get()
	if cache.room != m.SelectedID {
		cache = &virtualCache{room: m.SelectedID, heights: map[string]float64{}}
		cacheRef.Set(cache)
	}
	messages := chronological(m.Messages)
	// Keep the exact scroll anchor without scheduling a full list render for
	// every pixel of movement. The state copy below only changes when the
	// virtual window can change; this ref is also read when history is prepended.
	effectivePos := positionRef.Get()
	if effectivePos.room == "" {
		effectivePos = pos.Get()
	}
	if jumpPendingForRoom(m.SelectedID) {
		effectivePos.bottom = true
	} else if virtualBottomDisarmed(m.SelectedID) {
		effectivePos.bottom = false
	}
	layout := virtualWindow(m, messages, cache, effectivePos)
	focus := captureVirtualFocus()
	ui.UseLayoutEffect(func() func() {
		return syncVirtualTimeline(m, messages, layout, cache, effectivePos, focus, func(next virtualPosition) {
			positionRef.Set(next)
			pos.Set(next)
		}, func() { pos.Update(func(p virtualPosition) virtualPosition { p.revision++; return p }) })
	})
	onScroll := ui.UseEvent(func(event ui.Event) {
		top, height := virtualScrollGeometry(event)
		if height <= 0 {
			return
		}
		next := virtualAnchor(m, messages, cache, top, height)
		if virtualAwayIntent(event) {
			next.bottom = false
		}
		old := positionRef.Get()
		next.revision = old.revision
		positionRef.Set(next)
		if virtualPositionNeedsRender(old, next) {
			pos.Set(next)
		}
	})
	class := listClass(m)
	var children []ui.Node
	if layout.active {
		class += " virtualized"
		children = virtualTimelineBody(m, props.handlers, messages, layout)
	} else {
		children = timelineBody(m, props.handlers)
	}
	return html.Div(html.Props{Class: class, ID: "chat-main", Role: "log", Data: map[string]string{"chat-anchor": listAnchor(m), "has-newer": boolString(m.HasNewer)}, Aria: map[string]string{"label": m.t(KeyMessagesRegion), "live": "polite", "relevant": "additions"}, OnScroll: onScroll}, children...)
}

func virtualTimelineBody(m Model, h handlers, messages []Message, layout virtualLayout) []ui.Node {
	items := make([]ui.Node, 0, layout.end-layout.start+8)
	if m.HasOlder {
		items = append(items, html.WithKey(html.Div(html.Props{Class: "virtual-prelude", Data: map[string]string{"virtual-prelude": "true"}}, html.Button(html.Props{Class: "button secondary small load-older", Type: "button", Disabled: m.Callbacks.LoadOlder == nil, Data: map[string]string{"action": "load-older"}, Text: m.t(KeyLoadOlder)})), "load-older"))
	} else {
		items = append(items, html.WithKey(html.Div(html.Props{Class: "virtual-prelude", Data: map[string]string{"virtual-prelude": "true"}}, channelIntro(m)), "intro"))
	}
	items = append(items, html.WithKey(html.Div(html.Props{Class: "virtual-spacer", Data: map[string]string{"virtual-spacer": "before"}}), "spacer-before"))
	prevDay := ""
	if layout.start > 0 {
		prevDay = dayKey(messages[layout.start-1].SentAt)
	}
	for i := layout.start; i < layout.end; i++ {
		msg := messages[i]
		items = append(items, html.WithKey(virtualMessageRow(m, h, msg, i, messages, prevDay, false), "virtual:"+msg.ID))
		prevDay = dayKey(msg.SentAt)
	}
	items = append(items, html.WithKey(html.Div(html.Props{Class: "virtual-spacer", Data: map[string]string{"virtual-spacer": "after"}}), "spacer-after"))
	protected := []string{m.EditingID, m.MenuID, m.PickerID, focusedVirtualMessageID()}
	seen := map[string]bool{}
	for _, id := range protected {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		for i, msg := range messages {
			if msg.ID != id || (i >= layout.start && i < layout.end) {
				continue
			}
			prev := ""
			if i > 0 {
				prev = dayKey(messages[i-1].SentAt)
			}
			items = append(items, html.WithKey(virtualMessageRow(m, h, msg, i, messages, prev, true), "virtual:"+id))
			break
		}
		if len(seen) >= 4 {
			break
		}
	}
	if m.HasNewer {
		items = append(items, html.WithKey(html.Div(html.Props{Class: "virtual-footer", Data: map[string]string{"virtual-footer": "true"}}, html.Button(html.Props{Class: "button secondary small load-newer", Type: "button", Disabled: m.Callbacks.LoadNewer == nil, Data: map[string]string{"action": "load-newer"}, Text: m.t(KeyLoadNewer)})), "load-newer"))
	}
	return items
}

func virtualMessageRow(m Model, h handlers, msg Message, index int, messages []Message, prevDay string, parked bool) ui.Node {
	day := dayKey(msg.SentAt)
	children := []ui.Node{}
	if day != "" && day != prevDay {
		children = append(children, html.Div(html.Props{Class: "day-divider", Role: "separator", Aria: map[string]string{"label": dayLabel(m, msg.SentAt)}}, html.Span(html.Props{Text: dayLabel(m, msg.SentAt)})))
	}
	unread := m.UnreadFromID != "" && msg.ID == m.UnreadFromID
	if unread {
		children = append(children, html.Div(html.Props{Class: "unread-divider", Role: "separator", Aria: map[string]string{"label": m.t(KeyNew)}}, html.Span(html.Props{Text: m.t(KeyNew)})))
	}
	continued := !unread && index > 0 && day == prevDay && msg.AuthorID != "" && msg.AuthorID == messages[index-1].AuthorID && !msg.SentAt.IsZero() && msg.SentAt.Sub(messages[index-1].SentAt) < 5*time.Minute && m.EditingID != msg.ID && m.EditingID != messages[index-1].ID
	children = append(children, message(m, h, msg, continued))
	class := "virtual-row"
	if parked {
		class += " virtual-parked"
	}
	return html.Div(html.Props{Class: class, Data: map[string]string{"virtual-row": msg.ID}}, children...)
}

func visibleMessageIDs(m Model, messages []Message, layout virtualLayout) []string {
	ids := make([]string, 0, virtualTimelineMaxRows+len(m.ThreadMessages)+2)
	if viewer := imageViewerPostID(); viewer != "" {
		ids = append(ids, viewer)
	}
	if layout.active {
		for _, msg := range messages[layout.start:layout.end] {
			ids = append(ids, msg.ID)
		}
	} else {
		for _, msg := range messages {
			ids = append(ids, msg.ID)
		}
	}
	if m.ShowThread {
		if m.ThreadParentID != "" {
			ids = append(ids, m.ThreadParentID)
		}
		for _, msg := range m.ThreadMessages {
			ids = append(ids, msg.ID)
		}
	}
	return ids
}

func visibleMessageSignature(ids []string) string { return strings.Join(ids, "\x00") }
