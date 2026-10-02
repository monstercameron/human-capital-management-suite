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

// personaDirectoryRefresh reads the agent directory of the open conversation
// again. CHATBUG-014: the requests of a burst (a stream's replay) share one
// read after the burst has paused (chatperf2_personas.go). The read blocks, so
// it runs on its own goroutine, after the timer callback has returned.
var personaDirectoryRefresh = newChatperfRenderCoalescer(browserDebounceScheduler, time.Now, chatperf2DirectoryPace, func() {
	go func() {
		cfg := chatBrowser.config(journeyclient.Config{})
		conversation := chatBrowser.selectedID()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		refreshPersonaChatDirectory(ctx, cfg, conversation)
	}()
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
		// AGENTUX-070: the channel's manager requires private answers from here.
		model.Callbacks.SetChannelAgentPrivacy = func(private bool) { go setChannelAgentPrivacy(cfg, conversation, private) }
	})
	syncAgentDirectConversationTitle(chatBrowser.snapshot())
	chatStreamRender.Schedule()
	chatui.RefreshMentionMenu("chat-composer")
	chatui.RefreshMentionMenu("thread-composer")
	// AGENTUX-066: who reads messages here, read beside the agent directory.
	refreshAmbientReads(ctx, cfg, conversation)
	if chatux012HTTPFinal(err) {
		return nil
	}
	return err
}

