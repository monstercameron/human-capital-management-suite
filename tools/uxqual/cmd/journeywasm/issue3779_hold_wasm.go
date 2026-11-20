//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3779: the browser half of a conversation's legal hold badge. The page
// asks the server about the open conversation, keeps the last word per
// conversation and asks again after a while, so a hold placed or released
// elsewhere shows without a reload. A refusal (the reader is not a workspace
// administrator) or a failure shows nothing: the badge appears only on the
// server's word.

const holdStale = 60 * time.Second

var holdBrowser struct {
	sync.Mutex
	states  map[string]chatui.HoldState
	askedAt map[string]time.Time
	loading map[string]bool
}

// holdProject gives the page's model what is known of the open conversation's
// legal hold, and starts the read when it is not known or has gone stale.
func holdProject(model chatui.Model, cfg journeyclient.Config) chatui.Model {
	// Only an administrator is ever told, so nobody else is asked. The server
	// refuses everyone else whatever the page asks.
	if model.SelectedID == "" || !model.IsTenantAdmin {
		return model
	}
	conversation := model.SelectedID
	holdBrowser.Lock()
	if holdBrowser.states == nil {
		holdBrowser.states, holdBrowser.askedAt, holdBrowser.loading = map[string]chatui.HoldState{}, map[string]time.Time{}, map[string]bool{}
	}
	state, known := holdBrowser.states[conversation]
	asked, askedBefore := holdBrowser.askedAt[conversation]
	load := !holdBrowser.loading[conversation] && (!askedBefore || time.Since(asked) > holdStale)
	if load {
		holdBrowser.loading[conversation] = true
	}
	if known {
		model.Hold = state
	}
	holdBrowser.Unlock()
	if load {
		go holdLoad(cfg, conversation)
	}
	return model
}

func holdLoad(cfg journeyclient.Config, conversation string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	state, err := holdRead(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), conversation)
	ui.PostAsync(func() {
		holdBrowser.Lock()
		delete(holdBrowser.loading, conversation)
		holdBrowser.askedAt[conversation] = time.Now()
		changed := false
		if err == nil {
			changed = holdBrowser.states[conversation] != state
			holdBrowser.states[conversation] = state
		} else if _, had := holdBrowser.states[conversation]; had {
			// An unreadable answer is not "not held": show nothing it did not say.
			delete(holdBrowser.states, conversation)
			changed = true
		}
		holdBrowser.Unlock()
		if changed {
			refreshChatRoute()
		}
	})
}
