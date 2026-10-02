package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-037: the Agents pages draw loading the way Chat does: the region's own
// frame at once, placeholder rows shaped like the rows that will arrive, the
// status announced once and not printed, and after eight seconds a line that
// says it is taking longer than usual with Try again. The Chat component is
// chatui.ChatLoadingFrame; this one is the same markup and the same classes for
// the pages that are rendered here.

// AgentLoadingShape is the shape of the rows the region will show.
type AgentLoadingShape string

const (
	// AgentLoadingRows is a list of rows: a title bar, a text bar and a status chip.
	AgentLoadingRows AgentLoadingShape = "rows"
	// AgentLoadingCards is a grid of cards: a title bar, two text bars and a button.
	AgentLoadingCards AgentLoadingShape = "cards"
	// AgentLoadingTable is a table: a header row and body rows.
	AgentLoadingTable AgentLoadingShape = "table"
)

// AgentLoadingProps describes one loading region.
type AgentLoadingProps struct {
	Locale LocaleContext
	Shape  AgentLoadingShape
	// Rows is how many placeholder rows or cards to draw; 0 means the usual.
	Rows int
	// Status is the accessible status ("Loading authorized controls"): read out
	// once, never drawn.
	Status string
	// RetryRaw are the attributes of the region's own Try again control (for
	// example data-owner-refresh); nil draws the slow line without a button.
	RetryRaw map[string]any
}

var agentLoadingCopy = map[string][3]string{
	"slow":  {"This is taking longer than usual.", "Das dauert länger als gewöhnlich.", "يستغرق هذا وقتاً أطول من المعتاد."},
	"retry": {"Try again", "Erneut versuchen", "حاول مجدداً"},
}

// AgentLoadingText answers from the table above in en, de and ar.
func AgentLoadingText(locale LocaleContext, key string) string {
	values := agentLoadingCopy[key]
	switch resolved := strings.ToLower(locale.Resolved); {
	case strings.HasPrefix(resolved, "de"):
		return values[1]
	case strings.HasPrefix(resolved, "ar"):
		return values[2]
	}
	return values[0]
}

func agentLoadingBar(class string) ui.Node {
	return html.Span(html.Props{Class: "loading-block chatux037-bar " + class})
}

// AgentLoadingFrame is the placeholder an Agents region draws while it reads.
func AgentLoadingFrame(props AgentLoadingProps) ui.Node {
	shape := props.Shape
	if shape == "" {
		shape = AgentLoadingRows
	}
	count := props.Rows
	if count <= 0 {
		count = 4
		if shape == AgentLoadingCards {
			count = 3
		}
	}
	rows := make([]ui.Node, 0, count+1)
	switch shape {
	case AgentLoadingCards:
		for i := 0; i < count; i++ {
			rows = append(rows, html.Div(html.Props{Class: "chatux037-card"},
				agentLoadingBar("w40 tall"), agentLoadingBar("w90"), agentLoadingBar("w75"), agentLoadingBar("chatux037-action")))
		}
	case AgentLoadingTable:
		rows = append(rows, html.Div(html.Props{Class: "chatux037-row chatux037-table-row head"}, agentLoadingBar("w60"), agentLoadingBar("w60"), agentLoadingBar("w60")))
		for i := 0; i < count; i++ {
			rows = append(rows, html.Div(html.Props{Class: "chatux037-row chatux037-table-row"}, agentLoadingBar("w90"), agentLoadingBar("w75"), agentLoadingBar("chatux037-chip")))
		}
	default:
		for i := 0; i < count; i++ {
			rows = append(rows, html.Div(html.Props{Class: "chatux037-row chatux037-rows-row"},
				agentLoadingBar("chatux037-avatar small"),
				html.Div(html.Props{Class: "chatux037-lines"}, agentLoadingBar("w40"), agentLoadingBar("w75")),
				agentLoadingBar("chatux037-chip")))
		}
	}
	slow := []ui.Node{html.Span(html.Props{}, ui.Text(AgentLoadingText(props.Locale, "slow")))}
	if props.RetryRaw != nil {
		slow = append(slow, html.Button(html.Props{Type: "button", Class: "button secondary chatux037-retry", Raw: props.RetryRaw}, ui.Text(AgentLoadingText(props.Locale, "retry"))))
	}
	children := []ui.Node{}
	if props.Status != "" {
		children = append(children, html.Span(html.Props{Class: "sr-only", Role: "status"}, ui.Text(props.Status)))
	}
	children = append(children,
		html.Div(html.Props{Class: "chatux037-rows chatux037-rows-" + string(shape), Aria: map[string]string{"hidden": "true"}}, rows...),
		html.Div(html.Props{Class: "chatux037-slow", Role: "status"}, slow...))
	return html.Div(html.Props{Class: "chatux037-loading", Dir: string(props.Locale.Direction), Raw: map[string]any{"data-chat-loading": string(shape)}, Aria: map[string]string{"busy": "true"}}, children...)
}

// AgentLoadingStyles holds the layout of the placeholder, the slow line and the
// reveal after eight seconds. The shimmer is the platform's .loading-block rule,
// which uses the motion tokens and stops under reduced motion; the reveal is a
// visibility step, not motion, so it is kept when animation is turned off.
const AgentLoadingStyles = `.chatux037-loading{display:grid;align-content:start;gap:14px;box-sizing:border-box;inline-size:100%;padding-block:8px}` +
	`.chatux037-rows{display:grid;gap:14px}.chatux037-rows-cards{grid-template-columns:repeat(auto-fill,minmax(min(100%,260px),1fr))}` +
	`.chatux037-row{display:grid;gap:12px;align-items:center;min-inline-size:0}` +
	`.chatux037-rows-row{grid-template-columns:32px minmax(0,1fr) 72px}.chatux037-table-row{grid-template-columns:1.3fr 1fr 72px}` +
	`.chatux037-lines{display:grid;gap:8px;min-inline-size:0}` +
	`.chatux037-bar{block-size:10px;border-radius:999px}.chatux037-bar.tall{block-size:14px}` +
	`.chatux037-bar.w40{inline-size:40%}.chatux037-bar.w60{inline-size:60%}.chatux037-bar.w75{inline-size:75%}.chatux037-bar.w90{inline-size:90%}` +
	`.chatux037-bar.chatux037-avatar{inline-size:32px;block-size:32px;border-radius:50%}` +
	`.chatux037-bar.chatux037-chip{inline-size:72px;block-size:24px;border-radius:999px}` +
	`.chatux037-bar.chatux037-action{inline-size:96px;block-size:32px;margin-block-start:4px}` +
	`.chatux037-card{display:grid;gap:10px;padding:16px;border:1px solid var(--control-border,var(--line));border-radius:var(--hcm-radius-surface,8px);background:var(--surface,transparent)}` +
	`.chatux037-slow{display:flex;flex-wrap:wrap;align-items:center;gap:12px;margin:0;color:var(--muted);font-size:.875rem;visibility:hidden;opacity:0;animation:chatux037-reveal 1ms linear 8s forwards}` +
	`:root:root .chatux037-slow.chatux037-slow{animation:chatux037-reveal 1ms linear 8s forwards!important}` +
	`@keyframes chatux037-reveal{to{visibility:visible;opacity:1}}`
