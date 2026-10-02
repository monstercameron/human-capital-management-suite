//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/ambientagents"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// refreshAmbientReads reads which agents read every message in the open
// conversation and whether this person opted out of them. A conversation the
// ambient service does not answer for simply shows nothing, and a failed read
// keeps the last good values instead of blanking the line.
func refreshAmbientReads(ctx context.Context, cfg journeyclient.Config, conversation string) {
	if conversation == "" {
		return
	}
	key := ambientReadKey(cfg, conversation)
	snapshot, have, fetch := ambientReads.begin(key, time.Now())
	if fetch {
		var err error
		snapshot, err = agentUXAmbientRequest(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), conversation, "", nil)
		ambientReads.finish(key, snapshot, err, time.Now())
		if err != nil {
			return
		}
	} else if !have {
		return
	}
	// The cards and the administrator's switch share this answer; the client
	// that carries their clicks adopts it first (AGENTUX-066, -067, -068).
	snapshot = ambientBrowserFor(cfg).adopt(conversation, snapshot)
	changed := false
	chatBrowser.mutate(func(model *chatui.Model) {
		if model.SelectedID != conversation || model.CurrentTenantID != cfg.Tenant || model.CurrentUser != cfg.Subject {
			return
		}
		changed = applyAmbientSnapshot(model, snapshot, false)
		model.Callbacks.SetAmbientOptOut = func(optOut bool) { go setAmbientOptOut(cfg, conversation, optOut) }
	})
	if changed {
		chatStreamRender.Schedule()
	}
}

// setAmbientOptOut sends the person's switch and adopts the answer. The server
// enforces it before any read; the page only shows it.
func setAmbientOptOut(cfg journeyclient.Config, conversation string, optOut bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	snapshot, err := agentUXAmbientRequest(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), conversation, "opt-out", ambientagents.OptOutCommand{Conversation: conversation, OptOut: optOut})
	if err != nil {
		return
	}
	ambientReads.finish(ambientReadKey(cfg, conversation), snapshot, nil, time.Now())
	snapshot = ambientBrowserFor(cfg).adopt(conversation, snapshot)
	chatBrowser.mutate(func(model *chatui.Model) {
		if model.SelectedID == conversation && model.CurrentTenantID == cfg.Tenant && model.CurrentUser == cfg.Subject {
			applyAmbientSnapshot(model, snapshot, false)
		}
	})
	chatStreamRender.Schedule()
}
