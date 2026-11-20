//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"strings"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// chatsearchLoadSaved reads the person's saved searches. A read that fails
// keeps the list the page already has.
func chatsearchLoadSaved(cfg journeyclient.Config) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var wire []issue3929Wire
		if err := issue3929Request(ctx, http.DefaultClient, chatBrowser.config(cfg), http.MethodGet, "", nil, &wire); err != nil {
			return
		}
		items := issue3929Items(wire)
		chatBrowser.mutate(func(m *chatui.Model) { m.SearchSaved = items })
		refreshChatRoute()
	}()
}

// chatsearchFocusBox puts the cursor back in the search box, which also shows
// the popover again after a control inside it went away.
func chatsearchFocusBox() {
	ui.PostAsync(func() {
		if field := js.Global().Get("document").Call("getElementById", "chat-search"); field.Truthy() {
			field.Call("focus")
		}
	})
}

// chatsearchChangeSaved makes one change to the saved searches, reads the list
// again, and reports how it went on the model. A failed change leaves the list
// and the open editor alone. Focus goes where the person's attention was: the
// confirmation for a save from the results page, the search box otherwise.
func chatsearchChangeSaved(cfg journeyclient.Config, method, id string, input any, done, name string, focusNote bool) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		active := chatBrowser.config(cfg)
		err := issue3929Request(ctx, http.DefaultClient, active, method, id, input, nil)
		var items []chatui.SavedSearchItem
		status := done
		if err != nil {
			status = issue3929StatusFor(err)
		} else {
			var wire []issue3929Wire
			if issue3929Request(ctx, http.DefaultClient, active, http.MethodGet, "", nil, &wire) == nil {
				items = issue3929Items(wire)
			}
		}
		chatBrowser.mutate(func(m *chatui.Model) { issue3929Apply(m, items, status, name) })
		refreshChatRoute()
		switch {
		case focusNote:
			ui.PostAsync(func() {
				if note := js.Global().Get("document").Call("getElementById", chatui.Issue3929NoteID); note.Truthy() {
					note.Call("focus")
				}
			})
		case err == nil:
			chatsearchFocusBox()
		}
	}()
}

func chatsearchSaveSearch(cfg journeyclient.Config, name, query string) {
	name = strings.TrimSpace(name)
	chatsearchChangeSaved(cfg, http.MethodPost, "", map[string]string{"name": name, "query": strings.TrimSpace(query)}, "saved", name, true)
}

func chatsearchUpdateSaved(cfg journeyclient.Config, id, name, query string) {
	name = strings.TrimSpace(name)
	chatsearchChangeSaved(cfg, http.MethodPut, id, map[string]string{"name": name, "query": strings.TrimSpace(query)}, "updated", name, false)
}

func chatsearchDeleteSaved(cfg journeyclient.Config, id string) {
	chatsearchChangeSaved(cfg, http.MethodDelete, id, nil, "deleted", "", false)
}

// chatsearchEditSaved opens the editor of one saved search, or closes it for an
// empty id; closing returns the cursor to the search box.
func chatsearchEditSaved(id string) {
	chatBrowser.mutate(func(m *chatui.Model) {
		m.SavedSearchEditing, m.SavedSearchStatus, m.SavedSearchName = id, "", ""
	})
	refreshChatRoute()
	if id == "" {
		chatsearchFocusBox()
	}
}

// chatsearchSubmitSave keeps the search on screen under the name typed into the
// form above the results (Enter or the Save button).
func chatsearchSubmitSave(form js.Value, cfg journeyclient.Config) {
	field := form.Call("querySelector", "[name='name']")
	if !field.Truthy() {
		return
	}
	chatsearchBrowser.Lock()
	query := chatsearchBrowser.view.Query
	chatsearchBrowser.Unlock()
	chatsearchSaveSearch(cfg, field.Get("value").String(), query)
}
