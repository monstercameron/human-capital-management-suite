package chatui

import (
	"strings"
	"testing"
)

func TestChannelPollResultsExposePercentagesAndOwnSelection(t *testing.T) {
	m := Model{State: StateReady, SelectedID: "room", ShowDetails: true, Conversations: []Conversation{{ID: "room", Kind: PublicChannel}}, ChannelPoll: ChannelPoll{Revision: 4, Question: "Where for lunch?", TotalVotes: 4, MyOptionID: "sushi", Options: []ChannelPollOption{{ID: "pizza", Text: "Pizza", Count: 3}, {ID: "sushi", Text: "Sushi", Count: 1}}}, Callbacks: Callbacks{VoteChannelPoll: func(string) {}}}
	markup := renderWithTray(t, m, "poll")
	for _, want := range []string{"Where for lunch?", "Pizza", "75%", "Sushi", "25%", "<progress", `aria-label="Channel poll"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %q", want)
		}
	}
	// A poll from before polls were messages is a result: nothing in it is voted on.
	if strings.Contains(markup, "poll-vote") || strings.Contains(markup, "<button class=\"button secondary small channel-poll-vote") {
		t.Fatalf("the standing poll still offers votes: %s", markup)
	}
	m.Conversations[0].Kind = GroupChat
	if got := renderWithTray(t, m, "poll"); strings.Contains(got, "channel-poll") {
		t.Fatal("poll controls rendered in group chat")
	}
}

func TestChannelPollVoteCountUsesSingularForOneVote(t *testing.T) {
	m := Model{State: StateReady, SelectedID: "room", ShowDetails: true, Conversations: []Conversation{{ID: "room", Kind: PublicChannel}}, ChannelPoll: ChannelPoll{Question: "Where for lunch?", TotalVotes: 1}}
	if got := renderWithTray(t, m, "poll"); !strings.Contains(got, "1 vote") || strings.Contains(got, "1 votes") {
		t.Fatalf("one-vote label missing or pluralized: %s", got)
	}
	m.ChannelPoll.TotalVotes = 2
	if got := renderWithTray(t, m, "poll"); !strings.Contains(got, "2 votes") {
		t.Fatalf("plural vote label missing: %s", got)
	}
}

func TestChannelPollHeaderShortcutOpensPollSectionAndShowsActiveState(t *testing.T) {
	calls := 0
	m := Model{
		State: StateReady, SelectedID: "room", ShowDetails: true, ChannelPoll: ChannelPoll{Question: "Where for lunch?"},
		Conversations: []Conversation{{ID: "room", Kind: PublicChannel}},
		Callbacks: Callbacks{OpenChannelPoll: func(id string) {
			calls++
			if id != "room" {
				t.Errorf("poll shortcut conversation id = %q, want room", id)
			}
		}},
	}
	got := renderWithTray(t, m, "poll")
	// CHATUX-001: the poll button left the header (the composer's add menu opens
	// the poll now); the section it opens and the action behind it are unchanged.
	for _, want := range []string{`class="chat-row selected"`, `data-id="room"`, `id="chat-poll-section"`, `data-loading="false"`, `tabIndex="-1"`} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered poll shortcut/section missing %q", want)
		}
	}
	if strings.Contains(got, "channel-poll-trigger") {
		t.Fatal("the poll button is still in the header")
	}
	trigger := renderNode(t, channelPollTrigger(m, handlers{}))
	for _, want := range []string{`class="channel-poll-trigger active"`, `data-action="open-poll"`, `aria-label="Channel poll: Where for lunch?"`, `data-id="room"`} {
		if !strings.Contains(trigger, want) {
			t.Errorf("the poll button lost %q", want)
		}
	}
	m.actWith("open-poll", "room", "")
	if calls != 1 {
		t.Fatalf("poll shortcut callback called %d times, want 1", calls)
	}
}

func TestChannelPollVoteDispatchChecksCurrentOption(t *testing.T) {
	calls := []string{}
	m := Model{ChannelPoll: ChannelPoll{Options: []ChannelPollOption{{ID: "valid"}}}, Callbacks: Callbacks{VoteChannelPoll: func(id string) { calls = append(calls, id) }}}
	m.actWith("poll-vote", "forged", "")
	m.actWith("poll-vote", "valid", "")
	if len(calls) != 1 || calls[0] != "valid" {
		t.Fatalf("vote dispatch = %v", calls)
	}
}

func TestChannelPollPercentRoundsAndHandlesEmpty(t *testing.T) {
	for _, tc := range []struct{ count, total, want int }{{0, 0, 0}, {1, 3, 33}, {2, 3, 67}, {1, 8, 13}, {5, 4, 100}} {
		if got := pollPercent(tc.count, tc.total); got != tc.want {
			t.Errorf("pollPercent(%d,%d)=%d want %d", tc.count, tc.total, got, tc.want)
		}
	}
}

// Round 3 C-10: Create needs a question and two non-empty option lines.
func TestPollFormReadyNeedsQuestionAndTwoOptions(t *testing.T) {
	for _, c := range []struct {
		question, options string
		want              bool
	}{
		{"", "", false},
		{"Lunch?", "", false},
		{"Lunch?", "Tacos\n  \n", false},
		{"  ", "Tacos\nSushi", false},
		{"Lunch?", "Tacos\r\n\r\nSushi", true},
	} {
		if got := pollFormReady(c.question, c.options); got != c.want {
			t.Errorf("pollFormReady(%q, %q) = %v, want %v", c.question, c.options, got, c.want)
		}
	}
}
