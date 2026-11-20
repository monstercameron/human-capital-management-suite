//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3811: the browser half of the welcome card. The page asks the server
// what the onboarding owner says of this reader's checklist for the welcome
// cards of the open conversation, keeps the last word per card and asks again
// once it is stale, so a task finished elsewhere shows without a reload. A card
// not yet heard about says it is being checked; one that could not be read
// says so, and never shows an old list as if it were current.

const onboardingStale = 30 * time.Second

type onboardingBrowser struct {
	mu       sync.Mutex
	cfg      journeyclient.Config
	disposed bool
	views    map[string]chatui.OnboardingCardView
	loadedAt map[string]time.Time
	loading  map[string]bool
}

var onboardingActive *onboardingBrowser

func onboardingKey(conversation, post string) string { return conversation + "\x00" + post }

func installOnboardingCards(cfg journeyclient.Config) func() {
	b := &onboardingBrowser{cfg: personaChatHTTPConfig(cfg), views: map[string]chatui.OnboardingCardView{}, loadedAt: map[string]time.Time{}, loading: map[string]bool{}}
	onboardingActive = b
	return func() {
		b.disposed = true
		if onboardingActive == b {
			onboardingActive = nil
		}
	}
}

// onboardingProject gives the page's model what is known of the welcome cards
// on screen, and starts the read for the ones not yet known or gone stale.
func onboardingProject(model chatui.Model, _ journeyclient.Config) chatui.Model {
	b := onboardingActive
	if b == nil || b.disposed || model.SelectedID == "" {
		return model
	}
	conversation := model.SelectedID
	var want []string
	b.mu.Lock()
	model.Onboarding.Views = map[string]chatui.OnboardingCardView{}
	seen := map[string]bool{}
	for _, list := range [][]chatui.Message{model.Messages, model.ThreadMessages} {
		for _, msg := range list {
			if seen[msg.ID] || msg.ID == "" || !chatui.IsOnboardingCardBody(msg.Body) {
				continue
			}
			seen[msg.ID] = true
			key := onboardingKey(conversation, msg.ID)
			if view, known := b.views[key]; known {
				model.Onboarding.Views[msg.ID] = view
			}
			if !b.loading[key] && time.Since(b.loadedAt[key]) > onboardingStale && len(want) < 10 {
				want = append(want, msg.ID)
				b.loading[key] = true
			}
		}
	}
	b.mu.Unlock()
	if len(want) > 0 {
		sort.Strings(want)
		go b.load(conversation, want)
	}
	return model
}

func (b *onboardingBrowser) load(conversation string, posts []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	views, err := onboardingRead(ctx, http.DefaultClient, b.cfg, conversation, posts)
	ui.PostAsync(func() {
		b.mu.Lock()
		for _, id := range posts {
			key := onboardingKey(conversation, id)
			delete(b.loading, key)
			b.loadedAt[key] = time.Now()
			if err != nil {
				// An unreadable owner is not an empty checklist: the old list is
				// dropped (it may be out of date) and the card says it cannot be checked.
				b.views[key] = chatui.OnboardingCardView{Loaded: true, State: "unavailable"}
			} else if view, ok := views[id]; ok {
				b.views[key] = view
			}
		}
		b.mu.Unlock()
		refreshChatRoute()
	})
}
