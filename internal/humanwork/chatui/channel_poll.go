package chatui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func pollPercent(count, total int) int {
	if count <= 0 || total <= 0 {
		return 0
	}
	percent := (count*100 + total/2) / total
	if percent > 100 {
		return 100
	}
	return percent
}

func channelPollSection(m Model, h handlers) ui.Node {
	c := m.selected()
	if c.Kind != PublicChannel && c.Kind != PrivateChannel {
		return html.Span(html.Props{})
	}
	children := []ui.Node{html.Div(html.Props{Class: "details-section-head"}, html.H3(html.Props{Text: m.t(KeyPollTitle)}))}
	if m.ChannelPollLoading {
		children = append(children, html.P(html.Props{Role: "status", Aria: map[string]string{"live": "polite"}, Text: m.t(KeyPollLoading)}))
	}
	if m.ChannelPollError != "" {
		children = append(children, html.P(html.Props{Role: "alert", Text: m.t(KeyPollError)}), actionButton("button secondary small", "poll-retry", "", m.t(KeyRetry), m.Callbacks.RetryChannelPoll == nil || m.ChannelPollPending, ui.Text(m.t(KeyRetry))))
		return html.Section(html.Props{ID: "chat-poll-section", Class: "details-section channel-poll", TabIndex: -1, Data: map[string]string{"loading": boolString(m.ChannelPollLoading)}, Aria: map[string]string{"label": m.t(KeyPollTitle)}}, children...)
	}
	poll := m.ChannelPoll
	if poll.Question == "" {
		// Round 3 C-10: Create stays disabled until the form can succeed -- a
		// question and at least two non-empty option lines (h.pollInput keeps
		// h.local.pollReady current) -- rather than a filled primary on an
		// empty form. The hint sits under the options box as field help.
		children = append(children, html.Form(html.Props{Class: "channel-poll-form", OnSubmit: h.pollCreate, OnInput: h.pollInput},
			html.Label(html.Props{For: "channel-poll-question", Text: m.t(KeyPollQuestion)}),
			html.Input(html.Props{ID: "channel-poll-question", Class: "chat-input", Type: "text", MaxLength: 240, Required: true, Placeholder: m.t(KeyPollQuestionPlaceholder), AutoComplete: "off", Disabled: m.ChannelPollPending || m.ChannelPollLoading}),
			html.Label(html.Props{For: "channel-poll-options", Text: m.t(KeyPollOptions)}),
			html.Textarea(html.Props{ID: "channel-poll-options", Class: "chat-input", Rows: 4, Required: true, Placeholder: m.t(KeyPollOptionsPlaceholder), Aria: map[string]string{"describedby": "channel-poll-options-hint"}, Disabled: m.ChannelPollPending || m.ChannelPollLoading}),
			html.P(html.Props{ID: "channel-poll-options-hint", Class: "muted field-help", Text: m.t(KeyPollOptionsHint)}),
			// Round 3 C-3: the sticky footer is this wrapper, not the button, so
			// the button keeps the shared primary/disabled fill instead of a
			// canvas background that painted its label invisible.
			html.Div(html.Props{Class: "channel-poll-actions"},
				html.Button(html.Props{Class: "button small", Type: "submit", Disabled: m.ChannelPollPending || m.ChannelPollLoading || m.Callbacks.CreateChannelPoll == nil || !h.local.pollReady, Text: m.t(KeyPollCreate)}))))
		return html.Section(html.Props{ID: "chat-poll-section", Class: "details-section channel-poll", TabIndex: -1, Data: map[string]string{"loading": boolString(m.ChannelPollLoading)}, Aria: map[string]string{"label": m.t(KeyPollTitle)}}, children...)
	}
	voteCountKey := KeyPollVotes
	if poll.TotalVotes == 1 {
		voteCountKey = KeyPollVoteOne
	}
	// CHAT-06: m.n renders 0 as "" (it drops the leading zero for a
	// singular/plural count that is otherwise never zero), which left an
	// empty poll reading "votes" with no number; m.nz keeps the "0".
	children = append(children, html.P(html.Props{Class: "channel-poll-question", Text: poll.Question}), html.P(html.Props{Class: "muted", Text: m.tf(voteCountKey, map[string]string{"n": m.nz(poll.TotalVotes)})}))
	rows := make([]ui.Node, 0, len(poll.Options))
	for _, option := range poll.Options {
		percent := pollPercent(option.Count, poll.TotalVotes)
		selected := option.ID == poll.MyOptionID
		label := option.Text + " · " + strconv.Itoa(option.Count) + " (" + strconv.Itoa(percent) + "%)"
		if selected {
			label += " · " + m.t(KeyPollVoted)
		}
		voteLabel := m.t(KeyPollVote)
		if poll.MyOptionID != "" {
			voteLabel = m.t(KeyPollChangeVote)
		}
		if selected {
			voteLabel = m.t(KeyPollVoted)
		}
		rowClass := "channel-poll-option"
		if selected {
			rowClass += " selected"
		}
		rows = append(rows, html.Li(html.Props{Class: rowClass},
			actionButton("button secondary small channel-poll-vote", "poll-vote", option.ID, label, m.ChannelPollPending || m.ChannelPollLoading || m.Callbacks.VoteChannelPoll == nil, ui.Text(voteLabel)),
			html.Span(html.Props{Class: "channel-poll-option-label", Text: option.Text}),
			html.Progress(html.Props{Class: "channel-poll-progress", Raw: map[string]any{"value": percent, "max": 100}, Aria: map[string]string{"label": label}}),
			html.Span(html.Props{Class: "channel-poll-count", Text: strconv.Itoa(option.Count) + " · " + strconv.Itoa(percent) + "%"})))
	}
	children = append(children, html.Ul(html.Props{Class: "channel-poll-list", Aria: map[string]string{"label": m.t(KeyPollResults)}}, rows...))
	return html.Section(html.Props{ID: "chat-poll-section", Class: "details-section channel-poll", TabIndex: -1, Data: map[string]string{"loading": boolString(m.ChannelPollLoading)}, Aria: map[string]string{"label": m.t(KeyPollTitle)}}, children...)
}

// pollFormReady reports whether a poll with this question and options text
// can be created: a question and at least two non-empty option lines.
func pollFormReady(question, options string) bool {
	return strings.TrimSpace(question) != "" && len(pollOptions(options)) >= 2
}

func pollOptions(value string) []string {
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	options := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			options = append(options, line)
		}
	}
	return options
}
