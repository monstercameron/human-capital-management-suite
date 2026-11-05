package productui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Agent012StudioScopeItem is one requested tool or source with its effective
// decision and the explanation naming the deciding policy layers.
type Agent012StudioScopeItem struct {
	ID      string
	Allowed bool
	Reason  string
}

// Agent012StudioStep is one creator step with its completion state.
type Agent012StudioStep struct {
	ID      string
	Done    bool
	Current bool
}

// Agent012StudioSnapshot is the transport-neutral Studio read model. It mirrors
// the canonical Studio status view field for field; the page renders this
// snapshot instead of a separate browser-only agent model.
type Agent012StudioSnapshot struct {
	Available             bool
	AgentID               string
	DisplayName           string
	OwnerID               string
	Revision              uint64
	State                 string
	ManifestDigest        string
	EvaluationDigest      string
	ReviewDigest          string
	ReviewerID            string
	SandboxPresent        bool
	SandboxPassed         bool
	SandboxFresh          bool
	PublicationPresent    bool
	PublicationGeneration uint64
	PublicationVersion    uint64
	Organizations         []string
	Jurisdictions         []string
	DataCategories        []string
	EscalationOwner       string
	SuccessMeasures       string
	SourceIDs             []string
	ChatSpaceIDs          []string
	Audience              string
	Purpose               string
	CapabilityIDs         []string
	TriggerModes          []string
	ChannelIDs            []string
	ReplyVisibility       string
	MaxCostMicros         uint64
	MaxInputTokens        uint64
	MaxOutputTokens       uint64
	MaxConcurrentRuns     uint32
	AutonomyCeiling       string
	ModelPolicyID         string
	ModelPolicyVersion    uint64
	Tools                 []Agent012StudioScopeItem
	Sources               []Agent012StudioScopeItem
	Steps                 []Agent012StudioStep
	CanValidate           bool
	CanSubmit             bool
	CanReview             bool
	CanPublish            bool
	CanTransfer           bool
}

// Agent012StudioPageProps owns the Studio page presentation state. ActionPath
// is the server route that accepts the revision-fenced mutation forms; an
// empty path renders state without actions.
type Agent012StudioPageProps struct {
	I18nProps
	Snapshot   Agent012StudioSnapshot
	ActionPath string
	Navigate   func(string)
}

// Agent012StudioPage renders the Agent Studio draft, validation, evaluation,
// and publication flow from the canonical snapshot. All copy resolves through
// the Studio vocabulary, dynamic values carry dir="auto", and every mutation
// is a native form carrying the expected revision and digest fence.
func Agent012StudioPage(props Agent012StudioPageProps) ui.Node {
	locale := props.Locale
	if !props.Snapshot.Available {
		return html.Div(html.Props{Class: "agent-page-frame", Dir: string(locale.Direction)},
			ui.CreateElement(EmptyState, EmptyStateProps{
				Badge: agent012StudioText(locale, "unavailable.badge"), Tone: "warning",
				Title:       agent012StudioText(locale, "unavailable.title"),
				Description: agent012StudioText(locale, "unavailable.description"),
			}),
		)
	}
	snapshot := props.Snapshot
	text := func(key string) string { return agent012StudioText(locale, key) }
	sections := []ui.Node{
		agent012StudioHeader(locale, snapshot, props.ActionPath),
		agent012StudioSteps(locale, snapshot),
		ui.CreateElement(Panel, PanelProps{Title: text("section.charter"), Body: agent012StudioFacts(locale, []Agent012Fact{
			{labelKey: "charter.orgs", values: snapshot.Organizations},
			{labelKey: "charter.juris", values: snapshot.Jurisdictions},
			{labelKey: "charter.data", values: snapshot.DataCategories},
			{labelKey: "charter.owner", values: []string{snapshot.EscalationOwner}},
			{labelKey: "charter.success", values: []string{snapshot.SuccessMeasures}},
		})}),
		ui.CreateElement(Panel, PanelProps{Title: text("section.context"), Body: agent012StudioFacts(locale, []Agent012Fact{
			{labelKey: "context.sources", values: snapshot.SourceIDs},
			{labelKey: "context.chats", values: snapshot.ChatSpaceIDs},
			{labelKey: "context.audience", values: []string{snapshot.Audience}},
			{labelKey: "context.purpose", values: []string{snapshot.Purpose}},
		})}),
		ui.CreateElement(Panel, PanelProps{Title: text("section.tools"), Body: agent012StudioFacts(locale, []Agent012Fact{
			{labelKey: "tools.requested", values: snapshot.CapabilityIDs},
		})}),
		ui.CreateElement(Panel, PanelProps{Title: text("section.triggers"), Body: agent012StudioFacts(locale, []Agent012Fact{
			{labelKey: "triggers.modes", values: snapshot.TriggerModes},
			{labelKey: "triggers.channels", values: snapshot.ChannelIDs},
			{labelKey: "triggers.reply", values: []string{snapshot.ReplyVisibility}},
		})}),
		ui.CreateElement(Panel, PanelProps{Title: text("section.budgets"), Body: agent012StudioFacts(locale, []Agent012Fact{
			{labelKey: "budgets.cost", values: []string{strconv.FormatUint(snapshot.MaxCostMicros, 10)}},
			{labelKey: "budgets.input", values: []string{strconv.FormatUint(snapshot.MaxInputTokens, 10)}},
			{labelKey: "budgets.output", values: []string{strconv.FormatUint(snapshot.MaxOutputTokens, 10)}},
			{labelKey: "budgets.runs", values: []string{strconv.FormatUint(uint64(snapshot.MaxConcurrentRuns), 10)}},
			{labelKey: "budgets.autonomy", values: []string{snapshot.AutonomyCeiling}},
			{labelKey: "budgets.model", values: []string{agent012ModelPolicy(snapshot)}},
		})}),
		ui.CreateElement(Panel, PanelProps{Title: text("section.authority"), Body: agent012StudioAuthority(locale, snapshot)}),
		ui.CreateElement(Panel, PanelProps{Title: text("section.sandbox"), Body: agent012StudioSandbox(locale, snapshot)}),
		ui.CreateElement(Panel, PanelProps{Title: text("section.review"), Body: agent012StudioReview(locale, props)}),
		ui.CreateElement(Panel, PanelProps{Title: text("section.publish"), Body: agent012StudioPublication(locale, props)}),
	}
	if props.Snapshot.CanTransfer && strings.TrimSpace(props.ActionPath) != "" {
		sections = append(sections, ui.CreateElement(Panel, PanelProps{Title: text("section.transfer"), Body: agent012StudioTransfer(locale, props)}))
	}
	return html.Div(html.Props{Class: "agent-page-frame agent012-studio", Dir: string(locale.Direction), Aria: map[string]string{"labelledby": "agent012-studio-title"}}, sections...)
}

