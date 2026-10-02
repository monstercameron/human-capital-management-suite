//go:build js && wasm

package main

import (
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/ambientagents"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// The one ambient client of the page. It owns the click and submit listeners of
// the to-do and reminder cards and of the administrator's "Reads every message
// here" switch, and it is created once, when the first conversation is read
// (AGENTUX-066, -067, -068).
var (
	ambientClientMu sync.Mutex
	ambientClient   *agentUXAmbientBrowser
	ambientCfg      journeyclient.Config
)

// ambientBrowserFor returns the page's ambient client, created on first use and
// pointed at the current credentials.
func ambientBrowserFor(cfg journeyclient.Config) *agentUXAmbientBrowser {
	ambientClientMu.Lock()
	defer ambientClientMu.Unlock()
	ambientCfg = cfg
	if ambientClient == nil {
		ambientClient = newAgentUXAmbientBrowser(personaChatHTTPConfig(cfg), showAmbientAnswer)
	} else {
		ambientClient.setConfig(personaChatHTTPConfig(cfg))
	}
	return ambientClient
}

// showAmbientAnswer is what the client does with every answer it gets to a card
// or switch: it puts the answer in the open conversation, remembers it for the
// next open so a conversation shown again is not older than the person's last
// action, and draws it. An answer for a conversation that is no longer open is
// kept for that conversation and not drawn.
func showAmbientAnswer(snapshot ambientagents.Snapshot, failed bool) {
	ambientClientMu.Lock()
	cfg, client := ambientCfg, ambientClient
	ambientClientMu.Unlock()
	if client == nil {
		return
	}
	conversation := client.conversation()
	if conversation == "" {
		return
	}
	busy := false
	for _, card := range snapshot.Cards {
		busy = busy || card.Busy || card.Editing
	}
	// Only a settled answer is remembered: a card that is waiting or open for
	// editing is the page's own state, not the server's.
	if !failed && !busy {
		ambientReads.finish(ambientReadKey(cfg, conversation), snapshot, nil, time.Now())
	}
	changed := false
	chatBrowser.mutate(func(model *chatui.Model) {
		if model.SelectedID != conversation || model.CurrentTenantID != cfg.Tenant || model.CurrentUser != cfg.Subject {
			return
		}
		changed = applyAmbientSnapshot(model, snapshot, failed)
	})
	if changed {
		chatStreamRender.Schedule()
	}
}
