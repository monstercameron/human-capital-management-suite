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
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

var personaChatBrowser struct {
	sync.Mutex
	conversation, identity string
	cancel                 context.CancelFunc
	click                  js.Func
	bound                  bool
}

// browserDebounceScheduler and refreshCoalescer both return from their timer
// callbacks before this blocking read begins.
var personaDirectoryRefresh = newRefreshCoalescer(browserDebounceScheduler, 500*time.Millisecond, func() error {
	cfg := chatBrowser.config(journeyclient.Config{})
	conversation := chatBrowser.selectedID()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	refreshPersonaChatDirectory(ctx, cfg, conversation)
	return nil
})

var personaDirectoryRetry func()

func init() { personaDirectoryRetry = personaDirectoryRefresh.Schedule }

// The workspace HTTP surface shares the document origin. A websocket tunnel
// may use another port and must never choose where these HTTP reads go.
func personaChatHTTPConfig(cfg journeyclient.Config) journeyclient.Config {
	cfg.TunnelURL = js.Global().Get("location").Get("origin").String()
	return cfg
}

// refreshPersonaChatDirectory reads the agents of one conversation. It returns
// the read's error when it did not get an answer (a refusal is an answer: the
// conversation has no agents the viewer may use), so a caller can try again.
func refreshPersonaChatDirectory(ctx context.Context, cfg journeyclient.Config, conversation string) error {
	if conversation == "" {
		return nil
	}
	chatBrowser.mutate(func(model *chatui.Model) {
		if model.SelectedID == conversation && model.CurrentTenantID == cfg.Tenant && model.CurrentUser == cfg.Subject {
			model.PersonaLookup = chatui.PersonaLookupLoading
			model.PersonaLookupConversationID = conversation
			model.Callbacks.RetryPersonaMentions = personaDirectoryRetry
		}
	})
	chatStreamRender.Schedule()
	chatui.RefreshMentionMenu("chat-composer")
	chatui.RefreshMentionMenu("thread-composer")
	var payload personaChatDirectory
	err := personaChatRequest(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), http.MethodGet, personachat.Path, conversation, &payload)
	chatBrowser.mutate(func(model *chatui.Model) {
		if model.SelectedID != conversation || model.CurrentTenantID != cfg.Tenant || model.CurrentUser != cfg.Subject {
			return
		}
		applyPersonaDirectoryResult(model, payload, cfg, conversation, err, personaDirectoryRetry)
	})
	syncAgentDirectConversationTitle(chatBrowser.snapshot())
	chatStreamRender.Schedule()
	chatui.RefreshMentionMenu("chat-composer")
	chatui.RefreshMentionMenu("thread-composer")
	if chatux012HTTPFinal(err) {
		return nil
	}
	return err
}

func syncAgentDirectConversationTitle(model chatui.Model) {
	document := js.Global().Get("document")
	if !document.Truthy() {
		return
	}
	root := document.Get("documentElement")
	// getAttribute answers null for a missing attribute, and a null js.Value
	// stringifies to "<null>", which then became the tab title.
	base := ""
	if stored := root.Call("getAttribute", "data-agent-dm-title-base"); stored.Type() == js.TypeString {
		base = stored.String()
	}
	if base == "" {
		base = document.Get("title").String()
		root.Call("setAttribute", "data-agent-dm-title-base", base)
	}
	next := agentDirectConversationTitle(base, model)
	if document.Get("title").String() != next {
		document.Set("title", next)
	}
}

