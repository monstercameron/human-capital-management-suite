package main

import (
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CHATUX-012: every read Chat makes on load retries until it succeeds. One
// retrier serves them all, keyed per read ("widgets|<room>", "status|<room>"),
// so a failing read is never in flight twice.
var chatReadRetries = chatui.NewReadRetrier()

// chatux012Integrate2Keys prefixes every retry loop the status, features, reader
// and location reads own, so one call ends them all when the page is left.
const chatux012Integrate2Keys = "integrate2|"

// chatux012FinalRefusal reports whether err is the server saying no, which is an
// answer and is shown as one, as opposed to a failure to get an answer (the
// server restarting, the socket dropping), which is retried.
func chatux012FinalRefusal(err error) bool {
	if err == nil {
		return false
	}
	switch status.Code(err) {
	case codes.PermissionDenied, codes.NotFound, codes.Unauthenticated, codes.InvalidArgument, codes.FailedPrecondition:
		return true
	}
	return false
}

// chatux012HTTPFinal is chatux012FinalRefusal for the same-origin HTTP reads:
// a 403 is the server's answer, anything else is a failure to get one.
func chatux012HTTPFinal(err error) bool { return errors.Is(err, errPersonaChatDenied) }

// chatux012StatusFinal is chatux012FinalRefusal for the channel status reads:
// the server named a reason (a refusal whose code is not "unavailable"), as
// opposed to the read not getting through.
func chatux012StatusFinal(err error) bool {
	var refusal chatstateRefusal
	return errors.As(err, &refusal) && refusal.Code != "" && refusal.Code != "unavailable"
}

// chatux012LocationFinal is chatux012FinalRefusal for the location reads, which
// turn the HTTP status into the chat package's sentinel errors.
func chatux012LocationFinal(err error) bool {
	return errors.Is(err, chat.ErrPermissionDenied) || errors.Is(err, chat.ErrNotFound) || errors.Is(err, chat.ErrInvalidArgument) || errors.Is(err, chat.ErrConflict)
}

// chatux012Alive reports whether the person is still where a read began: the
// same sign-in and, when room is not empty, the same open conversation.
func chatux012Alive(cfg journeyclient.Config, room string) func() bool {
	active := chatBrowser.config(cfg)
	return func() bool {
		if room != "" && chatBrowser.selectedID() != room {
			return false
		}
		current := chatBrowser.config(journeyclient.Config{})
		return current.Tenant == active.Tenant && current.Subject == active.Subject && current.Bearer == active.Bearer
	}
}

// chatux012ClearDirectoryHold lets a retry read the worker directory at once:
// after a failure the state holds the next read back for two seconds, which
// would otherwise swallow the retry's first attempt.
func (s *chatState) chatux012ClearDirectoryHold() {
	s.mu.Lock()
	s.directoryRetryAfter = time.Time{}
	s.mu.Unlock()
}

// chatux012Retry keeps one retry loop for key. A second call for the same key
// wakes the waiting loop instead of starting another.
func chatux012Retry(key string, alive func() bool, attempt func() bool) {
	chatReadRetries.Start(key, alive, attempt)
}
