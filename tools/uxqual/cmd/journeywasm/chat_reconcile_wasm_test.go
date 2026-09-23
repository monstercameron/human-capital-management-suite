//go:build js && wasm

package main

import (
	"strings"
	"syscall/js"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/router"
	"github.com/monstercameron/GoWebComponents/v5/testkit/render"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestChatControlsPreserveTimelineDOM(t *testing.T) {
	installWASMHistory(t, productui.Path(productui.PageChat))
	oldDocument := js.Global().Get("document")
	oldWindowAdd, oldWindowRemove := js.Global().Get("addEventListener"), js.Global().Get("removeEventListener")
	oldFocused, oldResolved := lastFocusedProductRoute, lastResolvedProductView
	oldModel, oldRetry, oldQuiet := chatBrowser.snapshot(), productRouteRetry, productQuietRefreshPending
	oldRerender := chatRerender
	fallbacks := 0
	productRouteRetry = func() { fallbacks++ }
	noop := js.FuncOf(func(js.Value, []js.Value) any { return nil })
	query := js.FuncOf(func(js.Value, []js.Value) any { return js.Null() })
	queryAll := js.FuncOf(func(js.Value, []js.Value) any { return js.Global().Get("Array").New() })
	document := js.Global().Get("Object").New()
	document.Set("documentElement", js.Null())
	document.Set("querySelector", query)
	document.Set("querySelectorAll", queryAll)
	document.Set("addEventListener", noop)
	document.Set("removeEventListener", noop)
	js.Global().Set("document", document)
	js.Global().Set("addEventListener", noop)
	js.Global().Set("removeEventListener", noop)
	lastFocusedProductRoute = currentPath() + "?" + currentQuery()
	t.Cleanup(func() {
		js.Global().Set("document", oldDocument)
		js.Global().Set("addEventListener", oldWindowAdd)
		js.Global().Set("removeEventListener", oldWindowRemove)
		lastFocusedProductRoute, lastResolvedProductView = oldFocused, oldResolved
		chatBrowser.mutate(func(current *chatui.Model) { *current = oldModel })
		productRouteRetry, chatRerender, productQuietRefreshPending = oldRetry, oldRerender, oldQuiet
		query.Release()
		queryAll.Release()
		noop.Release()
	})
	fixture := render.New(t, render.WithQueuedScheduler())
	defer fixture.Cleanup()
	model := chatui.Model{
		State: chatui.StateReady, SelectedID: "room", CurrentUser: "reader",
		PhotoURLs:     map[string]string{"writer": "/workspace/assets/person-writer.jpg"},
		Conversations: []chatui.Conversation{{ID: "room", Name: "Room", Joined: true}},
		Messages:      []chatui.Message{{ID: "post", AuthorID: "writer", Author: "Writer", Body: "hello", SentAt: time.Unix(100, 0), Attachments: []chatui.Attachment{{ID: "image", Name: "image.png", ContentType: "image/png", URL: "blob:stable"}}}},
		Callbacks:     chatui.Callbacks{OpenPicker: func(string) {}, OpenMenu: func(string) {}, ReactWith: func(string, string) {}},
	}
	view := productui.NewView(productui.PageChat, "tenant", "reader", "scope")
	view.Chat = model
	chatBrowser.mutate(func(current *chatui.Model) { *current = model })
	resolved := func() *router.Element {
		return ui.CreateElement(renderProductRoute, router.Attrs{productViewKey: view})
	}
	fixture.Render(productui.LoadingProxy(productui.LoadingProxyProps{Page: productui.PageChat}))
	fixture.Stabilize()
	fixture.Rerender(resolved())
	fixture.Stabilize()
	for _, diagnostic := range fixture.BuildDiagnostics() {
		if diagnostic.Classification != "performance" {
			t.Fatalf("cold chat reconciliation diagnostic: %+v", diagnostic)
		}
	}
	if chatRerender == nil {
		t.Fatal("mounted chat page did not register its local refresh")
	}
	list := fixture.ByID("chat-main")
	if list == nil || len(fixture.AllByTag("article")) != 1 || len(fixture.AllByTag("img")) != 2 {
		t.Fatal("chat timeline not mounted")
	}
	listID := list.NodeID()
	articleID := fixture.AllByTag("article")[0].NodeID()
	imageID := fixture.AllByTag("img")[0].NodeID()
	// A thread RPC completes outside the UI event handler. Its committed state
	// must reach the DOM through the framework's async ingress without another
	// click to flush the owned component update.
	model.ShowThread, model.ThreadParentID, model.ThreadLoading = true, "post", false
	parent := model.Messages[0]
	model.ThreadParent = &parent
	model.ThreadMessages = []chatui.Message{{ID: "reply", Author: "Reader", Body: "async thread reply landed", SentAt: time.Unix(101, 0)}}
	chatBrowser.mutate(func(current *chatui.Model) { *current = model })
	refreshChatThreadRoute("room", "post")
	fixture.Stabilize()
	if !strings.Contains(fixture.Text(), "async thread reply landed") {
		t.Fatalf("async thread completion did not repaint the DOM: %s", fixture.Text())
	}
	model.ShowThread, model.ThreadParentID, model.ThreadLoading = false, "", false
	model.ThreadParent, model.ThreadMessages = nil, nil
	model.ShowDetails = true
	model.Conversations[0].Kind = chatui.PublicChannel
	model.ChannelTodo = chatui.ChannelTodoList{Revision: 2, Items: []chatui.ChannelTodoItem{{ID: "task", Text: "Review budget"}}}
	chatBrowser.mutate(func(current *chatui.Model) { *current = model })
	refreshChannelTodoRoute("room", chatBrowser.currentGeneration())
	fixture.Stabilize()
	if !strings.Contains(fixture.Text(), "Review budget") {
		t.Fatal("todo list did not mount")
	}
	model.ChannelTodoPending = true
	chatBrowser.mutate(func(current *chatui.Model) { *current = model })
	refreshChannelTodoRoute("room", chatBrowser.currentGeneration())
	fixture.Stabilize()
	model.ChannelTodoPending = false
	model.ChannelTodo.Items[0].Completed = true
	model.ChannelTodo.Revision = 3
	chatBrowser.mutate(func(current *chatui.Model) { *current = model })
	refreshChannelTodoRoute("room", chatBrowser.currentGeneration())
	fixture.Stabilize()
	completed := false
	for _, button := range fixture.AllByTag("button") {
		if button.Attr("data-action") == "todo-toggle" && button.Attr("aria-label") == "Reopen task: Review budget" && button.Attr("disabled") == "" {
			completed = true
			break
		}
	}
	if !completed {
		t.Fatalf("async todo completion did not repaint enabled controls: %s", fixture.Text())
	}
	model.ShowThread, model.ThreadParentID, model.ThreadLoading = false, "", false
	model.ThreadParent, model.ThreadMessages = nil, nil
	chatBrowser.mutate(func(current *chatui.Model) { *current = model })
	refreshChatRoute()
	fixture.Stabilize()
	for _, change := range []func(){
		func() { model.PickerID = "post" },
		func() { model.PickerID = ""; model.MenuID = "post" },
		func() {
			model.MenuID = ""
			model.Messages[0].Chips = []chatui.ReactionChip{{Emoji: "👍", Count: 1, Mine: true}}
		},
	} {
		change()
		chatBrowser.mutate(func(current *chatui.Model) { *current = model })
		refreshChatRoute()
		fixture.Stabilize()
		if fallbacks != 0 {
			t.Fatal("mounted chat action revalidated the product route")
		}
		if got := fixture.ByID("chat-main"); got == nil || got.NodeID() != listID {
			t.Fatal("local chat update remounted the timeline")
		}
		// The router inserts a warm loading answer before each resolved
		// answer. Both must retain the same chat nodes.
		fixture.Rerender(warmProductRouteContent(view))
		fixture.Stabilize()
		if got := fixture.ByID("chat-main"); got == nil || got.NodeID() != listID {
			t.Fatal("chat timeline remounted during warm loading")
		}
		fixture.Rerender(resolved())
		fixture.Stabilize()
		if got := fixture.ByID("chat-main"); got == nil || got.NodeID() != listID {
			t.Fatal("chat timeline remounted after control update")
		}
		if rows := fixture.AllByTag("article"); len(rows) != 1 || rows[0].NodeID() != articleID {
			t.Fatal("message remounted after control update")
		}
		if images := fixture.AllByTag("img"); len(images) != 2 || images[0].NodeID() != imageID {
			t.Fatal("image remounted after control update")
		}
	}
	model.HasOlder = true
	model.Messages = append([]chatui.Message{{ID: "earlier", AuthorID: "reader", Author: "Reader", Body: "earlier", SentAt: time.Unix(50, 0)}}, model.Messages...)
	chatBrowser.mutate(func(current *chatui.Model) { *current = model })
	refreshChatRoute()
	fixture.Stabilize()
	if rows := fixture.AllByTag("article"); len(rows) != 2 || rows[1].NodeID() != articleID {
		t.Fatal("loading older messages remounted an existing message")
	}
	// The old direct loading shape really does remount the workspace. This
	// control keeps the test sensitive to component-boundary regressions.
	fixture.Rerender(productui.BuildPageContent(view))
	fixture.Stabilize()
	if got := fixture.ByID("chat-main"); got == nil || got.NodeID() == listID {
		t.Fatal("direct loading shape unexpectedly preserved the chat timeline")
	}
	model.ShowThread, model.ThreadParentID = true, "post"
	chatBrowser.mutate(func(current *chatui.Model) { *current = model })
	refreshChatThreadRoute("room", "post") // queued, then invalidated by unmount
	fixture.Rerender(html.Div(html.Props{ID: "other-page"}))
	fixture.Stabilize()
	if chatRerender != nil {
		t.Fatal("unmounted chat page retained its local refresh callback")
	}
	refreshChatRoute()
	if fallbacks != 1 {
		t.Fatalf("unmounted chat fallback revalidations = %d, want one", fallbacks)
	}
	refreshChatThreadRoute("room", "post")
	fixture.Stabilize()
	if fallbacks != 1 {
		t.Fatalf("unmounted thread completion revalidated another route: %d", fallbacks)
	}
	fixture.Rerender(resolved())
	fixture.Stabilize()
	if chatRerender == nil {
		t.Fatal("remounted chat page did not register a fresh local refresh")
	}
}
