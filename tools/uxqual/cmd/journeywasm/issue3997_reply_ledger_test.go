//go:build !(js && wasm)

package main

import (
	"errors"
	"testing"
	"time"
)

func TestIssue3997_PartialDeliveryRetriesOnlyTheMissingPart(t *testing.T) {
	var l replyLedger
	var threadCalls, channelCalls int
	channelErr := errors.New("channel unavailable")
	thread := func() error { threadCalls++; return nil }
	channel := func() error { channelCalls++; return channelErr }

	out, err := deliverReply(&l, "room", "root", "hi", true, thread, channel)
	if out != replyChannelCopyFail || err == nil {
		t.Fatalf("first attempt = %v, %v; want the channel copy reported failed", out, err)
	}
	// Retry: the channel is back. The reply must not be posted a second time.
	channelErr = nil
	out, err = deliverReply(&l, "room", "root", "hi", true, thread, channel)
	if out != replyDelivered || err != nil {
		t.Fatalf("retry = %v, %v; want delivered", out, err)
	}
	if threadCalls != 1 || channelCalls != 2 {
		t.Fatalf("thread sends %d (want 1), channel sends %d (want 2)", threadCalls, channelCalls)
	}
}

func TestIssue3997_FailedReplySendsNoChannelCopy(t *testing.T) {
	var l replyLedger
	channelCalls := 0
	out, err := deliverReply(&l, "room", "root", "hi", true,
		func() error { return errors.New("down") },
		func() error { channelCalls++; return nil })
	if out != replyThreadFailed || err == nil || channelCalls != 0 {
		t.Fatalf("out=%v err=%v channelCalls=%d; a failed reply must not leave a lone channel post", out, err, channelCalls)
	}
	if l.threadPosted("room", "root", "hi") {
		t.Fatal("a failed reply was recorded as posted")
	}
}

func TestIssue3997_DeliveredAttemptIsForgottenAndScopedByRoom(t *testing.T) {
	var l replyLedger
	calls := 0
	send := func() error { calls++; return nil }
	deliverReply(&l, "room-a", "root", "hi", false, send, send)
	deliverReply(&l, "room-b", "root", "hi", false, send, send)
	deliverReply(&l, "room-a", "root", "hi", false, send, send)
	if calls != 3 {
		t.Fatalf("calls = %d; the same words in another room, or sent again after delivery, are new replies", calls)
	}
}

func TestIssue3997_AbandonedAttemptLapsesSoLaterIdenticalWordsPost(t *testing.T) {
	var l replyLedger
	now := time.Unix(1000, 0)
	replyLedgerNow = func() time.Time { return now }
	defer func() { replyLedgerNow = time.Now }()
	threadCalls := 0
	thread := func() error { threadCalls++; return nil }
	// The copy failed and the reader walked away.
	deliverReply(&l, "room", "root", "Thanks", true, thread, func() error { return errors.New("down") })
	now = now.Add(replyLedgerTTL + time.Second)
	deliverReply(&l, "room", "root", "Thanks", false, thread, thread)
	if threadCalls != 2 {
		t.Fatalf("thread sends = %d; identical words sent much later must be a new reply", threadCalls)
	}
}
