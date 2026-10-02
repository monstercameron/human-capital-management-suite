//go:build js && wasm

package main

import (
	"context"
	"runtime"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// CHATUX-009: the first load of Chat draws the page shell, the conversation
// list and a skeleton of the message list as soon as the conversation list has
// been read, and fills the messages in as they arrive. Three things made the
// first message wait for everything else (measured: 8 to 17 s to the composer
// at 1440 px):
//
//   - the product shell's own reads (journeys, workers, preferences) and the
//     Chat reads ran one after the other, though neither needs the other;
//   - the route did not render until the open conversation's messages, pins and
//     agent list had all been read, so the conversation list waited for them;
//   - the worker directory (a large answer to decode on a single thread) was read
//     in the middle of that wait.
//
// The route loader therefore reads the Chat list beside the shell's reads and
// returns without the open conversation's messages. That conversation is opened
// through the same path a click on it takes, which shows a skeleton and replaces
// it when the messages land. The open starts as soon as the list is adopted
// (CHATBUG-014), so the messages are normally in the model by the first render.

// chatux009RailWait is how long the first paint waits for the agent list: it is
// read beside the conversation list, so it is normally there already, and a
// slower answer is applied when it lands.
const chatux009RailWait = 150 * time.Millisecond

var chatux009Deferred struct {
	sync.Mutex
	cfg  journeyclient.Config
	open chatperfFirstOpen
}

// chatux009Defer records that the first render shows conversation id without
// its messages. The loader starts that conversation's open straight away
// (chatux009OpenAhead); chatux009Mounted starts it if the loader did not.
func chatux009Defer(cfg journeyclient.Config, id string) {
	chatux009Deferred.Lock()
	chatux009Deferred.cfg = cfg
	chatux009Deferred.Unlock()
	chatux009Deferred.open.record(id)
}

// chatux009OpenAhead starts the open of the conversation the loader left
// without messages, without waiting for the page to be on screen (CHATBUG-014;
// see chatperf_first_open.go). The route is not held for it: the first render
// draws whatever the model has by then, messages or skeleton.
func chatux009OpenAhead() {
	id, ok := chatux009Deferred.open.ahead()
	if !ok {
		return
	}
	chatux009Deferred.Lock()
	cfg := chatux009Deferred.cfg
	chatux009Deferred.Unlock()
	bootMark("chat-open-ahead")
	go chatux009Open(cfg, id)
}

// chatux009Mounted runs when the Chat page is on screen. It opens the
// conversation the loader left without messages, unless that open was started
// ahead of the paint.
func chatux009Mounted() {
	chatux009EndRender()
	id, owed, start := chatux009Deferred.open.mounted()
	if !owed {
		return
	}
	bootMark("chat-mounted")
	if !start {
		return
	}
	chatux009Deferred.Lock()
	cfg := chatux009Deferred.cfg
	chatux009Deferred.Unlock()
	go chatux009Open(cfg, id)
}

// chatux009Open opens the conversation the first load selected, through the
// same path a click on it takes.
func chatux009Open(cfg journeyclient.Config, id string) {
	// The address may name a channel, a person or a shared message: those
	// open their own conversation, so they run first, as they did when the
	// loader read the messages itself.
	chatux009HoldFor(id)
	defer chatux009HoldFor("")
	openChatShareFragment(cfg)
	openChatChannelFragment(cfg)
	openChatPersonFragment(cfg)
	if id != "" && chatBrowser.selectedID() == id {
		openChatConversation(cfg, id)
		return
	}
	// Nothing was opened, so nothing will read the directory afterwards.
	go loadChatDirectory(cfg)
}

// chatux009Read is the answer to one Chat list read started by the route loader.
type chatux009Read struct {
	model chatui.Model
	err   error
}

// chatux009Early holds the read the page started before the router existed.
var chatux009Early struct {
	sync.Mutex
	answer <-chan chatux009Read
}

// chatux009Prefetch starts the Chat list read as soon as the client can make
// calls, when the address is Chat's, instead of when the router gets to it. The
// router and its sixty page routes take a few hundred milliseconds of the single
// thread to set up; yielding once lets the call be sent first, so the round trip
// to the server overlaps that work instead of following it.
func chatux009Prefetch(ctx context.Context) {
	if currentPath() != productui.Path(productui.PageChat) {
		return
	}
	answer := chatux009BeginRead(ctx)
	chatux009Early.Lock()
	chatux009Early.answer = answer
	chatux009Early.Unlock()
	runtime.Gosched()
}

// chatux009StartRead is the route loader's Chat list read: the one the page
// started early when there is one, otherwise a new one.
func chatux009StartRead(ctx context.Context) <-chan chatux009Read {
	chatux009Early.Lock()
	early := chatux009Early.answer
	chatux009Early.answer = nil
	chatux009Early.Unlock()
	if early != nil {
		return early
	}
	return chatux009BeginRead(ctx)
}

// chatux009BeginRead begins the Chat conversation list read.
func chatux009BeginRead(ctx context.Context) <-chan chatux009Read {
	answer := make(chan chatux009Read, 1)
	chatux009BeginRender()
	chatux009TrackLoading(true)
	go func() {
		model, err := loadChatProjection(ctx)
		if model.State != chatui.StateLoading {
			// Nothing is left to wait for; a first load that deferred its messages
			// keeps the clock running until the page has them.
			chatux009TrackLoading(false)
		}
		answer <- chatux009Read{model: model, err: err}
	}()
	return answer
}

// chatux009AwaitRead waits for that read. A load that was not started early (the
// read is nil) makes the same read here.
func chatux009AwaitRead(ctx context.Context, answer <-chan chatux009Read) (chatui.Model, error) {
	if answer == nil {
		return loadChatProjection(ctx)
	}
	select {
	case read := <-answer:
		return read.model, read.err
	case <-ctx.Done():
		return chatBrowser.snapshot(), ctx.Err()
	}
}

// chatux009People is the people list the product shell read for this route, held
// for Chat to resolve names from before the shell's view has been published.
var chatux009Shell struct {
	sync.Mutex
	people []productui.Person
}

func chatux009SetPeople(people []productui.Person) {
	chatux009Shell.Lock()
	chatux009Shell.people = people
	chatux009Shell.Unlock()
}

func chatux009People() []productui.Person {
	chatux009Shell.Lock()
	defer chatux009Shell.Unlock()
	return chatux009Shell.people
}

// chatux009AdoptShellPeople resolves the names Chat shows from the people the
// product shell has already read, so the first paint carries names without
// waiting for Chat's own read of the same directory.
func chatux009AdoptShellPeople(people []productui.Person) {
	if len(people) == 0 {
		return
	}
	chatux009SetPeople(people)
	chatBrowser.applyChatDirectory(chatDirectorySnapshot())
}
