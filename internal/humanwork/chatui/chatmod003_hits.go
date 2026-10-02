package chatui

import (
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

// CHATMOD-003: what the filters caught. The server has kept every match since
// the filters were written, and a filter on a dry run does nothing else, but
// no page showed them: an administrator switched "Record only for 7 days" on
// and had nowhere to read what was recorded. This section of the filter panel
// lists the matches (when, which filter and version, what happened to the
// message, where) and sums up each filter that is on a dry run.
//
// A match holds a digest of the words and never the words, so there is nothing
// of the message to show, and a match in a conversation the viewer cannot read
// arrives without its author.

// ModHitsProps is the section's data. The page that owns the panel reads the
// matches; nothing is read until the person asks.
type ModHitsProps struct {
	Model Model
	// Workspace is true on the workspace's own panel, which lists every channel.
	Workspace bool
	Records   []chatfilter.Record
	// Loaded is true once a read answered; Loading while one is going. Error is
	// the copy line of a failed read: the matches already shown stay.
	Loaded, Loading bool
	Error           string
	// Older is true when the server has more matches than were read.
	Older bool
	Now   time.Time
	// Load reads the newest matches; with older set it reads the page after the
	// ones shown.
	Load func(older bool)
}

// ModHitsPanel is the "What the filters caught" section.
func ModHitsPanel(props ModHitsProps) ui.Node { return ui.CreateElement(modHitsPanel, props) }

// The three ways to look at the list.
const (
	ModHitsAll   = "all"
	ModHitsActed = "acted"
	ModHitsDry   = "dry"
)

// ModHitsShown filters the matches by the chosen view, newest first.
func ModHitsShown(records []chatfilter.Record, show string) []chatfilter.Record {
	out := make([]chatfilter.Record, 0, len(records))
	for _, r := range records {
		if show == ModHitsActed && r.Hit.DryRun || show == ModHitsDry && !r.Hit.DryRun {
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.After(out[j].At)
		}
		return out[i].ID > out[j].ID
	})
	return out
}

// ModDryLine sums up one filter's dry run: how many matches it recorded and
// what it would have done to them.
type ModDryLine struct {
	RuleID, Name, Action, Target string
	Count                        int
}

