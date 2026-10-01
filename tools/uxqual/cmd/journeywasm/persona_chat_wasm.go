//go:build js && wasm

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

var personaChatBrowser struct {
	sync.Mutex
	conversation, identity string
	cancel                 context.CancelFunc
	click                  js.Func
	bound                  bool
}

var personaDirectoryRefresh = newRefreshCoalescer(browserDebounceScheduler, 500*time.Millisecond, func() error {
	cfg := chatBrowser.config(journeyclient.Config{})
	conversation := chatBrowser.selectedID()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	refreshPersonaChatDirectory(ctx, cfg, conversation)
	return nil
})

// The workspace HTTP surface shares the document origin. A websocket tunnel
// may use another port and must never choose where these HTTP reads go.
func personaChatHTTPConfig(cfg journeyclient.Config) journeyclient.Config {
	cfg.TunnelURL = js.Global().Get("location").Get("origin").String()
	return cfg
}

func refreshPersonaChatDirectory(ctx context.Context, cfg journeyclient.Config, conversation string) {
	if conversation == "" {
		return
	}
	var payload personaChatDirectory
	err := personaChatRequest(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), http.MethodGet, "/api/chat/personas", conversation, &payload)
	if ctx.Err() != nil {
		return
	}
	chatBrowser.mutate(func(model *chatui.Model) {
		if model.SelectedID != conversation || model.CurrentTenantID != cfg.Tenant || model.CurrentUser != cfg.Subject {
			return
		}
		if err != nil {
			model.ResolvedPersonaMentions, model.PersonaPostActors = nil, nil
			return
		}
		model.ResolvedPersonaMentions = personaChatProfiles(payload.Personas, cfg, conversation)
		model.PersonaPostActors = personaChatActorProjection(payload.PostActors)
	})
	chatStreamRender.Schedule()
}

func configurePersonaChatBrowser(cfg journeyclient.Config) {
	personaChatBrowser.Lock()
	if personaChatBrowser.cancel != nil {
		personaChatBrowser.cancel()
	}
	personaChatBrowser.conversation = ""
	if !personaChatBrowser.bound {
		personaChatBrowser.click = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			target := args[0].Get("target")
			if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
				return nil
			}
			button := target.Call("closest", "[data-agent-action='retry'][data-agent-invocation-id]")
			if button.Truthy() {
				if button.Get("disabled").Bool() {
					return nil
				}
				args[0].Call("preventDefault")
				button.Set("disabled", true)
				go func() {
					defer button.Set("disabled", false)
					retryPersonaChat(cfg, button.Call("getAttribute", "data-agent-invocation-id").String())
				}()
			}
			return nil
		})
		js.Global().Get("document").Call("addEventListener", "click", personaChatBrowser.click)
		personaChatBrowser.bound = true
	}
	personaChatBrowser.Unlock()
}

