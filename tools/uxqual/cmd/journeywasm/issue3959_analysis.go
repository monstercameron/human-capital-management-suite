package main

import (
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// Issue 3959: the page half of Prepare an analysis. The request is a set of
// choices over the message the person picked, so it lives only here: nothing is
// sent to the server and nothing outlives the page, which keeps it private. It
// is per conversation. The hand-off navigates to the governed analytics page
// with ids and fixed tokens; no figure is produced in Chat.

type analysisBrowser struct {
	mu           sync.Mutex
	refresh      func()
	focus        func(selector string)
	conversation string
	postID       string
	metrics      map[string]bool
	choices      map[string]string
}

var analysisActive = &analysisBrowser{refresh: func() {}, focus: func(string) {}}

var analysisQuote = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

func analysisSelector(action, id string) string {
	return `[data-action="` + action + `"][data-id="` + analysisQuote.Replace(id) + `"]`
}

func (b *analysisBrowser) reset() { b.postID, b.metrics, b.choices = "", nil, nil }

// project gives the page model the builder state and its controls. Only a
// viewer who may open the analytics page is offered the action.
func (b *analysisBrowser) project(model chatui.Model, view productui.View) chatui.Model {
	b.mu.Lock()
	defer b.mu.Unlock()
	if model.SelectedID == "" {
		return model
	}
	conversation := model.SelectedID
	if b.conversation != conversation {
		b.conversation = conversation
		b.reset()
	}
	navigate := view.Navigate
	locale := view.Locale.Resolved
	if navigate == nil || !view.CanOpenAnalytics() {
		b.reset()
		return model
	}
	state := chatui.AnalysisState{Enabled: true, PostID: b.postID, Population: b.choices["population"], Period: b.choices["period"], Grouping: b.choices["grouping"]}
	if len(b.metrics) > 0 {
		state.Metrics = make(map[string]bool, len(b.metrics))
		for id := range b.metrics {
			state.Metrics[id] = true
		}
	}
	state.Open = func(post string) { b.open(conversation, post) }
	state.Close = func() { b.close(conversation) }
	state.Metric = func(id string) { b.toggle(conversation, id) }
	state.Choose = func(field, value string) { b.choose(conversation, field, value) }
	state.HandOff = func(conv, post string, request chatui.AnalysisRequest) {
		if href := chatui.AnalysisHandoffHref(locale, conv, post, request); href != "" {
			navigate(href)
		}
	}
	model.Analysis = state
	return model
}

func (b *analysisBrowser) open(conversation, post string) {
	b.mu.Lock()
	if post == "" || b.conversation != conversation {
		b.mu.Unlock()
		return
	}
	if b.postID != post {
		b.metrics, b.choices = nil, nil
	}
	b.postID = post
	b.mu.Unlock()
	b.refresh()
	b.focus(`[id="` + analysisQuote.Replace("analysis-"+post) + `"]`)
}

func (b *analysisBrowser) close(conversation string) {
	b.mu.Lock()
	post := ""
	if b.conversation == conversation {
		post = b.postID
		b.reset()
	}
	b.mu.Unlock()
	b.refresh()
	if post != "" {
		// The builder is gone; the message row takes focus back.
		b.focus(`[data-message-id="` + analysisQuote.Replace(post) + `"]`)
	}
}

func (b *analysisBrowser) toggle(conversation, id string) {
	b.mu.Lock()
	if b.conversation != conversation || b.postID == "" || id == "" {
		b.mu.Unlock()
		return
	}
	if b.metrics[id] {
		delete(b.metrics, id)
	} else {
		if b.metrics == nil {
			b.metrics = map[string]bool{}
		}
		b.metrics[id] = true
	}
	b.mu.Unlock()
	b.refresh()
	b.focus(analysisSelector("analysis-metric", id))
}

func (b *analysisBrowser) choose(conversation, field, value string) {
	b.mu.Lock()
	if b.conversation != conversation || b.postID == "" || field == "" || value == "" {
		b.mu.Unlock()
		return
	}
	if b.choices == nil {
		b.choices = map[string]string{}
	}
	b.choices[field] = value
	b.mu.Unlock()
	b.refresh()
	b.focus(analysisSelector("analysis-choose", field+":"+value))
}

// analysisProject is the page hook.
func analysisProject(model chatui.Model, view productui.View) chatui.Model {
	return analysisActive.project(model, view)
}