func agent012StudioHeader(locale LocaleContext, snapshot Agent012StudioSnapshot, actionPath string) ui.Node {
	text := func(key string) string { return agent012StudioText(locale, key) }
	stateKey := map[string]string{
		"DRAFT": "state.draft", "VALIDATED": "state.validated",
		"REVIEW_REQUIRED": "state.review", "PUBLISHED": "state.published",
	}[strings.ToUpper(strings.TrimSpace(snapshot.State))]
	if stateKey == "" {
		stateKey = "state.draft"
	}
	return html.Div(html.Props{Class: "agent012-studio-head"},
		html.H1(html.Props{ID: "agent012-studio-title"}, ui.Text(text("title"))),
		html.P(html.Props{Class: "muted"}, ui.Text(text("subtitle"))),
		html.Ul(html.Props{Class: "agent012-studio-identity"},
			html.Li(html.Props{}, html.Strong(html.Props{Raw: map[string]any{"dir": "auto"}}, ui.Text(snapshot.DisplayName))),
			html.Li(html.Props{}, ui.Text(text("owner")+": "), html.Span(html.Props{Raw: map[string]any{"dir": "auto"}}, ui.Text(snapshot.OwnerID))),
			html.Li(html.Props{}, ui.Text(text("revision")+": "+strconv.FormatUint(snapshot.Revision, 10))),
			html.Li(html.Props{}, html.Span(html.Props{Class: "status", Data: map[string]string{"agent-studio-state": snapshot.State}}, ui.Text(text(stateKey)))),
		),
		agent012StudioStateActions(locale, snapshot, actionPath),
	)
}

func agent012StudioStateActions(locale LocaleContext, snapshot Agent012StudioSnapshot, actionPath string) ui.Node {
	if strings.TrimSpace(actionPath) == "" {
		return nil
	}
	var actions []ui.Node
	switch strings.ToUpper(strings.TrimSpace(snapshot.State)) {
	case "DRAFT":
		if snapshot.CanValidate {
			actions = append(actions, agent012StudioFencedForm(locale, actionPath, "validate", snapshot, agent012StudioText(locale, "validate.button"), nil))
		}
	case "VALIDATED":
		if snapshot.CanSubmit {
			actions = append(actions, agent012StudioFencedForm(locale, actionPath, "submit", snapshot, agent012StudioText(locale, "submit.button"), nil))
		}
	}
	if len(actions) == 0 {
		return nil
	}
	return html.Div(html.Props{Class: "action-row"}, actions...)
}

