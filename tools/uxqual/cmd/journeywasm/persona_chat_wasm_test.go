//go:build js && wasm

package main

import (
	"context"
	"syscall/js"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestTodo_AGENTP_020_WASMRebindCancelsPrivateStream(t *testing.T) {
	global := js.Global()
	priorDocument := global.Get("document")
	priorLocation := global.Get("location")
	location := global.Get("Object").New()
	location.Set("origin", "https://workspace.example")
	global.Set("location", location)
	defer global.Set("location", priorLocation)
	requestConfig := personaChatHTTPConfig(journeyclient.Config{TunnelURL: "wss://rpc.example:18553/ws", Bearer: "test-token"})
	if requestConfig.TunnelURL != "https://workspace.example" || requestConfig.Bearer != "test-token" {
		t.Fatalf("HTTP surface crossed RPC origin: %+v", requestConfig.TunnelURL)
	}
	document := global.Get("Object").New()
	installs := 0
	listener := js.Undefined()
	add := js.FuncOf(func(_ js.Value, args []js.Value) any { installs++; listener = args[1]; return nil })
	defer add.Release()
	document.Set("addEventListener", add)
	global.Set("document", document)
	defer global.Set("document", priorDocument)
	ctx, cancel := context.WithCancel(context.Background())
	personaChatBrowser.Lock()
	priorBound, priorClick, priorCancel := personaChatBrowser.bound, personaChatBrowser.click, personaChatBrowser.cancel
	priorConversation, priorIdentity := personaChatBrowser.conversation, personaChatBrowser.identity
	personaChatBrowser.bound, personaChatBrowser.cancel = false, cancel
	personaChatBrowser.Unlock()
	defer func() {
		personaChatBrowser.Lock()
		personaChatBrowser.click.Release()
		personaChatBrowser.bound, personaChatBrowser.click, personaChatBrowser.cancel = priorBound, priorClick, priorCancel
		personaChatBrowser.conversation, personaChatBrowser.identity = priorConversation, priorIdentity
		personaChatBrowser.Unlock()
	}()
	configurePersonaChatBrowser(journeyclient.Config{Tenant: "new-tenant", Subject: "new-owner"})
	configurePersonaChatBrowser(journeyclient.Config{Tenant: "new-tenant", Subject: "new-owner"})
	if ctx.Err() != context.Canceled || installs != 1 {
		t.Fatalf("rebind cancelled=%v installs=%d", ctx.Err(), installs)
	}
	event := global.Get("Object").New()
	event.Set("target", global.Get("Object").New())
	listener.Invoke(event)
	if personaChatBrowser.conversation != "" {
		t.Fatal("rebind retained previous private conversation")
	}
}
