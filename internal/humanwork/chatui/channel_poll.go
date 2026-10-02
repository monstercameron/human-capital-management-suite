package chatui

import (
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
	children := []ui.Node{}
	if m.ChannelPollLoading {
		children = append(children, ChatLoadingFrame(LoadingFrame{Locale: m.Locale, Shape: LoadingShapeSection, Rows: 1, Status: m.t(KeyPollLoading), RetryData: map[string]string{"action": "poll-retry"}}))
	}
	if m.ChannelPollError != "" {
		children = append(children, html.P(html.Props{Role: "alert", Text: modAuthorErrorOr(m, m.ChannelPollError, m.t(KeyPollError))}), actionButton("button secondary small", "poll-retry", "", m.t(KeyRetry), m.Callbacks.RetryChannelPoll == nil || m.ChannelPollPending, ui.Text(m.t(KeyRetry))))
		return html.Section(html.Props{ID: "chat-poll-section", Class: "details-section channel-poll", TabIndex: -1, Data: map[string]string{"loading": boolString(m.ChannelPollLoading)}, Aria: map[string]string{"label": m.t(KeyPollTitle)}}, children...)
	}
	poll := m.ChannelPoll
	if poll.Question == "" && chatcmd003Available(m) {
		// CHATBUG-057: a poll is posted in the conversation, where every member
		// votes on it. With no standing poll here there is nothing to show but
		// the way to start one, which is the same preview /poll opens.
		children = append(children, html.P(html.Props{Class: "muted", Text: chatcmd003Text(m, "standing-none")}),
			html.Div(html.Props{Class: "channel-poll-actions"},
				html.Button(html.Props{Class: "button small", Type: "button", Data: map[string]string{"action": "composer-add", "id": "chat-composer", "extra": "poll"}, Text: chatcmd003Text(m, "standing-start")})))
		return html.Section(html.Props{ID: "chat-poll-section", Class: "details-section channel-poll", TabIndex: -1, Data: map[string]string{"loading": boolString(m.ChannelPollLoading)}, Aria: map[string]string{"label": m.t(KeyPollTitle)}}, children...)
	}
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
	// CHATCMD-002: a poll is a message in the conversation now, where every
	// member votes. This one from before is shown as its result, with no vote
	// buttons; its starter moves it into the conversation as a card.
	if chatcmd003Available(m) {
		children = append(children, html.P(html.Props{Class: "muted", Dir: "auto", Text: chatcmd002EditText(m, "standing-read")}))
	}
	rows := make([]ui.Node, 0, len(poll.Options))
	for _, option := range poll.Options {
		percent := pollPercent(option.Count, poll.TotalVotes)
		label := option.Text + " · " + chatCount(m.Locale, option.Count) + " (" + chatCount(m.Locale, percent) + "%)"
		rows = append(rows, html.Li(html.Props{Class: "channel-poll-option readonly"},
			html.Span(html.Props{Class: "channel-poll-option-label", Dir: "auto", Text: option.Text}),
			html.Progress(html.Props{Class: "channel-poll-progress", Raw: map[string]any{"value": percent, "max": 100}, Aria: map[string]string{"label": label}}),
			html.Span(html.Props{Class: "channel-poll-count", Text: chatCount(m.Locale, option.Count) + " · " + chatCount(m.Locale, percent) + "%"})))
	}
	children = append(children, html.Ul(html.Props{Class: "channel-poll-list", Aria: map[string]string{"label": m.t(KeyPollResults)}}, rows...))
	// CHATBUG-074: a channel's poll can be ended, which makes room for the next
	// one. The channel's managers are offered it here; the server also accepts
	// the person who started the poll. Beside it, the same people may move the
	// poll into the conversation (CHATBUG-057).
	if m.ChannelTeam.CanPin && m.Chatcmd002.CloseChannelPoll != nil {
		actions := []ui.Node{}
		if chatbug057CanMove(m) {
			actions = append(actions, html.Button(html.Props{Class: "button small", Type: "button", Data: map[string]string{"action": "chatbug057-move"}, Text: chatcmd003Text(m, "standing-move")}))
		}
		actions = append(actions, html.Button(html.Props{Class: "button secondary small", Type: "button", Disabled: m.ChannelPollPending || m.ChannelPollLoading, Data: map[string]string{"action": "poll-close"}, Text: chatcmd003Text(m, "close-poll")}))
		children = append(children, html.Div(html.Props{Class: "channel-poll-actions"}, actions...))
	}
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
