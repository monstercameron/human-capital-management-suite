//go:build js && wasm

package main

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"syscall/js"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/projectui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Sharing boards and tickets. Copy link and Copy for Docs write to the
// clipboard; Send to chat posts through the chat conversation client chat
// itself uses (chatBrowser), never a second client. The conversation list
// for the picker is read with the same client and cached briefly.

const projectShareTTL = 60 * time.Second

var (
	projectShareMu       sync.Mutex
	projectShareTargets  []projectui.ShareTarget
	projectShareAt       time.Time
	projectShareKey      string
	projectShareInstalls bool
	projectShareToast    js.Value
)

// projectShareTargetsFor returns the conversations the viewer can post to:
// joined channels by name, then direct and group messages that have one.
func projectShareTargetsFor(ctx context.Context, cfg journeyclient.Config) []projectui.ShareTarget {
	key := cfg.Tenant + "\x00" + cfg.Subject
	projectShareMu.Lock()
	if key == projectShareKey && time.Since(projectShareAt) < projectShareTTL {
		targets := projectShareTargets
		projectShareMu.Unlock()
		return targets
	}
	projectShareMu.Unlock()
	client := chatBrowser.conversationClient()
	if client == nil {
		return nil
	}
	active := chatBrowser.config(cfg)
	callCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	list, err := client.ListConversations(chatRPCContext(callCtx, active), &chatv1.ListConversationsRequest{TenantId: active.Tenant, PageSize: 100})
	if err != nil {
		return nil
	}
	channels, direct := []projectui.ShareTarget{}, []projectui.ShareTarget{}
	for _, conversation := range list.GetConversations() {
		if conversation == nil || conversation.GetArchived() || conversation.GetId() == "" {
			continue
		}
		name := strings.TrimSpace(conversation.GetName())
		switch conversation.GetKind() {
		case chatv1.ConversationKind_CONVERSATION_KIND_PUBLIC_CHANNEL, chatv1.ConversationKind_CONVERSATION_KIND_PRIVATE_CHANNEL:
			if name == "" {
				continue
			}
			channels = append(channels, projectui.ShareTarget{ID: conversation.GetId(), Label: "#" + strings.TrimPrefix(name, "#")})
		default:
			if name == "" {
				continue
			}
			direct = append(direct, projectui.ShareTarget{ID: conversation.GetId(), Label: name, Direct: true})
		}
	}
	sort.SliceStable(channels, func(i, j int) bool { return strings.ToLower(channels[i].Label) < strings.ToLower(channels[j].Label) })
	sort.SliceStable(direct, func(i, j int) bool { return strings.ToLower(direct[i].Label) < strings.ToLower(direct[j].Label) })
	targets := append(channels, direct...)
	projectShareMu.Lock()
	projectShareTargets, projectShareAt, projectShareKey = targets, time.Now(), key
	projectShareMu.Unlock()
	return targets
}

// projectShareCanonical keeps only the selectors that name the thing: the
// project, and the task when there is one.
func projectShareCanonical(href string) string {
	parsed, err := url.Parse(href)
	if err != nil {
		return href
	}
	query := parsed.Query()
	values := url.Values{}
	if project := query.Get("project"); project != "" {
		values.Set("project", project)
	}
	if task := query.Get("task"); task != "" {
		values.Set("task", task)
	} else if view := query.Get("board_view"); view != "" {
		values.Set("board_view", view)
	}
	if len(values) == 0 {
		return href
	}
	return parsed.Path + "?" + values.Encode()
}

func projectAbsolute(href string) string {
	return js.Global().Get("location").Get("origin").String() + href
}

func projectClipboard(text string) {
	if clipboard := js.Global().Get("navigator").Get("clipboard"); clipboard.Truthy() {
		func() {
			defer func() { _ = recover() }()
			clipboard.Call("writeText", text)
		}()
	}
}