func startPersonaChat(cfg journeyclient.Config, conversation string) {
	if conversation == "" {
		return
	}
	active := chatBrowser.config(cfg)
	identity := active.Tenant + "\x00" + active.Subject + "\x00" + active.Bearer
	personaChatBrowser.Lock()
	if personaChatBrowser.conversation == conversation && personaChatBrowser.identity == identity {
		personaChatBrowser.Unlock()
		return
	}
	if personaChatBrowser.cancel != nil {
		personaChatBrowser.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	personaChatBrowser.cancel, personaChatBrowser.conversation, personaChatBrowser.identity = cancel, conversation, identity
	personaChatBrowser.Unlock()
	go func() {
		readCtx, readCancel := context.WithTimeout(ctx, 20*time.Second)
		refreshPersonaChatDirectory(readCtx, active, conversation)
		readCancel()
		watchPersonaChat(ctx, active, conversation)
	}()
}

func watchPersonaChat(ctx context.Context, cfg journeyclient.Config, conversation string) {
	endpoint, err := personaChatURL(personaChatHTTPConfig(cfg), "/api/chat/personas/invocations", conversation)
	if err != nil {
		return
	}
	endpoint += "&watch=1"
	for ctx.Err() == nil {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		request.Header.Set("Authorization", "Bearer "+cfg.Bearer)
		request.Header.Set("Accept", "text/event-stream")
		response, readErr := http.DefaultClient.Do(request)
		if response != nil && (response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden) {
			response.Body.Close()
			chatBrowser.mutate(func(model *chatui.Model) {
				if model.SelectedID == conversation && model.CurrentTenantID == cfg.Tenant && model.CurrentUser == cfg.Subject {
					model.PersonaInvocations, model.ResolvedPersonaMentions, model.PersonaPostActors = nil, nil, nil
				}
			})
			chatStreamRender.Schedule()
			return
		}
		if readErr == nil && response.StatusCode == http.StatusOK {
			scanner := bufio.NewScanner(response.Body)
			scanner.Buffer(make([]byte, 4096), 1<<20)
			for scanner.Scan() {
				line := scanner.Text()
				if !strings.HasPrefix(line, "data:") {
					continue
				}
				var payload struct {
					Invocations []personaChatInvocation `json:"invocations"`
				}
				if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &payload) != nil {
					continue
				}
				if ctx.Err() != nil {
					break
				}
				chatBrowser.mutate(func(model *chatui.Model) {
					if model.SelectedID != conversation || model.CurrentTenantID != cfg.Tenant || model.CurrentUser != cfg.Subject {
						return
					}
					model.PersonaInvocations = personaChatInvocations(payload.Invocations, cfg, conversation)
					locale := productui.ResolveProductLocale(model.Locale)
					model.RenderPersonaTask = func(task chatui.PersonaTaskCardProps) ui.Node {
						return html.Article(html.Props{Class: "persona-task-card", Data: map[string]string{"agent-task-card": task.ID, "agent-task-revision": task.Revision}}, productui.RenderAgentTaskSummary(locale, productui.AgentTask{ID: task.ID, Title: task.Title, Goal: task.Goal, State: productui.AgentTaskState(task.State)}, task.OpenTaskHref))
					}
				})
				chatStreamRender.Schedule()
				personaDirectoryRefresh.Schedule()
			}
		}
		if response != nil {
			response.Body.Close()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func retryPersonaChat(cfg journeyclient.Config, invocation string) {
	active := chatBrowser.config(cfg)
	endpoint, err := personaChatURL(personaChatHTTPConfig(active), "/api/chat/personas/invocations/"+url.PathEscape(invocation)+"/retry", "")
	if err != nil {
		return
	}
	key := js.Global().Get("crypto").Call("randomUUID").String()
	encoded, _ := json.Marshal(map[string]string{"idempotency_key": key})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(encoded)))
	request.Header.Set("Authorization", "Bearer "+active.Bearer)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if response != nil {
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			err = errPersonaChat
		}
	}
	if chatActionFailed("retry this agent", err) {
		return
	}
}

func sendPersonaChat(cfg journeyclient.Config, conversation, parent, body string, refs []chatui.ChatReference) {
	active := chatBrowser.config(cfg)
	references, err := personaChatReferences(refs, active.Tenant, conversation)
	if err != nil {
		chatActionFailed("send this agent mention", err)
		return
	}
	client := chatBrowser.conversationClient()
	if client == nil {
		return
	}
	keyTarget := conversation + "\x00" + parent
	key := chatBrowser.sendKey(keyTarget, personaChatSendIdentity(body, refs), func() string { return fmt.Sprintf("wasm-persona-%d", time.Now().UnixNano()) })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	callCtx := chatRPCContext(ctx, active)
	sent, err := client.SendPost(callCtx, &chatv1.SendPostRequest{TenantId: active.Tenant, ConversationId: conversation, ParentId: parent, Body: body, References: references, IdempotencyKey: key})
	if chatActionFailed("send this agent mention", err) {
		return
	}
	chatBrowser.clearSendKey(keyTarget)
	if parent == "" {
		chatBrowser.setDraft(conversation, "")
		flushChatDraftPersist(active)
		chatBrowser.applySentChatPost(conversation, sent.GetPost(), active.Locale, chatDirectorySnapshot(), time.Now())
		parent = sent.GetPost().GetId()
	}
	if chatBrowser.selectedID() == conversation {
		if callback := chatBrowser.snapshot().Callbacks.OpenThread; callback != nil {
			callback(parent)
		}
		refreshChatThread(callCtx, client, active, conversation, parent)
	}
	chatStreamRender.Schedule()
}
