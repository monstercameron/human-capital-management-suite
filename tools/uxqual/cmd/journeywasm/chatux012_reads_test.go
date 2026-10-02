package main

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CHATUX-012, client state: what counts as a failed read (retried) as opposed to
// the server's answer (shown), that a failure is not remembered as having been
// asked, and that a failing read has one loop however often it is requested.
func TestTodo_CHATUX_012(t *testing.T) {
	t.Run("a refusal is an answer and an unreachable server is not", func(t *testing.T) {
		if chatux012FinalRefusal(nil) {
			t.Fatal("no error is not a refusal")
		}
		for _, code := range []codes.Code{codes.PermissionDenied, codes.NotFound, codes.Unauthenticated, codes.InvalidArgument, codes.FailedPrecondition} {
			if !chatux012FinalRefusal(status.Error(code, "no")) {
				t.Fatalf("%v is the server's answer, not a failure to get one", code)
			}
		}
		for _, code := range []codes.Code{codes.Unavailable, codes.DeadlineExceeded, codes.Internal, codes.Canceled, codes.Unknown} {
			if chatux012FinalRefusal(status.Error(code, "restarting")) {
				t.Fatalf("%v during a restart must be retried", code)
			}
		}
		if chatux012FinalRefusal(errors.New("connection refused")) {
			t.Fatal("a transport error must be retried")
		}
		if !chatux012HTTPFinal(errPersonaChatDenied) || chatux012HTTPFinal(errPersonaChat) {
			t.Fatal("only a 403 is final for the same-origin reads")
		}
		if !chatux012StatusFinal(chatstateRefusal{Code: "permission_denied"}) || chatux012StatusFinal(chatstateRefusal{Code: "unavailable"}) || chatux012StatusFinal(errChatstateEvent) {
			t.Fatal("the status read must retry an unavailable service and stop at a named refusal")
		}
		if !chatux012LocationFinal(chat.ErrPermissionDenied) || !chatux012LocationFinal(chat.ErrNotFound) || chatux012LocationFinal(chat.ErrUnavailable) || chatux012LocationFinal(errors.New("EOF")) {
			t.Fatal("the location read must retry an unavailable service and stop at a refusal")
		}
	})

	t.Run("a failed read is released, not remembered as asked", func(t *testing.T) {
		state := newChatStateForTest(t)
		cfg := journeyclient.Config{Tenant: "northwind", Subject: "avery", Bearer: "session", Locale: "en-US"}
		state.reset(nil, cfg, nil)
		epoch, claimed := state.claimDirectoryReadFor(cfg)
		if !claimed {
			t.Fatal("the first directory read was not claimed")
		}
		state.releaseDirectoryReadAt(epoch, cfg) // the read failed
		if _, again := state.claimDirectoryReadFor(cfg); again {
			t.Fatal("the failure hold did not apply: a retry loop would hammer the server")
		}
		// The retry clears the short hold and asks at once.
		state.chatux012ClearDirectoryHold()
		if _, again := state.claimDirectoryReadFor(cfg); !again {
			t.Fatal("a failed directory read was remembered as asked")
		}
		if !state.claimDMPeerRead("dm-1") || state.claimDMPeerRead("dm-1") {
			t.Fatal("a direct conversation's peer read must be claimed once at a time")
		}
		state.releaseDMPeerRead("dm-1") // the read failed
		if !state.claimDMPeerRead("dm-1") {
			t.Fatal("a failed peer read was remembered as asked")
		}
	})

	t.Run("an agent list that failed leaves nothing cached to answer for it", func(t *testing.T) {
		var cache agentRailCache
		identity := agentRailIdentity("northwind", "avery")
		rooms := []chatui.Conversation{{ID: "dm-1", Kind: chatui.DirectMessage}}
		if _, current := cache.state(identity, rooms); current {
			t.Fatal("an empty cache answered for the agent list")
		}
		// A failed read stores nothing, so the next read is made.
		if cache.has(identity) {
			t.Fatal("the cache holds an answer nobody gave")
		}
		cache.store(identity, rooms, []agentRailEntry{{ConversationID: "dm-1", AgentID: "agent-1", Name: "Policy Helper"}})
		if _, current := cache.state(identity, rooms); !current {
			t.Fatal("the answer that landed was not kept")
		}
	})

	t.Run("one loop per read however often it is requested", func(t *testing.T) {
		original := chatReadRetries.After
		chatReadRetries.After = func(time.Duration) <-chan time.Time {
			fired := make(chan time.Time, 1)
			fired <- time.Time{}
			return fired
		}
		defer func() { chatReadRetries.After = original }()
		release := make(chan struct{})
		entered := make(chan struct{}, 4)
		var running, peak, attempts atomic.Int32
		attempt := func() bool {
			now := running.Add(1)
			if now > peak.Load() {
				peak.Store(now)
			}
			attempts.Add(1)
			entered <- struct{}{}
			<-release
			running.Add(-1)
			return true
		}
		chatux012Retry("widgets|room-1", nil, attempt)
		<-entered
		for i := 0; i < 4; i++ {
			chatux012Retry("widgets|room-1", nil, attempt)
		}
		close(release)
		deadline := time.Now().Add(5 * time.Second)
		for chatReadRetries.Active("widgets|room-1") {
			if time.Now().After(deadline) {
				t.Fatal("the loop did not end after its read landed")
			}
			time.Sleep(time.Millisecond)
		}
		if peak.Load() != 1 || attempts.Load() != 1 {
			t.Fatalf("peak in flight = %d, attempts = %d, want 1 and 1", peak.Load(), attempts.Load())
		}
	})
}
