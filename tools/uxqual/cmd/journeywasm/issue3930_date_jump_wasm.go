//go:build js && wasm

package main

import (
	"context"
	"sync"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Jump to date (#3930), browser half. chatDateJumpFind (issue3930_date_jump.go)
// resolves the day to a message through the ordinary paged read; this file
// loads the window around it, remembers where the person was, and tells the
// header bar what happened.

// chatDateJumpPrior is the place a jump left: the message at the top of the
// view and its sequence, in one conversation. Sequence 0 means the person was
// reading the newest messages.
type chatDateJumpPrior struct {
	conversationID string
	postID         string
	sequence       uint64
}

var (
	chatDateJumpMu    sync.Mutex
	chatDateJumpPlace chatDateJumpPrior
	chatDateJumpRun   uint64
)

func chatDateJumpSet(conversationID string, view chatui.DateJumpView) {
	view.ConversationID = conversationID
	chatBrowser.mutate(func(m *chatui.Model) {
		if m.SelectedID == conversationID {
			m.DateJump = view
		}
	})
	refreshChatRoute()
}

// chatDateJumpHere notes the message at the top of the view as the place to come
// back to. In the newest window the place is "the newest messages".
func chatDateJumpHere(model chatui.Model) chatDateJumpPrior {
	prior := chatDateJumpPrior{conversationID: model.SelectedID}
	if model.HasNewer {
		visible := chatui.VisibleMessageID()
		for _, message := range model.Messages {
			if message.ID == visible && message.Sequence > 0 {
				prior.postID, prior.sequence = message.ID, message.Sequence
				break
			}
		}
		if prior.postID == "" && len(model.Messages) > 0 {
			middle := model.Messages[len(model.Messages)/2]
			prior.postID, prior.sequence = middle.ID, middle.Sequence
		}
	}
	return prior
}

// loadChatWindowAt shows the window of messages around one sequence, the way a
// search hit opens in its own conversation.
func loadChatWindowAt(cfg journeyclient.Config, conversationID string, sequence uint64, postID string) error {
	model := chatBrowser.snapshot()
	generation := chatBrowser.currentGeneration()
	client := chatBrowser.conversationClient()
	if client == nil {
		return context.Canceled
	}
	active := chatBrowser.config(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	model.SelectedID = conversationID
	if _, err := loadChatPosts(chatRPCContext(ctx, active), client, active, &model, chatDirectorySnapshot(), chatSearchAnchor{sequence: sequence, postID: postID}); err != nil {
		return err
	}
	committed := chatBrowser.commit(generation, func(current *chatui.Model) {
		if current.SelectedID != conversationID {
			return
		}
		current.Messages, current.HasOlder, current.HasNewer = model.Messages, model.HasOlder, true
		current.FocusMessageID = postID
		current.State, current.Error = chatui.StateReady, ""
	})
	if !committed {
		return context.Canceled
	}
	refreshChatRoute()
	ui.PostAsync(func() {
		current := chatBrowser.snapshot()
		if current.SelectedID == conversationID && current.FocusMessageID == postID {
			chatui.FocusSearchMessageOrElse(postID, func() {
				chatSearchHitMissing(conversationID, productui.ResolveProductLocale(active.Locale).Text(chatui.KeySearchMessageUnavailable))
			})
		}
	})
	return nil
}

// jumpChatToDate moves the open conversation to the first message on or after
// the local day (YYYY-MM-DD).
func jumpChatToDate(cfg journeyclient.Config, date string) {
	model := chatBrowser.snapshot()
	conversationID := model.SelectedID
	if conversationID == "" {
		return
	}
	dayStart, ok := chatDateJumpParse(date)
	if !ok {
		chatDateJumpSet(conversationID, chatui.DateJumpView{Status: chatui.DateJumpInvalid, Date: date, CanGoBack: model.DateJump.CanGoBack})
		return
	}
	client := chatBrowser.conversationClient()
	if client == nil {
		chatDateJumpSet(conversationID, chatui.DateJumpView{Status: chatui.DateJumpError, Date: date, CanGoBack: model.DateJump.CanGoBack})
		return
	}
	// A newer press supersedes an older one still searching.
	chatDateJumpMu.Lock()
	chatDateJumpRun++
	run := chatDateJumpRun
	prior := chatDateJumpHere(model)
	chatDateJumpMu.Unlock()
	hadBack := model.DateJump.CanGoBack
	chatDateJumpSet(conversationID, chatui.DateJumpView{Status: chatui.DateJumpLoading, Date: date, CanGoBack: hadBack})
	active := chatBrowser.config(cfg)
	go func() {
		current := func() bool {
			chatDateJumpMu.Lock()
			defer chatDateJumpMu.Unlock()
			return run == chatDateJumpRun && chatBrowser.selectedID() == conversationID
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rpc := chatRPCContext(ctx, active)
		result, err := chatDateJumpFind(func(request *chatv1.ListPostsRequest) ([]*chatv1.Post, error) {
			response, err := client.ListPosts(rpc, request)
			return response.GetPosts(), err
		}, active.Tenant, conversationID, dayStart, dayStart.AddDate(0, 0, 1), chatDateJumpUsable)
		if !current() {
			return
		}
		if err != nil {
			chatDateJumpSet(conversationID, chatui.DateJumpView{Status: chatui.DateJumpError, Date: date, CanGoBack: hadBack})
			return
		}
		if result.Post == nil {
			chatDateJumpSet(conversationID, chatui.DateJumpView{Status: chatui.DateJumpEmpty, Date: date, CanGoBack: hadBack})
			return
		}
		if err := loadChatWindowAt(cfg, conversationID, result.Post.GetSequence(), result.Post.GetId()); err != nil {
			if current() {
				chatDateJumpSet(conversationID, chatui.DateJumpView{Status: chatui.DateJumpError, Date: date, CanGoBack: hadBack})
			}
			return
		}
		chatDateJumpMu.Lock()
		chatDateJumpPlace = prior
		chatDateJumpMu.Unlock()
		chatDateJumpSet(conversationID, chatui.DateJumpView{Status: string(result.Outcome), Date: date, Landed: dateJumpTime(result.Post), CanGoBack: true})
	}()
}

// jumpBackFromDate returns to the place the last jump left.
func jumpBackFromDate(cfg journeyclient.Config) {
	conversationID := chatBrowser.selectedID()
	chatDateJumpMu.Lock()
	prior := chatDateJumpPlace
	chatDateJumpRun++
	chatDateJumpMu.Unlock()
	if conversationID == "" || prior.conversationID != conversationID {
		chatDateJumpSet(conversationID, chatui.DateJumpView{})
		return
	}
	chatDateJumpSet(conversationID, chatui.DateJumpView{})
	if prior.sequence == 0 || prior.postID == "" {
		jumpToNewestChatMessages(cfg)
		return
	}
	go func() {
		if err := loadChatWindowAt(cfg, conversationID, prior.sequence, prior.postID); err != nil && chatBrowser.selectedID() == conversationID {
			noteChatAction(productui.ResolveProductLocale(chatBrowser.config(cfg).Locale).Text(chatui.KeySearchMessageUnavailable))
		}
	}()
}
