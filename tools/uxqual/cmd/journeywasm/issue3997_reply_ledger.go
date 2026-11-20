package main

import (
	"sync"
	"time"
)

// replyLedger remembers, for a thread reply that is also copied to the
// conversation, which destinations already committed (#3997). A retry after an
// uncertain or half-failed attempt then sends only the missing part, so a reply
// that landed is never posted twice and a channel copy is never skipped.
//
// The key is the room, the thread root and the words, so editing the text
// starts a new attempt. An entry also lapses after replyLedgerTTL, so the same
// short words sent again much later (for example "Thanks") are a new reply and
// are never swallowed by an attempt the reader walked away from.
type replyLedger struct {
	mu     sync.Mutex
	posted map[string]time.Time
}

// replyLedgerTTL is how long a half-finished attempt stays retryable.
const replyLedgerTTL = 3 * time.Minute

// replyLedgerNow is the clock; tests replace it.
var replyLedgerNow = time.Now

func replyLedgerKey(conversationID, parentID, body string) string {
	return conversationID + "\x00" + parentID + "\x00" + body
}

// threadPosted reports whether the reply under the thread root already committed.
func (l *replyLedger) threadPosted(conversationID, parentID, body string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := replyLedgerKey(conversationID, parentID, body)
	at, ok := l.posted[key]
	if ok && replyLedgerNow().Sub(at) > replyLedgerTTL {
		delete(l.posted, key)
		return false
	}
	return ok
}

func (l *replyLedger) markThreadPosted(conversationID, parentID, body string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.posted == nil {
		l.posted = make(map[string]time.Time)
	}
	l.posted[replyLedgerKey(conversationID, parentID, body)] = replyLedgerNow()
}

// forget ends the attempt once every wanted destination has committed.
func (l *replyLedger) forget(conversationID, parentID, body string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.posted, replyLedgerKey(conversationID, parentID, body))
}

// replyOutcome says what a finished attempt leaves for the reader.
type replyOutcome int

const (
	replyDelivered       replyOutcome = iota // everything wanted was posted
	replyThreadFailed                        // the reply did not post; nothing else was tried
	replyChannelCopyFail                     // the reply posted; the channel copy did not
)

// deliverReply sends the reply, then the channel copy when wanted, skipping a
// reply the ledger says already committed. The copy goes only after the reply
// is posted, so the two never race and a failed reply never leaves a lone copy.
func deliverReply(l *replyLedger, conversationID, parentID, body string, alsoChannel bool, sendThread, sendChannel func() error) (replyOutcome, error) {
	if !l.threadPosted(conversationID, parentID, body) {
		if err := sendThread(); err != nil {
			return replyThreadFailed, err
		}
		l.markThreadPosted(conversationID, parentID, body)
	}
	if alsoChannel {
		if err := sendChannel(); err != nil {
			return replyChannelCopyFail, err
		}
	}
	l.forget(conversationID, parentID, body)
	return replyDelivered, nil
}
