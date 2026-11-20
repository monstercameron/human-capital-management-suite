package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue #900002: the Drafts, Followed threads, Follow-ups and Scheduled panels
// open directly under their own buttons, one at a time, and the Unread filter
// reads as a toggle in the rail's own row language.

// Panel names for issue900002Exclusive.
const (
	issue900002Drafts    = "drafts"
	issue900002Followed  = "followed"
	issue900002FollowUps = "followups"
	issue900002Scheduled = "scheduled"
)

// issue900002Exclusive opens the named panel and closes the other three, along
// with anything a closed panel was holding half done (a discard waiting for its
// confirmation, a scheduled post being edited).
func issue900002Exclusive(u *localUI, keep string) {
	if keep != issue900002Drafts {
		u.draftsOpen, u.draftsAsk, u.draftsNote = false, "", ""
	}
	if keep != issue900002Followed {
		u.followedOpen = false
	}
	if keep != issue900002FollowUps {
		u.followUpsOpen = false
	}
	if keep != issue900002Scheduled {
		u.scheduledOpen, u.scheduledEdit = false, ""
	}
	switch keep {
	case issue900002Drafts:
		u.draftsOpen = true
	case issue900002Followed:
		u.followedOpen = true
	case issue900002FollowUps:
		u.followUpsOpen = true
	case issue900002Scheduled:
		u.scheduledOpen = true
	}
}

// issue900002Fixed is the block above the conversation list. Each entry is its
// button followed at once by its region, so a panel never lands after another
// row; the closed regions stay in the page as empty hidden elements so every
// aria-controls resolves.
func issue900002Fixed(m Model, h handlers) []ui.Node {
	followed := html.WithKey(html.Section(html.Props{ID: "issue3916-panel", Hidden: true}), "region")
	if h.local.followedOpen {
		followed = html.WithKey(issue3916Panel(m), "region")
	}
	followUps := html.WithKey(html.Section(html.Props{ID: "issue3787-panel", Hidden: true}), "region")
	if h.local.followUpsOpen {
		followUps = html.WithKey(issue3787Panel(m), "region")
	}
	nodes := []ui.Node{
		chatsaveSidebar(m),
		issue3915Sidebar(m, h),
		html.Div(html.Props{Class: "issue900002-entry"}, html.WithKey(issue3916Control(m, h), "button"), followed),
		html.Div(html.Props{Class: "issue900002-entry"}, html.WithKey(issue3787Control(m, h), "button"), followUps),
	}
	if m.Callbacks.LoadScheduled != nil {
		tray := html.WithKey(html.Section(html.Props{ID: issue3788PanelID + "-tray", Hidden: true}), "region")
		if h.local.scheduledOpen {
			tray = html.WithKey(issue3788Tray(m, h.local), "region")
		}
		nodes = append(nodes, html.Div(html.Props{Class: "issue900002-entry"}, html.WithKey(issue3788Control(m, h), "button"), tray))
	}
	return append(nodes, chatmod005Sidebar(m))
}

// Issue900002Styles aligns the four entries with Saved and Moderation, bounds
// each open panel so the conversation list is never pushed off screen, and
// draws the Unread filter as a rail row. Logical properties only.
const Issue900002Styles = `.rail-fixed .issue3916-control,.rail-fixed .issue3787-control,.rail-fixed .issue3788-control{padding:0}` +
	`.rail-fixed .issue3916-panel,.rail-fixed .issue3787-panel,.rail-fixed .issue3788-tray,.rail-fixed .issue3915-panel{margin:.25rem 0 .5rem;box-sizing:border-box;max-block-size:min(40vh,20rem);overflow-y:auto;overscroll-behavior:contain}` +
	`.rail-fixed .issue3915-panel[hidden]{display:none}` +
	`.issue3936-filter{padding:0 8px .25rem}` +
	`.issue3936-toggle{border:0;background:none;color:var(--muted);border-radius:var(--hcm-radius-control);padding:0;padding-inline:12px 8px;gap:10px;min-block-size:32px;font-size:.9375rem}` +
	`.issue3936-toggle:hover{background:var(--soft);color:var(--ink)}` +
	`.issue3936-toggle.on,.issue3936-toggle[aria-pressed="true"]{background:var(--soft);color:var(--ink);font-weight:600;box-shadow:inset 3px 0 0 var(--accent,currentColor)}` +
	`.chat-workspace[dir="rtl"] .issue3936-toggle.on,.chat-workspace[dir="rtl"] .issue3936-toggle[aria-pressed="true"]{box-shadow:inset -3px 0 0 var(--accent,currentColor)}` +
	`.issue3936-toggle:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`@media (max-width:640px){.issue3936-toggle{min-block-size:44px}}` +
	`@media (prefers-reduced-motion:reduce){.rail-fixed .issue3916-panel,.rail-fixed .issue3787-panel,.rail-fixed .issue3788-tray,.rail-fixed .issue3915-panel{scroll-behavior:auto}}`