func agent012StudioSteps(locale LocaleContext, snapshot Agent012StudioSnapshot) ui.Node {
	items := make([]ui.Node, 0, len(snapshot.Steps))
	for _, step := range snapshot.Steps {
		status := agent012StudioText(locale, "step.todo")
		props := html.Props{Data: map[string]string{"agent-studio-step": step.ID}}
		if step.Done {
			status = agent012StudioText(locale, "step.done")
		}
		if step.Current {
			status = agent012StudioText(locale, "step.current")
			props.Aria = map[string]string{"current": "step"}
		}
		items = append(items, html.Li(props,
			html.Strong(html.Props{}, ui.Text(agent012StudioText(locale, "step."+step.ID))),
			ui.Text(" · "+status),
		))
	}
	return html.Ol(html.Props{Class: "agent012-studio-steps", Aria: map[string]string{"label": agent012StudioText(locale, "steps_label")}}, items...)
}

// Agent012Fact is one already-ordered label/value fact row.
type Agent012Fact struct {
	labelKey string
	values   []string
}

func agent012StudioFacts(locale LocaleContext, facts []Agent012Fact) ui.Node {
	rows := make([]ui.Node, 0, len(facts))
	for _, fact := range facts {
		rows = append(rows, html.Li(html.Props{},
			html.Strong(html.Props{}, ui.Text(agent012StudioText(locale, fact.labelKey)+": ")),
			html.Span(html.Props{Raw: map[string]any{"dir": "auto"}}, ui.Text(strings.Join(fact.values, ", "))),
		))
	}
	return html.Ul(html.Props{Class: "agent012-studio-facts"}, rows...)
}

func agent012ModelPolicy(snapshot Agent012StudioSnapshot) string {
	if strings.TrimSpace(snapshot.ModelPolicyID) == "" {
		return ""
	}
	return snapshot.ModelPolicyID + " v" + strconv.FormatUint(snapshot.ModelPolicyVersion, 10)
}

func agent012StudioAuthority(locale LocaleContext, snapshot Agent012StudioSnapshot) ui.Node {
	text := func(key string) string { return agent012StudioText(locale, key) }
	tools := agent012ScopeList(locale, text("authority.tools"), snapshot.Tools)
	sources := agent012ScopeList(locale, text("authority.sources"), snapshot.Sources)
	if len(snapshot.Tools) == 0 && len(snapshot.Sources) == 0 {
		return html.P(html.Props{Class: "muted"}, ui.Text(text("authority.none")))
	}
	denied := 0
	for _, item := range append(append([]Agent012StudioScopeItem{}, snapshot.Tools...), snapshot.Sources...) {
		if !item.Allowed {
			denied++
		}
	}
	children := []ui.Node{tools, sources}
	if denied == 0 {
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(text("authority.none_denied"))))
	}
	return html.Div(html.Props{}, children...)
}

func agent012ScopeList(locale LocaleContext, title string, items []Agent012StudioScopeItem) ui.Node {
	if len(items) == 0 {
		return nil
	}
	rows := make([]ui.Node, 0, len(items))
	for _, item := range items {
		verdict := agent012StudioText(locale, "authority.allowed")
		if !item.Allowed {
			verdict = agent012StudioText(locale, "authority.denied")
		}
		rows = append(rows, html.Li(html.Props{Data: map[string]string{"agent-studio-scope": item.ID}},
			html.Strong(html.Props{Raw: map[string]any{"dir": "auto"}}, ui.Text(item.ID)),
			ui.Text(" · "+verdict),
			html.Span(html.Props{Class: "muted", Raw: map[string]any{"dir": "auto"}}, ui.Text(" — "+item.Reason)),
		))
	}
	return html.Div(html.Props{},
		html.H3(html.Props{}, ui.Text(title)),
		html.Ul(html.Props{}, rows...),
	)
}

func agent012StudioSandbox(locale LocaleContext, snapshot Agent012StudioSnapshot) ui.Node {
	text := func(key string) string { return agent012StudioText(locale, key) }
	if !snapshot.SandboxPresent || strings.TrimSpace(snapshot.EvaluationDigest) == "" {
		return html.P(html.Props{Class: "muted"}, ui.Text(text("sandbox.absent")))
	}
	verdict := text("sandbox.not_passing")
	if snapshot.SandboxPassed && snapshot.SandboxFresh {
		verdict = text("sandbox.passed")
	}
	return html.Div(html.Props{},
		html.P(html.Props{}, ui.Text(text("sandbox.digest")+": "), agent012Digest(snapshot.EvaluationDigest)),
		html.P(html.Props{}, html.Span(html.Props{Class: "status"}, ui.Text(verdict))),
	)
}