func configurePersonaChatBrowser(cfg journeyclient.Config) {
	personaChatBrowser.Lock()
	if personaChatBrowser.cancel != nil {
		personaChatBrowser.cancel()
	}
	personaChatBrowser.conversation = ""
	if !personaChatBrowser.bound {
		personaChatBrowser.click = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 || !args[0].Get("isTrusted").Truthy() {
				return nil
			}
			target := args[0].Get("target")
			if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
				return nil
			}
			button := target.Call("closest", "[data-agent-action='retry'][data-agent-invocation-id]")
			if button.Truthy() {
				if !integrate2RetryAllowed(args[0].Get("isTrusted").Truthy(), domAttribute(button, "data-agent-action"), domAttribute(button, "data-agent-invocation-id"), button.Get("disabled").Bool()) {
					return nil
				}
				args[0].Call("preventDefault")
				button.Set("disabled", true)
				go func() {
					defer button.Set("disabled", false)
					retryPersonaChat(cfg, domAttribute(button, "data-agent-invocation-id"))
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
	// CHATBUG-040: the agent activity (which messages were answered, and how) is
	// asked for with the page, beside the agent directory, not after it. A read
	// that gets no answer is repeated with backoff (CHATUX-012).
	go func() {
		readCtx, readCancel := context.WithTimeout(ctx, 20*time.Second)
		err := refreshPersonaChatDirectory(readCtx, active, conversation)
		readCancel()
		if err != nil {
			chatux012RetryPersonaDirectory(ctx, active, conversation)
		}
	}()
	go func() {
		if watchPersonaChat(ctx, active, conversation) {
			chatux012RetryPersonaWatch(ctx, active, conversation)
		}
	}()
	go tickPersonaChatElapsed(ctx, active, conversation)
}

// watchPersonaChat follows the agent activity of one conversation until the
// reader leaves it. It reports whether it ended in a failure that deserves
// another try (CHATUX-012): leaving, or a refusal, are not failures.
func watchPersonaChat(ctx context.Context, cfg journeyclient.Config, conversation string) bool {
	endpoint, err := personaChatURL(personaChatHTTPConfig(cfg), personachat.Path+"/invocations", conversation)
	if err != nil {
		return false
	}
	endpoint += "&watch=1"
	failures := 0
	directoryPosts := ""
	for ctx.Err() == nil {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		request.Header.Set("Authorization", "Bearer "+cfg.Bearer)
		request.Header.Set("Accept", "text/event-stream")
		response, readErr := http.DefaultClient.Do(request)
		if personaWatchTerminal(response, readErr) {
			denied := personaWatchDenied(response)
			if response != nil {
				response.Body.Close()
			}
			if denied {
				quietPersonaChatWatch(cfg, conversation)
				return false
			}
			failPersonaChatWatch(cfg, conversation)
			return true
		}
		if personaChatResponseOK(response, readErr) {
			failures = 0
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
					model.PersonaInvocations = reconcileAgentPending(model.PersonaInvocations, personaChatInvocations(payload.Invocations, cfg, conversation))
					model.PersonaActivityReady = true
					bindAgentInvocationConversations(model)
					locale := productui.ResolveProductLocale(model.Locale)
					model.RenderPersonaTask = func(task chatui.PersonaTaskCardProps) ui.Node {
						return html.Article(html.Props{Class: "persona-task-card", Data: map[string]string{"agent-task-card": task.ID, "agent-task-revision": task.Revision}}, productui.RenderAgentTaskSummary(locale, productui.AgentTask{ID: task.ID, Title: task.Title, Goal: task.Goal, State: productui.AgentTaskState(task.State)}, task.OpenTaskHref))
					}
				})
				chatStreamRender.Schedule()
				nextPosts := chat5InvocationPosts(payload.Invocations)
				if directoryPosts != nextPosts {
					directoryPosts = nextPosts
					personaDirectoryRefresh.Schedule()
				}
			}
			if scanErr := scanner.Err(); scanErr != nil {
				readErr = scanErr
			} else {
				readErr = errPersonaChat
			}
		}
		if response != nil {
			response.Body.Close()
		}
		if personaWatchTerminal(nil, readErr) {
			failPersonaChatWatch(cfg, conversation)
			return true
		}
		delay := personaWatchBackoff(failures)
		failures++
		select {
		case <-ctx.Done():
			return false
		case <-time.After(delay):
		}
	}
	return false
}

func tickPersonaChatElapsed(ctx context.Context, cfg journeyclient.Config, conversation string) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			changed := false
			chatBrowser.mutate(func(model *chatui.Model) {
				if model.SelectedID == conversation && model.CurrentTenantID == cfg.Tenant && model.CurrentUser == cfg.Subject {
					changed = advancePersonaElapsed(model)
				}
			})
			if changed {
				chatStreamRender.Schedule()
			}
		}
	}
}

// quietPersonaChatWatch ends the watch of a conversation the server will not
// show agent activity for. That is "no agents here", not a failure: the
// mention menu keeps its people and says nothing about agents (CHATBUG-028).
func quietPersonaChatWatch(cfg journeyclient.Config, conversation string) {
	chatBrowser.mutate(func(model *chatui.Model) {
		if model.SelectedID != conversation || model.CurrentTenantID != cfg.Tenant || model.CurrentUser != cfg.Subject {
			return
		}
		model.PersonaInvocations, model.ResolvedPersonaMentions, model.PersonaPostActors = nil, nil, nil
		model.PersonaActivityReady = true
		model.PersonaLookup = chatui.PersonaLookupReady
		model.PersonaLookupConversationID = conversation
	})
	chatStreamRender.Schedule()
}

func failPersonaChatWatch(cfg journeyclient.Config, conversation string) {
	chatBrowser.mutate(func(model *chatui.Model) {
		if model.SelectedID != conversation || model.CurrentTenantID != cfg.Tenant || model.CurrentUser != cfg.Subject {
			return
		}
		model.PersonaInvocations, model.ResolvedPersonaMentions, model.PersonaPostActors = nil, nil, nil
		model.PersonaActivityReady = true
		model.PersonaLookup = chatui.PersonaLookupFailed
		model.PersonaLookupConversationID = conversation
		model.Callbacks.RetryPersonaMentions = func() { restartPersonaChat(cfg, conversation) }
	})
	chatStreamRender.Schedule()
}