// projectDocsMarkdown is a Markdown link on its own line; Docs unfurls a
// paragraph holding only a project link into a card.
func projectDocsMarkdown(title, href string) string {
	title = strings.NewReplacer("[", "(", "]", ")", "\n", " ").Replace(strings.TrimSpace(title))
	if title == "" {
		title = href
	}
	return "\n[" + title + "](" + href + ")\n"
}

func bindProjectShare(cfg journeyclient.Config) {
	if projectShareInstalls {
		return
	}
	document := js.Global().Get("document")
	if !document.Truthy() {
		return
	}
	projectShareInstalls = true
	document.Call("addEventListener", "click", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		target := args[0].Get("target")
		if docs := projectClosest(target, `[data-projectui-action="copy-docs"]`); docs.Truthy() {
			projectClipboard(projectDocsMarkdown(projectAttr(docs, "data-title"), projectShareCanonical(projectResolveShareHref(projectAttr(docs, "data-href")))))
			markProjectCopied(docs)
			return nil
		}
		if share := projectClosest(target, `[data-projectui-action="share-chat"]`); share.Truthy() {
			if words := projectClosest(share, ".projectui-journey-actions"); words.Truthy() {
				ensureProjectShareDialog(words)
			}
			openProjectShareDialog(cfg, projectShareCanonical(projectResolveShareHref(projectAttr(share, "data-href"))), projectAttr(share, "data-title"))
			if menu := projectClosest(share, "details[open]"); menu.Truthy() {
				menu.Call("removeAttribute", "open")
			}
			return nil
		}
		if closer := projectClosest(target, `[data-projectui-action="close-share"]`); closer.Truthy() {
			if dialog := projectClosest(closer, "dialog"); dialog.Truthy() {
				dialog.Call("close")
			}
		}
		return nil
	}))
	document.Call("addEventListener", "submit", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		form := args[0].Get("target")
		if projectAttr(form, "data-projectui-action") != "share-send" {
			return nil
		}
		args[0].Call("preventDefault")
		sendProjectShare(cfg, form)
		return nil
	}))
}

func markProjectCopied(button js.Value) {
	if label := button.Call("querySelector", ".projectui-menu-item-label"); label.Truthy() {
		if done := projectAttr(button, "data-copied"); done != "" {
			label.Set("textContent", done)
		}
	}
	button.Call("setAttribute", "data-copied-state", "true")
}

func openProjectShareDialog(cfg journeyclient.Config, href, title string) {
	document := js.Global().Get("document")
	dialog := document.Call("getElementById", "projectui-share-dialog")
	if !dialog.Truthy() || href == "" {
		return
	}
	form := dialog.Call("querySelector", "form")
	for name, value := range map[string]string{"href": href, "title": title} {
		if field := form.Call("querySelector", `[name="`+name+`"]`); field.Truthy() {
			field.Set("value", value)
		}
	}
	if note := form.Call("querySelector", `[name="note"]`); note.Truthy() {
		note.Set("value", "")
	}
	if item := document.Call("getElementById", "projectui-share-item"); item.Truthy() {
		item.Set("textContent", title)
	}
	if failure := form.Call("querySelector", ".projectui-share-error"); failure.Truthy() {
		failure.Set("hidden", true)
	}
	form.Call("removeAttribute", "aria-busy")
	func() {
		defer func() { _ = recover() }()
		dialog.Call("showModal")
	}()
	if picker := form.Call("querySelector", "select"); picker.Truthy() {
		picker.Call("focus")
		if projectAttr(picker, "data-fill") == "true" && projectAttr(picker, "data-filled") != "true" {
			go fillProjectSharePicker(cfg, picker)
		}
	}
}