// syncAgentDirectConversationTitle keeps the tab in step with the open
// conversation. CHATBUG-052 made the tab title one rule for every conversation
// and page, so the agent conversation is no longer a case of its own.
func syncAgentDirectConversationTitle(chatui.Model) { syncChatTabTitle() }

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
				invocation, question := domAttribute(button, "data-agent-invocation-id"), domAttribute(button, "data-agent-question")
				// CHATBUG-047: the failed card gives way to the working state at
				// once, under the question, before the server has answered.
				chatBrowser.mutate(func(model *chatui.Model) {
					model.AgentRetries = chatbug047Asked(model.AgentRetries, question, time.Now(), invocation)
					model.AgentDismissed = chatbug054Undismiss(model.AgentDismissed, invocation, "question:"+question)
				})
				chatStreamRender.Schedule()
				go func() {
					defer button.Set("disabled", false)
					retryPersonaChat(cfg, invocation, question)
				}()
				return nil
			}
			if dismiss := target.Call("closest", "[data-agent-action='dismiss'][data-agent-invocation-id]"); dismiss.Truthy() {
				// CHATBUG-054: the failed card is removed for the person who asked.
				args[0].Call("preventDefault")
				key := domAttribute(dismiss, "data-agent-invocation-id")
				chatBrowser.mutate(func(model *chatui.Model) { model.AgentDismissed = chatbug054Dismiss(model.AgentDismissed, key) })
				chatStreamRender.Schedule()
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
	// CHATBUG-088: the same conversation is started again only when the model no
	// longer holds its directory (choosing a conversation clears it).
	held := personaDirectoryHeld(chatBrowser.snapshot(), conversation)
	personaChatBrowser.Lock()
	if personaChatBrowser.conversation == conversation && personaChatBrowser.identity == identity && held {
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
	directoryPosts, firstAnswer := "", true
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
				var payload personaChatActivity
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
					// CHATBUG-040: the stored answers that came with the first read go
					// on the page in the same step as the activity that names them.
					chatbug040ApplyAnswers(model, payload.Answers, time.Now())
					// CHATUX-026: an answer shared before this page was loaded is
					// drawn as shared, and one whose copy is gone as private.
					if share, changed := chatux026SharedOnOpen(model.AgentShare, payload.Answers); changed {
						model.AgentShare = share
					}
					previous := model.PersonaInvocations
					model.PersonaInvocations = chatbug079StoredAnswers(previous, reconcileAgentPending(previous, personaChatInvocations(payload.Invocations, cfg, conversation)), time.Now())
					model.AgentRetries = chatbug047Settled(model.AgentRetries, model.PersonaInvocations)
					model.AgentFeedbackSaved = personaFeedbackSaved.stored(payload.Invocations)
					model.PersonaActivityReady = true
					bindAgentInvocationConversations(model)
					locale := productui.ResolveProductLocale(model.Locale)
					model.RenderPersonaTask = func(task chatui.PersonaTaskCardProps) ui.Node {
						return html.Article(html.Props{Class: "persona-task-card", Data: map[string]string{"agent-task-card": task.ID, "agent-task-revision": task.Revision}}, productui.RenderAgentTaskSummary(locale, productui.AgentTask{ID: task.ID, Title: task.Title, Goal: task.Goal, State: productui.AgentTaskState(task.State)}, task.OpenTaskHref))
					}
				})
				chatStreamRender.Schedule()
				nextPosts := chat5InvocationPosts(payload.Invocations)
				// CHATBUG-014: the watch's first answer is the state the
				// directory was just read from.
				if chatperf2DirectoryStale(firstAnswer, directoryPosts, nextPosts) {
					personaDirectoryRefresh.Schedule()
				}
				directoryPosts, firstAnswer = nextPosts, false
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
			changed, cards := false, false
			chatBrowser.mutate(func(model *chatui.Model) {
				if model.SelectedID == conversation && model.CurrentTenantID == cfg.Tenant && model.CurrentUser == cfg.Subject {
					changed = advancePersonaElapsed(model) || chatbug079AnswerJustDue(model.PersonaInvocations, time.Now()) || chatbug047Waiting(model.AgentRetries, time.Now())
					cards = len(model.EphemeralMessages) > 0
				}
			})
			if changed {
				chatStreamRender.Schedule()
			}
			if cards {
				chatbug061RememberCardHeights()
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

// retryPersonaChat asks the server to run a question again. question is the
// message the failed card sits under: the page shows the new attempt there, and
// says so on the card when nothing could be started.
func retryPersonaChat(cfg journeyclient.Config, invocation, question string) {
	active := chatBrowser.config(cfg)
	var result personachat.RetryResult
	settle := func(failed bool) {
		chatBrowser.mutate(func(model *chatui.Model) {
			model.AgentRetries = chatbug047Answered(model.AgentRetries, question, result.PostID, failed)
		})
		chatStreamRender.Schedule()
	}
	endpoint, err := personaChatURL(personaChatHTTPConfig(active), personachat.Path+"/invocations/"+url.PathEscape(invocation)+"/retry", "")
	if err != nil {
		settle(true)
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
		} else if json.NewDecoder(response.Body).Decode(&result) != nil {
			result = personachat.RetryResult{}
		}
	}
	if question != "" {
		// The card under the question says what came of it.
		settle(err != nil)
		return
	}
	chatActionFailed("retry this agent", err)
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

// restorePersonaFeedback shows the rating the server still holds. The card
// says beside the controls that the change was not saved (AGENTUX-059); it is
// said there once, not in a second notice elsewhere on the page.
func restorePersonaFeedback(invocation string) {
	previous := personaFeedbackSaved.rating(invocation)
	chatBrowser.mutate(func(model *chatui.Model) {
		model.AgentFeedbackRestored = personaFeedbackRestoredWith(model.AgentFeedbackRestored, invocation, previous)
	})
	chatStreamRender.Schedule()
}

// showPersonaFeedbackStored puts the ratings the server holds on the page.
func showPersonaFeedbackStored() {
	stored := personaFeedbackSaved.shown()
	chatBrowser.mutate(func(model *chatui.Model) { model.AgentFeedbackSaved = stored })
	chatStreamRender.Schedule()
}

func submitPersonaChatFeedback(cfg journeyclient.Config, invocation string, helpful bool) {
	// While the change is on its way, the agent activity's older rating for this
	// answer is not taken for the stored one (CHATBUG-066).
	personaFeedbackSaved.begin(invocation)
	defer personaFeedbackSaved.end(invocation)
	var result personachat.FeedbackResult
	if personaChatInvocationRequest(cfg, invocation, "feedback", map[string]any{"helpful": helpful, "reason": ""}, &result) != nil {
		restorePersonaFeedback(invocation)
		return
	}
	personaFeedbackSaved.confirm(invocation, personaFeedbackRating(helpful))
	showPersonaFeedbackStored()
}

func undoPersonaChatFeedback(cfg journeyclient.Config, invocation string) {
	personaFeedbackSaved.begin(invocation)
	defer personaFeedbackSaved.end(invocation)
	// The filled rating empties at once; a failure puts it back and says so.
	chatBrowser.mutate(func(model *chatui.Model) {
		model.AgentFeedbackSaved = personaFeedbackWithout(model.AgentFeedbackSaved, invocation)
	})
	chatStreamRender.Schedule()
	var result personachat.FeedbackResult
	if personaChatInvocationRequest(cfg, invocation, "feedback/undo", map[string]any{}, &result) != nil {
		restorePersonaFeedback(invocation)
		return
	}
	personaFeedbackSaved.confirm(invocation, "")
	showPersonaFeedbackStored()
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
	if response != nil {
		defer response.Body.Close()
	}
	return personaChatActionOutcome(response, err, result)
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
