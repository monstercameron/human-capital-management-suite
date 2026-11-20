package main

import (
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Jump to date (#3930). The server pages a conversation by sequence only, so
// the browser finds the first message on or after a local day by bisecting the
// sequence range with single-message reads on the ordinary paged path. Every
// read is the person's own ListPosts call, so authorization, retention and
// removed messages are exactly what the timeline already shows; no date or
// count of anything the person cannot open is ever read, and the whole channel
// is never fetched (about log2(n) reads).

type dateJumpOutcome string

const (
	// dateJumpExact: the first message of the chosen day.
	dateJumpExact dateJumpOutcome = "exact"
	// dateJumpLater: the day has no messages; this is the next one after it.
	dateJumpLater dateJumpOutcome = "later"
	// dateJumpEarliest: the day is before the oldest message the person can
	// open; this is that oldest message.
	dateJumpEarliest dateJumpOutcome = "earliest"
	// dateJumpNewest: nothing on or after the day; this is the newest message.
	dateJumpNewest dateJumpOutcome = "newest"
	// dateJumpEmpty: the person can open no messages here.
	dateJumpEmpty dateJumpOutcome = "empty"
)

type dateJumpResult struct {
	Outcome dateJumpOutcome
	Post    *chatv1.Post
}

type dateJumpFetch func(*chatv1.ListPostsRequest) ([]*chatv1.Post, error)

const (
	dateJumpScanPage  = 50
	dateJumpScanPages = 4
	dateJumpMaxReads  = 64
)

// chatDateJumpParse reads the date field's value (YYYY-MM-DD) as midnight on
// that local calendar day, the day the timeline's separators are drawn in.
func chatDateJumpParse(value string) (time.Time, bool) {
	if len(value) != 10 {
		return time.Time{}, false
	}
	parsed, err := time.ParseInLocation("2006-01-02", value, time.Local)
	if err != nil || parsed.Year() < 1970 {
		return time.Time{}, false
	}
	return parsed, true
}

// chatDateJumpUsable is a message the timeline draws as a row: not a reply,
// not removed.
func chatDateJumpUsable(post *chatv1.Post) bool {
	return post != nil && post.GetParentId() == "" && !chatmod004Gone(post) && !chatui.IsLegacyPrivateAnswerReceipt(post.GetBody())
}

func dateJumpTime(post *chatv1.Post) time.Time {
	if ts := post.GetCreatedAt(); ts != nil {
		return ts.AsTime()
	}
	return time.Time{}
}

// chatDateJumpFind returns the first usable message created at or after dayStart
// (local midnight); dayEnd is the next local midnight and only separates an
// exact day from a later one.
func chatDateJumpFind(fetch dateJumpFetch, tenantID, conversationID string, dayStart, dayEnd time.Time, usable func(*chatv1.Post) bool) (dateJumpResult, error) {
	reads := 0
	read := func(request *chatv1.ListPostsRequest) ([]*chatv1.Post, error) {
		reads++
		request.TenantId, request.ConversationId = tenantID, conversationID
		return fetch(request)
	}
	// forward is the first usable message with a sequence above after.
	forward := func(after uint64) (*chatv1.Post, error) {
		for page := 0; page < dateJumpScanPages; page++ {
			posts, err := read(&chatv1.ListPostsRequest{AfterSequence: after, PageSize: dateJumpScanPage})
			if err != nil || len(posts) == 0 {
				return nil, err
			}
			for _, post := range posts {
				if usable(post) {
					return post, nil
				}
				after = post.GetSequence()
			}
		}
		return nil, nil
	}
	newest := func() (dateJumpResult, error) {
		posts, err := read(&chatv1.ListPostsRequest{Descending: true, PageSize: dateJumpScanPage})
		if err != nil {
			return dateJumpResult{}, err
		}
		for _, post := range posts {
			if usable(post) {
				return dateJumpResult{Outcome: dateJumpNewest, Post: post}, nil
			}
		}
		return dateJumpResult{Outcome: dateJumpEmpty}, nil
	}
	land := func(after uint64, outcome dateJumpOutcome) (dateJumpResult, error) {
		post, err := forward(after)
		if err != nil {
			return dateJumpResult{}, err
		}
		if post == nil {
			return newest()
		}
		if outcome == dateJumpExact && !dateJumpTime(post).Before(dayEnd) {
			outcome = dateJumpLater
		}
		return dateJumpResult{Outcome: outcome, Post: post}, nil
	}

	firstPage, err := read(&chatv1.ListPostsRequest{PageSize: 1})
	if err != nil {
		return dateJumpResult{}, err
	}
	if len(firstPage) == 0 {
		return dateJumpResult{Outcome: dateJumpEmpty}, nil
	}
	first := firstPage[0]
	if !dateJumpTime(first).Before(dayStart) {
		// The day is no later than the oldest message the person can open.
		result, err := land(first.GetSequence()-1, dateJumpExact)
		if err == nil && result.Outcome == dateJumpLater {
			result.Outcome = dateJumpEarliest
		}
		return result, err
	}
	lastPage, err := read(&chatv1.ListPostsRequest{Descending: true, PageSize: 1})
	if err != nil {
		return dateJumpResult{}, err
	}
	if len(lastPage) == 0 || dateJumpTime(lastPage[0]).Before(dayStart) {
		return newest()
	}
	// lo is a message before the day, hi the first one found on or after it,
	// and bound the exclusive end of the sequences not yet ruled out.
	lo, hi := first.GetSequence(), lastPage[0].GetSequence()
	bound := hi
	for lo+1 < bound && reads < dateJumpMaxReads {
		mid := lo + (bound-lo)/2
		page, err := read(&chatv1.ListPostsRequest{AfterSequence: mid - 1, PageSize: 1})
		if err != nil {
			return dateJumpResult{}, err
		}
		if len(page) == 0 || page[0].GetSequence() >= bound {
			bound = mid
			continue
		}
		probe := page[0]
		if dateJumpTime(probe).Before(dayStart) {
			lo = probe.GetSequence()
		} else {
			hi, bound = probe.GetSequence(), probe.GetSequence()
		}
	}
	return land(hi-1, dateJumpExact)
}