// fillProjectSharePicker lists the viewer's conversations in a dialog whose
// page did not supply them.
func fillProjectSharePicker(cfg journeyclient.Config, picker js.Value) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	targets := projectShareTargetsFor(ctx, cfg)
	if len(targets) == 0 {
		return
	}
	document := js.Global().Get("document")
	picker.Set("innerHTML", "")
	var channels, direct js.Value
	for _, target := range targets {
		group := &channels
		label := projectAttr(picker, "data-channels")
		if target.Direct {
			group, label = &direct, projectAttr(picker, "data-direct")
		}
		if !group.Truthy() {
			*group = document.Call("createElement", "optgroup")
			(*group).Set("label", label)
			picker.Call("appendChild", *group)
		}
		option := document.Call("createElement", "option")
		option.Set("value", target.ID)
		option.Set("textContent", target.Label)
		(*group).Call("appendChild", option)
	}
	picker.Call("setAttribute", "data-filled", "true")
}

// sendProjectShare posts the note and the absolute link to the chosen
// conversation, then closes the dialog and offers the conversation.
func sendProjectShare(cfg journeyclient.Config, form js.Value) {
	if form.Call("hasAttribute", "aria-busy").Bool() {
		return
	}
	conversationID := projectFormValue(form, "conversation")
	href := projectFormValue(form, "href")
	note := ""
	if field := form.Call("querySelector", `[name="note"]`); field.Truthy() {
		note = strings.TrimSpace(field.Get("value").String())
	}
	label := ""
	if picker := form.Call("querySelector", "select"); picker.Truthy() {
		if option := picker.Get("selectedOptions").Index(0); option.Truthy() {
			label = option.Get("textContent").String()
		}
	}
	if conversationID == "" || href == "" {
		return
	}
	body := projectAbsolute(href)
	if note != "" {
		body = note + "\n" + body
	}
	form.Call("setAttribute", "aria-busy", "true")
	sentTemplate := projectAttr(form, "data-sent")
	go func() {
		client := chatBrowser.conversationClient()
		active := chatBrowser.config(cfg)
		failed := client == nil
		if !failed {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_, err := client.SendPost(chatRPCContext(ctx, active), &chatv1.SendPostRequest{
				TenantId: active.Tenant, ConversationId: conversationID, Body: body,
				IdempotencyKey: fmt.Sprintf("project-share-%d", time.Now().UnixNano()),
			})
			failed = err != nil
		}
		form.Call("removeAttribute", "aria-busy")
		if failed {
			if failure := form.Call("querySelector", ".projectui-share-error"); failure.Truthy() {
				failure.Set("hidden", false)
			}
			return
		}
		if dialog := projectClosest(form, "dialog"); dialog.Truthy() {
			dialog.Call("close")
		}
		invalidateChatRecipientProjection()
		showProjectToast(strings.ReplaceAll(sentTemplate, "{name}", label), "/workspace/app/chat#channel="+url.QueryEscape(conversationID), "")
	}()
}

func showProjectToast(text, href, linkLabel string) {
	document := js.Global().Get("document")
	// Inside an open task modal only the modal is interactive, so its own
	// toast is the one to show.
	toast := document.Call("querySelector", "dialog#project-task-dialog[open] .projectui-toast")
	if !toast.Truthy() {
		toast = document.Call("getElementById", "projectui-toast")
	}
	if !toast.Truthy() {
		return
	}
	if span := toast.Call("querySelector", ".projectui-toast-text"); span.Truthy() {
		span.Set("textContent", text)
	}
	if link := toast.Call("querySelector", ".projectui-toast-link"); link.Truthy() {
		link.Call("setAttribute", "href", href)
		if linkLabel != "" {
			link.Set("textContent", linkLabel)
		}
	}
	toast.Set("hidden", false)
	if projectShareToast.Truthy() {
		js.Global().Call("clearTimeout", projectShareToast)
	}
	var hide js.Func
	hide = js.FuncOf(func(js.Value, []js.Value) any {
		defer hide.Release()
		toast.Set("hidden", true)
		return nil
	})
	projectShareToast = js.Global().Call("setTimeout", hide, 8000)
}