func restartPersonaChat(cfg journeyclient.Config, conversation string) {
	personaChatBrowser.Lock()
	if personaChatBrowser.conversation == conversation {
		if personaChatBrowser.cancel != nil {
			personaChatBrowser.cancel()
		}
		personaChatBrowser.conversation = ""
	}
	personaChatBrowser.Unlock()
	startPersonaChat(cfg, conversation)
}

func retryPersonaChat(cfg journeyclient.Config, invocation string) {
	active := chatBrowser.config(cfg)
	endpoint, err := personaChatURL(personaChatHTTPConfig(active), personachat.Path+"/invocations/"+url.PathEscape(invocation)+"/retry", "")
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
	if response == nil && err == nil {
		err = errPersonaChat
	}
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

func cancelPersonaChat(cfg journeyclient.Config, invocation string) {
	var result personachat.CancelResult
	if personaChatInvocationAction(cfg, invocation, "cancel", map[string]any{}, &result) != nil {
		return
	}
	conversation := chatBrowser.selectedID()
	if conversation != "" {
		restartPersonaChat(cfg, conversation)
	}
}

// personaFeedbackSaved is what the server last confirmed for each answer; a
// rating or undo that fails puts that back (CHATBUG-006).
var personaFeedbackSaved personaFeedbackLedger

// clearPersonaFeedbackRestored runs when the person rates again, before the
// request, so the control shows their new choice rather than an earlier fallback.
func clearPersonaFeedbackRestored(invocation string) {
	chatBrowser.mutate(func(model *chatui.Model) {
		model.AgentFeedbackRestored = personaFeedbackRestoredWithout(model.AgentFeedbackRestored, invocation)
	})
}

// restorePersonaFeedback shows the rating the server still holds and says the
// change was not saved.
func restorePersonaFeedback(invocation string) {
	locale := ""
	previous := personaFeedbackSaved.rating(invocation)
	chatBrowser.mutate(func(model *chatui.Model) {
		locale = model.Locale
		model.AgentFeedbackRestored = personaFeedbackRestoredWith(model.AgentFeedbackRestored, invocation, previous)
	})
	chatStreamRender.Schedule()
	noteChatAction(chatui.AgentRatingNotSavedText(locale))
}

func submitPersonaChatFeedback(cfg journeyclient.Config, invocation string, helpful bool) {
	var result personachat.FeedbackResult
	if personaChatInvocationRequest(cfg, invocation, "feedback", map[string]any{"helpful": helpful, "reason": ""}, &result) != nil {
		restorePersonaFeedback(invocation)
		return
	}
	personaFeedbackSaved.confirm(invocation, personaFeedbackRating(helpful))
}

func undoPersonaChatFeedback(cfg journeyclient.Config, invocation string) {
	var result personachat.FeedbackResult
	if personaChatInvocationRequest(cfg, invocation, "feedback/undo", map[string]any{}, &result) != nil {
		restorePersonaFeedback(invocation)
		return
	}
	personaFeedbackSaved.confirm(invocation, "")
}

func personaChatInvocationAction(cfg journeyclient.Config, invocation, action string, payload map[string]any, result any) error {
	err := personaChatInvocationRequest(cfg, invocation, action, payload, result)
	if err == errPersonaChat {
		chatActionFailed(action+" this agent answer", err)
	}
	return err
}

// personaChatInvocationRequest posts one invocation action and reports failure
// without saying anything: the caller decides what the person is told.
func personaChatInvocationRequest(cfg journeyclient.Config, invocation, action string, payload map[string]any, result any) error {
	active := chatBrowser.config(cfg)
	endpoint, err := personaChatURL(personaChatHTTPConfig(active), personachat.Path+"/invocations/"+url.PathEscape(invocation)+"/"+action, "")
	if err != nil {
		return err
	}
	if payload == nil {
		payload = make(map[string]any)
	}
	payload["idempotency_key"] = js.Global().Get("crypto").Call("randomUUID").String()
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(encoded)))
	request.Header.Set("Authorization", "Bearer "+active.Bearer)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if response == nil && err == nil {
		err = errPersonaChat
	}
	if response != nil {
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			err = errPersonaChat
		} else if result != nil && json.NewDecoder(response.Body).Decode(result) != nil {
			err = errPersonaChat
		}
	}
	if err != nil {
		return errPersonaChat
	}
	return nil
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
	if chatmod002Failed("send this agent mention", err, conversation, chatmod002FieldKey(conversation, parent), chatui.ModAuthorSurfaceMessage, body) {
		return
	}
	chatBrowser.clearSendKey(keyTarget)
	chatBrowser.applySentChatPost(conversation, sent.GetPost(), active.Locale, chatDirectorySnapshot(), time.Now())
	if parent == "" {
		chatBrowser.setDraft(conversation, "")
		flushChatDraftPersist(active)
	}
	chatStreamRender.Schedule()
}
