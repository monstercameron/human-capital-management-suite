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

var chatAgentRail agentRailCache

// agentRailWait is how long the first paint of the conversation list waits for
// the agent list that travels with it. A slower answer is applied when it lands.
const agentRailWait = 1500 * time.Millisecond

type agentRailResult struct {
	entries []agentRailEntry
	err     error
}

type agentRailState int

const (
	agentRailReady agentRailState = iota
	agentRailPending
	agentRailFailed
)

// startAgentRail asks the server which of the person's direct conversations are
// with agents, with each agent's name, description and stored icon. It runs
// beside the conversation list read, so both land together.
func startAgentRail(cfg journeyclient.Config) <-chan agentRailResult {
	result := make(chan agentRailResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var payload agentRailPayload
		err := personaChatRequest(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), http.MethodGet, personachat.Path+"?rail=direct", "", &payload)
		result <- agentRailResult{entries: payload.Agents, err: err}
	}()
	return result
}

// resolveAgentRail returns the agent list for the rooms just read: the cached
// answer when it still covers them, otherwise the answer to the request already
// in flight (or one started now), waiting at most agentRailWait for it.
func resolveAgentRail(cfg journeyclient.Config, rooms []chatui.Conversation, pending <-chan agentRailResult, wait time.Duration) ([]agentRailEntry, agentRailState) {
	identity := agentRailIdentity(cfg.Tenant, cfg.Subject)
	entries, current := chatAgentRail.state(identity, rooms)
	if current {
		return entries, agentRailReady
	}
	if pending == nil {
		pending = startAgentRail(cfg)
	}
	select {
	case answer := <-pending:
		if answer.err != nil {
			chatux012RetryAgentRail(cfg)
			return nil, agentRailFailed
		}
		chatAgentRail.store(identity, rooms, answer.entries)
		return answer.entries, agentRailReady
	case <-time.After(wait):
		go finishAgentRail(cfg, identity, rooms, pending)
		if chatAgentRail.has(identity) {
			// An earlier answer is on screen already; the new one replaces it when
			// it lands, and nothing waits.
			return entries, agentRailReady
		}
		return entries, agentRailPending
	}
}

// finishAgentRail applies a slow answer to the conversation list already on
// screen, or stops the rows waiting if the request failed.
func finishAgentRail(cfg journeyclient.Config, identity string, rooms []chatui.Conversation, pending <-chan agentRailResult) {
	answer := <-pending
	if answer.err == nil {
		chatAgentRail.store(identity, rooms, answer.entries)
	} else {
		chatux012RetryAgentRail(cfg)
	}
	chatBrowser.mutate(func(model *chatui.Model) {
		if model.CurrentTenantID != cfg.Tenant || model.CurrentUser != cfg.Subject {
			return
		}
		if answer.err != nil {
			// The list could not be read: the rows stop waiting and show what the
			// conversation list itself says.
			model.AgentRailPending, model.AgentIconsReady = false, true
			return
		}
		applyAgentRail(model, answer.entries)
	})
	chatStreamRender.Schedule()
}
