package chatui

import (
	"strings"
	"testing"
)

func TestChannelPollResultsExposePercentagesAndOwnSelection(t *testing.T) {
	m := Model{State: StateReady, SelectedID: "room", ShowDetails: true, Conversations: []Conversation{{ID: "room", Kind: PublicChannel}}, ChannelPoll: ChannelPoll{Revision: 4, Question: "Where for lunch?", TotalVotes: 4, MyOptionID: "sushi", Options: []ChannelPollOption{{ID: "pizza", Text: "Pizza", Count: 3}, {ID: "sushi", Text: "Sushi", Count: 1}}}, Callbacks: Callbacks{VoteChannelPoll: func(string) {}}}
	markup := renderWithTray(t, m, "poll")
	for _, want := range []string{"Where for lunch?", "Pizza", "75%", "Sushi", "25%", "Your selection", "<progress", `aria-label="Channel poll"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %q", want)
		}
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
	for _, want := range []string{`class="channel-poll-trigger active"`, `data-action="open-poll"`, `aria-label="Channel poll: Where for lunch?"`, `class="chat-row selected"`, `data-id="room"`, `id="chat-poll-section"`, `data-loading="false"`, `tabIndex="-1"`} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered poll shortcut/section missing %q", want)
		}
	}
	triggerAt := strings.Index(got, `class="channel-poll-trigger active"`)
	if triggerAt < 0 || !strings.Contains(got[triggerAt:triggerAt+min(400, len(got)-triggerAt)], `data-id="room"`) {
		t.Fatal("poll header shortcut does not carry its rendered conversation ID")
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