func agent012StudioReview(locale LocaleContext, props Agent012StudioPageProps) ui.Node {
	text := func(key string) string { return agent012StudioText(locale, key) }
	snapshot := props.Snapshot
	children := []ui.Node{}
	if strings.TrimSpace(snapshot.ReviewDigest) == "" {
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(text("review.absent"))))
	} else {
		children = append(children,
			html.P(html.Props{}, ui.Text(text("review.digest")+": "), agent012Digest(snapshot.ReviewDigest)),
			html.P(html.Props{}, ui.Text(text("review.reviewer")+": "), html.Span(html.Props{Raw: map[string]any{"dir": "auto"}}, ui.Text(snapshot.ReviewerID))),
		)
	}
	if snapshot.CanReview && strings.ToUpper(strings.TrimSpace(snapshot.State)) == "REVIEW_REQUIRED" && strings.TrimSpace(props.ActionPath) != "" {
		children = append(children, agent012StudioFencedForm(locale, props.ActionPath, "review", snapshot, text("review.button"), []ui.Node{
			html.Div(html.Props{Class: "field"},
				html.Label(html.Props{For: "agent012-studio-reviewer"}, ui.Text(text("review.label"))),
				html.Input(html.Props{ID: "agent012-studio-reviewer", Name: "reviewer", Type: "text", Required: true, Raw: map[string]any{"autocomplete": "off"}}),
			),
		}))
	}
	return html.Div(html.Props{}, children...)
}

func agent012StudioPublication(locale LocaleContext, props Agent012StudioPageProps) ui.Node {
	text := func(key string) string { return agent012StudioText(locale, key) }
	snapshot := props.Snapshot
	children := []ui.Node{}
	if !snapshot.PublicationPresent {
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(text("publish.absent"))))
	} else {
		children = append(children, html.Ul(html.Props{Class: "agent012-studio-facts"},
			html.Li(html.Props{}, html.Strong(html.Props{}, ui.Text(text("publish.generation")+": ")), ui.Text(strconv.FormatUint(snapshot.PublicationGeneration, 10))),
			html.Li(html.Props{}, html.Strong(html.Props{}, ui.Text(text("publish.version")+": ")), ui.Text(strconv.FormatUint(snapshot.PublicationVersion, 10))),
		))
	}
	if snapshot.CanPublish && strings.TrimSpace(snapshot.ReviewDigest) != "" && strings.TrimSpace(props.ActionPath) != "" {
		children = append(children,
			html.P(html.Props{Class: "muted"}, ui.Text(text("publish.help"))),
			agent012StudioFencedForm(locale, props.ActionPath, "publish", snapshot, text("publish.button"), []ui.Node{
				html.HiddenInput("expected_generation", strconv.FormatUint(snapshot.PublicationGeneration, 10)),
			}),
		)
	}
	return html.Div(html.Props{}, children...)
}

func agent012StudioTransfer(locale LocaleContext, props Agent012StudioPageProps) ui.Node {
	text := func(key string) string { return agent012StudioText(locale, key) }
	return html.Div(html.Props{},
		html.P(html.Props{Class: "muted"}, ui.Text(text("transfer.help"))),
		agent012StudioFencedForm(locale, props.ActionPath, "transfer", props.Snapshot, text("transfer.button"), []ui.Node{
			html.Div(html.Props{Class: "field"},
				html.Label(html.Props{For: "agent012-studio-new-owner"}, ui.Text(text("transfer.label"))),
				html.Input(html.Props{ID: "agent012-studio-new-owner", Name: "new_owner", Type: "text", Required: true, Raw: map[string]any{"autocomplete": "off"}}),
			),
		}),
	)
}

func agent012StudioFencedForm(locale LocaleContext, actionPath, action string, snapshot Agent012StudioSnapshot, submit string, extra []ui.Node) ui.Node {
	_ = locale
	children := []ui.Node{
		html.HiddenInput("action", action),
		html.HiddenInput("agent_id", snapshot.AgentID),
		html.HiddenInput("expected_revision", strconv.FormatUint(snapshot.Revision, 10)),
		html.HiddenInput("expected_digest", snapshot.ManifestDigest),
	}
	children = append(children, extra...)
	children = append(children, html.Div(html.Props{Class: "action-row"}, html.Button(html.Props{Class: "button primary", Type: "submit"}, ui.Text(submit))))
	return html.Form(html.Props{Class: "agent012-studio-form", Action: actionPath, Method: "post", Data: map[string]string{"agent-studio-action": action}}, children...)
}

func agent012Digest(digest string) ui.Node {
	return html.Code(html.Props{Dir: "ltr", Data: map[string]string{"agent-studio-digest": digest}}, ui.Text(digest))
}
