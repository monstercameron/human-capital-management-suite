//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// CHATUX-012: the agent reads. The agent list that travels with the
// conversation list, and the agent directory and activity watch of the open
// conversation, are each retried until they land, so a page loaded while the
// server was restarting shows its agents without a reload.

// chatux012RetryAgentRail asks for the agent list again (1 s, 2 s, 5 s, then
// every 15 s) after the read made beside the conversation list failed. One loop
// runs per signed-in person however many times the list is read.
func chatux012RetryAgentRail(cfg journeyclient.Config) {
	identity := agentRailIdentity(cfg.Tenant, cfg.Subject)
	chatux012Retry("agent-rail|"+identity, chatux012Alive(cfg, ""), func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var payload agentRailPayload
		err := personaChatRequest(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), http.MethodGet, personachat.Path+"?rail=direct", "", &payload)
		if err != nil {
			return chatux012HTTPFinal(err)
		}
		chatAgentRail.store(identity, chatBrowser.snapshot().Conversations, payload.Agents)
		chatBrowser.mutate(func(model *chatui.Model) {
			if model.CurrentTenantID != cfg.Tenant || model.CurrentUser != cfg.Subject {
				return
			}
			applyAgentRail(model, payload.Agents)
		})
		chatStreamRender.Schedule()
		return true
	})
}

// chatux012RetryPersonaDirectory reads the agent directory of conversation again,
// with backoff, after a read of it did not get an answer, for as long as ctx (the
// conversation's own) lives.
func chatux012RetryPersonaDirectory(ctx context.Context, cfg journeyclient.Config, conversation string) {
	if ctx.Err() != nil {
		return
	}
	key := "persona-dir|" + conversation
	// A loop left over from an earlier visit belongs to a context that is gone.
	chatReadRetries.Cancel(key)
	chatux012Retry(key, func() bool { return ctx.Err() == nil }, func() bool {
		readCtx, readCancel := context.WithTimeout(ctx, 20*time.Second)
		err := refreshPersonaChatDirectory(readCtx, cfg, conversation)
		readCancel()
		return err == nil || ctx.Err() != nil
	})
}

// chatux012RetryPersonaWatch follows the agent activity of conversation again,
// with backoff, after the watch ended in a failure (the server restarting), for
// as long as ctx lives. Each attempt blocks for as long as the watch is up.
func chatux012RetryPersonaWatch(ctx context.Context, cfg journeyclient.Config, conversation string) {
	if ctx.Err() != nil {
		return
	}
	key := "persona-watch|" + conversation
	chatReadRetries.Cancel(key)
	chatux012Retry(key, func() bool { return ctx.Err() == nil }, func() bool {
		return !watchPersonaChat(ctx, cfg, conversation) || ctx.Err() != nil
	})
}
