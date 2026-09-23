//go:build js && wasm

package main

import (
	"context"
	"strings"
	"sync"
	"syscall/js"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/grpc"
)

var chatRetentionBrowser struct {
	sync.Mutex
	client  chatv1.ChatExtensionsServiceClient
	saving  bool
	message string
	failed  bool
}

func configureChatRetention(conn grpc.ClientConnInterface) {
	chatRetentionBrowser.Lock()
	chatRetentionBrowser.client = chatv1.NewChatExtensionsServiceClient(conn)
	chatRetentionBrowser.saving = false
	chatRetentionBrowser.message = ""
	chatRetentionBrowser.failed = false
	chatRetentionBrowser.Unlock()
}

func loadChatRetentionPolicy(parent context.Context, cfg journeyclient.Config, view *productui.View) {
	if view == nil {
		return
	}
	chatRetentionBrowser.Lock()
	client := chatRetentionBrowser.client
	message, failed := chatRetentionBrowser.message, chatRetentionBrowser.failed
	chatRetentionBrowser.message, chatRetentionBrowser.failed = "", false
	chatRetentionBrowser.Unlock()
	if client == nil {
		view.ChatRetentionError = "Chat retention settings are unavailable."
		return
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	response, err := client.GetRetentionPolicy(chatRPCContext(ctx, cfg), &chatv1.GetRetentionPolicyRequest{})
	if err != nil {
		view.ChatRetentionError = "Chat retention settings could not be loaded."
		return
	}
	view.ChatRetentionConfigured = response.GetConfigured()
	if policy := response.GetPolicy(); response.GetConfigured() && policy != nil {
		view.ChatRetentionPolicy = productui.ChatRetentionPolicy{Mode: policy.GetMode(), Revision: policy.GetRevision(), BudgetBytes: policy.GetBudgetBytes()}
		if policy.GetBeforeDateUnix() > 0 {
			view.ChatRetentionPolicy.BeforeDate = time.Unix(policy.GetBeforeDateUnix(), 0).UTC().Format("2006-01-02")
		}
	}
	if failed {
		view.ChatRetentionError = message
	} else if message != "" {
		view.ChatRetentionNotice = message
	}
}

func saveChatRetentionPolicy(cfg journeyclient.Config, policy productui.ChatRetentionPolicy, done func()) {
	chatRetentionBrowser.Lock()
	if chatRetentionBrowser.saving || chatRetentionBrowser.client == nil {
		chatRetentionBrowser.Unlock()
		return
	}
	chatRetentionBrowser.saving = true
	client := chatRetentionBrowser.client
	chatRetentionBrowser.Unlock()
	setChatRetentionSavingButton(true)
	go func() {
		defer func() {
			chatRetentionBrowser.Lock()
			chatRetentionBrowser.saving = false
			chatRetentionBrowser.Unlock()
			setChatRetentionSavingButton(false)
		}()
		requestPolicy := &chatv1.ChatRetentionPolicy{Mode: policy.Mode, Revision: policy.Revision, BudgetBytes: policy.BudgetBytes}
		if policy.Mode == "BEFORE_DATE" {
			date, err := time.Parse("2006-01-02", policy.BeforeDate)
			if err != nil {
				setChatRetentionFeedback("Choose a valid date before saving.", true)
				if done != nil {
					done()
				}
				return
			}
			requestPolicy.BeforeDateUnix = date.UTC().Unix()
		} else if policy.Mode == "SIZE_BUDGET" && policy.BudgetBytes <= 0 {
			setChatRetentionFeedback("Enter a positive storage limit before saving.", true)
			if done != nil {
				done()
			}
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := client.PutRetentionPolicy(chatRPCContext(ctx, cfg), &chatv1.PutRetentionPolicyRequest{Policy: requestPolicy, ExpectedRevision: policy.Revision})
		if err != nil {
			setChatRetentionFeedback("Chat retention policy could not be saved.", true)
		} else {
			setChatRetentionFeedback("Policy saved. Enforcement is not active yet.", false)
		}
		if done != nil {
			done()
		}
	}()
}

func setChatRetentionFeedback(message string, failed bool) {
	chatRetentionBrowser.Lock()
	chatRetentionBrowser.message = strings.TrimSpace(message)
	chatRetentionBrowser.failed = failed
	chatRetentionBrowser.Unlock()
}

func setChatRetentionSavingButton(saving bool) {
	button := js.Global().Get("document").Call("getElementById", "chat-retention-save")
	if button.Truthy() {
		button.Set("disabled", saving)
		if saving {
			button.Set("textContent", "Saving…")
		}
	}
}