// ModDrySummary is one line per filter that recorded a match while on a dry
// run, the busiest first.
func ModDrySummary(records []chatfilter.Record) []ModDryLine {
	byRule := map[string]*ModDryLine{}
	var order []string
	for _, r := range records {
		if !r.Hit.DryRun {
			continue
		}
		line := byRule[r.Hit.RuleID]
		if line == nil {
			line = &ModDryLine{RuleID: r.Hit.RuleID, Name: r.Hit.RuleName, Action: r.Hit.Action, Target: r.Hit.Target}
			byRule[r.Hit.RuleID] = line
			order = append(order, r.Hit.RuleID)
		}
		line.Count++
	}
	out := make([]ModDryLine, 0, len(order))
	for _, id := range order {
		out = append(out, *byRule[id])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// modHitOutcome says what happened to the message, or what would have.
func modHitOutcome(m Model, hit chatfilter.Hit) string {
	key := "h_" + hit.Action
	if hit.DryRun {
		key = "h_dry_" + hit.Action
	}
	text := modadminText(m, key)
	if text == "" {
		// An action this page has no sentence for, from a newer server.
		text = modadminText(m, "h_other")
	}
	return modadminFormat(text, "target", hit.Target)
}

// modHitWhere names the conversation a match was in without printing an
// identifier: its name when the viewer has it, else a plain phrase.
func modHitWhere(m Model, r chatfilter.Record) string {
	if r.Channel == "" {
		return modadminText(m, "hits_outside")
	}
	if name := modChannelName(m, r.Channel); name != "" {
		return "#" + name
	}
	return modadminText(m, "hits_private")
}

func modHitsNumber(m Model, n int) string {
	if m.Number != nil {
		return m.Number(n)
	}
	return chatCount(m.Locale, n)
}

func modHitsPanel(props ModHitsProps) ui.Node {
	m := props.Model
	t := func(key string) string { return modadminText(m, key) }
	show := ui.UseState(ModHitsAll)
	choose := ui.UseEvent(func() {
		if next := domValue("modadmin-hits-show"); next == ModHitsAll || next == ModHitsActed || next == ModHitsDry {
			show.Set(next)
		}
	})
	load := ui.UseEvent(func() {
		if props.Load != nil {
			props.Load(false)
		}
	})
	older := ui.UseEvent(func() {
		if props.Load != nil {
			props.Load(true)
		}
	})
	now := props.Now
	if now.IsZero() {
		now = time.Now()
	}
	hint := t("hits_hint_ch")
	if props.Workspace {
		hint = t("hits_hint_ws")
	}
	nodes := []ui.Node{html.H4(html.Props{ID: "modadmin-hits-title", Text: t("hits_title")}), html.P(html.Props{Class: "chatmod-hint", Text: hint})}
	if props.Error != "" {
		nodes = append(nodes, html.P(html.Props{Role: "alert", Class: "chatmod-error", Text: t(props.Error) + " " + t("hits_failed")}))
	}
	if !props.Loaded {
		label := t("hits_show")
		if props.Loading {
			label = t("hits_loading")
		} else if props.Error != "" {
			label = t("retry")
		}
		nodes = append(nodes, html.Div(html.Props{Class: "chatmod-actions"}, html.Button(html.Props{Type: "button", Text: label, OnClick: load, Disabled: props.Load == nil || props.Loading, Aria: map[string]string{"busy": boolString(props.Loading)}})))
		return html.Section(html.Props{Class: "chatmod-section chatmod-hits", Aria: map[string]string{"labelledby": "modadmin-hits-title"}}, nodes...)
	}
	if dry := ModDrySummary(props.Records); len(dry) > 0 {
		var lines []ui.Node
		for _, line := range dry {
			what := modHitOutcome(m, chatfilter.Hit{Action: line.Action, Target: line.Target, DryRun: true})
			lines = append(lines, html.Li(html.Props{Dir: "auto"},
				html.Span(html.Props{Class: "chatmod-name", Text: line.Name}),
				html.Span(html.Props{Class: "chatmod-state", Text: modadminFormat(t("hits_dry_line"), "n", modHitsNumber(m, line.Count), "what", what)})))
		}
		nodes = append(nodes, html.Div(html.Props{Class: "chatmod-group chatmod-hits-dry"},
			html.H5(html.Props{Text: t("hits_dry_title")}), html.P(html.Props{Class: "chatmod-hint", Text: t("hits_dry_hint")}), html.Ul(html.Props{Class: "chatmod-list"}, lines...)))
	}
	chosen := show.Get()
	options := []ui.Node{}
	for _, key := range []string{ModHitsAll, ModHitsActed, ModHitsDry} {
		options = append(options, html.Option(html.Props{Value: key, Text: t("hits_" + key), Selected: key == chosen}))
	}
	nodes = append(nodes, html.Div(html.Props{Class: "chatmod-field chatmod-hits-show"},
		html.Label(html.Props{For: "modadmin-hits-show", Text: t("hits_show_label")}),
		html.Select(html.Props{ID: "modadmin-hits-show", Class: "chatmod-select", OnChange: choose}, options...)))
	shown := ModHitsShown(props.Records, chosen)
	nodes = append(nodes, html.P(html.Props{Class: "chatmod-state", Role: "status", Text: modadminFormat(t("hits_count"), "n", modHitsNumber(m, len(shown)))}))
	if len(shown) == 0 {
		empty := "hits_none"
		if chosen == ModHitsDry {
			empty = "hits_none_dry"
		}
		nodes = append(nodes, html.P(html.Props{Class: "chatmod-hint", Text: t(empty)}))
	} else {
		head := html.Tr(html.Props{}, html.Th(html.Props{Raw: map[string]any{"scope": "col"}, Text: t("hits_when")}), html.Th(html.Props{Raw: map[string]any{"scope": "col"}, Text: t("hits_filter")}),
			html.Th(html.Props{Raw: map[string]any{"scope": "col"}, Text: t("hits_what")}), html.Th(html.Props{Raw: map[string]any{"scope": "col"}, Text: t("hits_where")}))
		var rows []ui.Node
		for _, r := range shown {
			name := r.Hit.RuleName
			if r.Hit.Version != "" {
				name += " · v" + r.Hit.Version
			}
			rows = append(rows, html.Tr(html.Props{Data: map[string]string{"dry": boolString(r.Hit.DryRun)}},
				html.Td(html.Props{Text: moderationWhen(m.Locale, r.At, time.Local, now)}),
				html.Td(html.Props{Dir: "auto", Text: name}),
				html.Td(html.Props{Dir: "auto", Text: modHitOutcome(m, r.Hit)}),
				html.Td(html.Props{Dir: "auto", Text: modHitWhere(m, r)})))
		}
		nodes = append(nodes, html.Div(html.Props{Class: "chatmod-hits-scroll", TabIndex: html.TabIndexZero, Role: "region", Aria: map[string]string{"labelledby": "modadmin-hits-title"}},
			html.Table(html.Props{Class: "chatmod-hits-table"}, html.Thead(html.Props{}, head), html.Tbody(html.Props{}, rows...))))
	}
	actions := []ui.Node{html.Button(html.Props{Type: "button", Text: t("hits_again"), OnClick: load, Disabled: props.Load == nil || props.Loading})}
	if props.Older {
		actions = append(actions, html.Button(html.Props{Type: "button", Text: t("hits_older"), OnClick: older, Disabled: props.Load == nil || props.Loading}))
	}
	nodes = append(nodes, html.Div(html.Props{Class: "chatmod-actions"}, actions...))
	return html.Section(html.Props{Class: "chatmod-section chatmod-hits", Aria: map[string]string{"labelledby": "modadmin-hits-title", "busy": boolString(props.Loading)}}, nodes...)
}

// chatmod003HitsStyles: a plain table that scrolls inside the panel, which is
// as narrow as the details pane.
const chatmod003HitsStyles = `.chatmod-hits-scroll{max-block-size:320px;overflow:auto;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control)}` +
	`.chatmod-hits-table{inline-size:100%;border-collapse:collapse;font-size:.8125rem}` +
	`.chatmod-hits-table th,.chatmod-hits-table td{padding:6px 8px;text-align:start;vertical-align:top;border-block-end:1px solid var(--hcm-color-border);overflow-wrap:anywhere}` +
	`.chatmod-hits-table th{position:sticky;inset-block-start:0;background:var(--hcm-color-surface);font-weight:650}` +
	`.chatmod-hits-table tr[data-dry="true"] td{color:var(--hcm-color-text-muted)}` +
	`.chatmod-hits-dry li{display:flex;flex-wrap:wrap;gap:4px 8px;padding-block:4px}` +
	`.chatmod-hits-scroll:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}`
