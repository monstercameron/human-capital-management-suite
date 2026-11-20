package main

import (
	"sort"
	"strconv"
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// issue3930Store answers ListPosts the way the server does for the fields the
// date jump uses: after/before sequence, descending, page size.
type issue3930Store struct {
	posts []*chatv1.Post
	calls int
}

func (s *issue3930Store) fetch(r *chatv1.ListPostsRequest) ([]*chatv1.Post, error) {
	s.calls++
	size := int(r.GetPageSize())
	if size <= 0 {
		size = 50
	}
	var out []*chatv1.Post
	if r.GetDescending() {
		for i := len(s.posts) - 1; i >= 0 && len(out) < size; i-- {
			p := s.posts[i]
			if r.GetBeforeSequence() > 0 && p.GetSequence() >= r.GetBeforeSequence() {
				continue
			}
			out = append(out, p)
		}
		return out, nil
	}
	for _, p := range s.posts {
		if p.GetSequence() > r.GetAfterSequence() && len(out) < size {
			out = append(out, p)
		}
	}
	return out, nil
}

func issue3930Post(seq uint64, at time.Time, parent string) *chatv1.Post {
	return &chatv1.Post{Id: "p" + strconv.FormatUint(seq, 10), Sequence: seq, ParentId: parent, CreatedAt: timestamppb.New(at)}
}

func issue3930Day(y int, m time.Month, d, hour int) time.Time {
	return time.Date(y, m, d, hour, 0, 0, 0, time.Local)
}

// issue3930Series is one post per sequence step of 2 (gaps), one per day from 1 Mar.
func issue3930Series(n int) *issue3930Store {
	s := &issue3930Store{}
	for i := 0; i < n; i++ {
		s.posts = append(s.posts, issue3930Post(uint64(10+i*2), issue3930Day(2026, time.March, 1, 9).AddDate(0, 0, i), ""))
	}
	sort.Slice(s.posts, func(i, j int) bool { return s.posts[i].GetSequence() < s.posts[j].GetSequence() })
	return s
}

func issue3930Find(t *testing.T, s *issue3930Store, day time.Time) dateJumpResult {
	t.Helper()
	res, err := chatDateJumpFind(s.fetch, "t", "c", day, day.AddDate(0, 0, 1), chatDateJumpUsable)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	return res
}

func TestIssue3930_DateJumpLandsOnFirstMessageOfTheDay(t *testing.T) {
	s := issue3930Series(400)
	target := issue3930Day(2026, time.May, 10, 0) // day index 70
	res := issue3930Find(t, s, target)
	if res.Outcome != dateJumpExact {
		t.Fatalf("outcome = %q, want exact", res.Outcome)
	}
	if want := uint64(10 + 70*2); res.Post.GetSequence() != want {
		t.Fatalf("landed on sequence %d, want %d", res.Post.GetSequence(), want)
	}
	if s.calls > 30 {
		t.Fatalf("used %d reads for 400 messages; the lookup must be logarithmic, never a walk of the channel", s.calls)
	}
}

func TestIssue3930_DateJumpOffersNextMessageWhenTheDayIsEmpty(t *testing.T) {
	s := &issue3930Store{}
	s.posts = append(s.posts,
		issue3930Post(1, issue3930Day(2026, time.March, 1, 9), ""),
		issue3930Post(2, issue3930Day(2026, time.March, 3, 9), ""),
		issue3930Post(3, issue3930Day(2026, time.March, 9, 9), ""))
	res := issue3930Find(t, s, issue3930Day(2026, time.March, 5, 0))
	if res.Outcome != dateJumpLater || res.Post.GetSequence() != 3 {
		t.Fatalf("result = %q seq %d, want later seq 3", res.Outcome, res.Post.GetSequence())
	}
}

func TestIssue3930_DateJumpBeforeHistoryLandsOnOldestVisible(t *testing.T) {
	s := issue3930Series(10)
	res := issue3930Find(t, s, issue3930Day(2025, time.January, 1, 0))
	if res.Outcome != dateJumpEarliest || res.Post.GetSequence() != 10 {
		t.Fatalf("result = %q seq %d, want earliest seq 10", res.Outcome, res.Post.GetSequence())
	}
}

func TestIssue3930_DateJumpAfterHistoryLandsOnNewest(t *testing.T) {
	s := issue3930Series(10)
	res := issue3930Find(t, s, issue3930Day(2026, time.December, 1, 0))
	if res.Outcome != dateJumpNewest || res.Post.GetSequence() != 28 {
		t.Fatalf("result = %q seq %d, want newest seq 28", res.Outcome, res.Post.GetSequence())
	}
}

func TestIssue3930_DateJumpEmptyConversation(t *testing.T) {
	res := issue3930Find(t, &issue3930Store{}, issue3930Day(2026, time.March, 5, 0))
	if res.Outcome != dateJumpEmpty || res.Post != nil {
		t.Fatalf("result = %+v, want empty", res)
	}
}

func TestIssue3930_DateJumpSkipsRepliesAndRemovedPosts(t *testing.T) {
	s := &issue3930Store{}
	s.posts = append(s.posts,
		issue3930Post(1, issue3930Day(2026, time.March, 1, 9), ""),
		issue3930Post(2, issue3930Day(2026, time.March, 5, 9), "p1"), // a reply: not a timeline row
		&chatv1.Post{Id: "gone", Sequence: 3, Deleted: true, CreatedAt: timestamppb.New(issue3930Day(2026, time.March, 5, 10))},
		issue3930Post(4, issue3930Day(2026, time.March, 5, 11), ""))
	res := issue3930Find(t, s, issue3930Day(2026, time.March, 5, 0))
	if res.Outcome != dateJumpExact || res.Post.GetSequence() != 4 {
		t.Fatalf("result = %q seq %d, want exact seq 4 (a root the timeline shows)", res.Outcome, res.Post.GetSequence())
	}
}

func TestIssue3930_DateJumpUsesLocalDayBoundary(t *testing.T) {
	// 23:30 local on the 4th is the 4th, whatever its UTC date: the jump must
	// agree with the day separators, which are drawn in local time.
	s := &issue3930Store{}
	s.posts = append(s.posts,
		issue3930Post(1, issue3930Day(2026, time.March, 4, 23).Add(30*time.Minute), ""),
		issue3930Post(2, issue3930Day(2026, time.March, 5, 0).Add(10*time.Minute), ""))
	res := issue3930Find(t, s, issue3930Day(2026, time.March, 5, 0))
	if res.Outcome != dateJumpExact || res.Post.GetSequence() != 2 {
		t.Fatalf("result = %q seq %d, want exact seq 2", res.Outcome, res.Post.GetSequence())
	}
}

func TestIssue3930_ParseJumpDate(t *testing.T) {
	day, ok := chatDateJumpParse("2026-03-05")
	if !ok || day.Year() != 2026 || day.Month() != time.March || day.Day() != 5 || day.Hour() != 0 || day.Location() != time.Local {
		t.Fatalf("parse = %v %v", day, ok)
	}
	for _, bad := range []string{"", "2026-3-5", "05/03/2026", "2026-02-30", "0000-01-01", "x"} {
		if _, ok := chatDateJumpParse(bad); ok {
			t.Fatalf("%q parsed", bad)
		}
	}
}

func TestIssue3930_OutcomesAreTheStatusesTheBarExplains(t *testing.T) {
	for outcome, status := range map[dateJumpOutcome]string{
		dateJumpExact: chatui.DateJumpExact, dateJumpLater: chatui.DateJumpLater, dateJumpEarliest: chatui.DateJumpEarliest,
		dateJumpNewest: chatui.DateJumpNewest, dateJumpEmpty: chatui.DateJumpEmpty,
	} {
		if string(outcome) != status {
			t.Fatalf("outcome %q is not the bar status %q", outcome, status)
		}
	}
}

// A jump's result (status line, Back to where you were) belongs to the room it
// was made in: leaving and coming back reloads the newest messages, so the old
// status and the old place must not be offered again.
func TestIssue3930_DateJumpStateDoesNotSurviveASwitchOfRoom(t *testing.T) {
	state := newChatStateForTest(t)
	state.selectChatConversation("general")
	state.mutate(func(m *chatui.Model) {
		m.DateJump = chatui.DateJumpView{ConversationID: "general", Status: chatui.DateJumpExact, Date: "2026-03-05", CanGoBack: true}
	})
	state.selectChatConversation("random")
	back, _ := state.selectChatConversation("general")
	if back.DateJump != (chatui.DateJumpView{}) {
		t.Fatalf("coming back to the room shows the old jump: %+v", back.DateJump)
	}
}
