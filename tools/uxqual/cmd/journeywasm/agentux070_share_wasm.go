//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"syscall/js"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// setAgentShare records where one private answer stands, then repaints.
func setAgentShare(invocation string, state chatui.AgentShareState) {
	chatBrowser.mutate(func(model *chatui.Model) {
		next := make(map[string]chatui.AgentShareState, len(model.AgentShare)+1)
		for key, value := range model.AgentShare {
			next[key] = value
		}
		next[invocation] = state
		model.AgentShare = next
	})
	chatStreamRender.Schedule()
}

// sharePersonaChatAnswer asks the server to post the person's private answer to
// the channel it was asked in. The server decides: it answers with the reason
// when the answer stays private.
func sharePersonaChatAnswer(cfg journeyclient.Config, invocation string) {
	setAgentShare(invocation, chatui.AgentShareState{Status: chatui.AgentShareSharing})
	state := personaChatShareRequest(cfg, invocation)
	setAgentShare(invocation, state)
	if state.Status == chatui.AgentShareShared {
		refreshChatRoute()
	}
}

// personaChatShareRequest posts the share action and reads the answer with
// agentShareResult.
func personaChatShareRequest(cfg journeyclient.Config, invocation string) chatui.AgentShareState {
	failed := chatui.AgentShareState{Status: chatui.AgentShareFailed}
	active := chatBrowser.config(cfg)
	endpoint, err := personaChatURL(personaChatHTTPConfig(active), personachat.Path+"/invocations/"+url.PathEscape(invocation)+"/share", "")
	if err != nil {
		return failed
	}
	payload, err := json.Marshal(map[string]string{"idempotency_key": js.Global().Get("crypto").Call("randomUUID").String()})
	if err != nil {
		return failed
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	request.Header.Set("Authorization", "Bearer "+active.Bearer)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil || response == nil {
		return failed
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	return agentShareResult(response.StatusCode, body)
}

// removeSharedPersonaChatAnswer takes back the copy of an answer the person
// shared to the channel (CHATUX-026). The copy is the person's own message, so
// it is removed the way they remove any message of theirs; the card is private
// again once it is gone, and says so when it could not be removed.
func removeSharedPersonaChatAnswer(cfg journeyclient.Config, invocation string) {
	model := chatBrowser.snapshot()
	shared := model.AgentShare[invocation]
	if shared.Status != chatui.AgentShareShared || shared.PostID == "" {
		return
	}
	setAgentShare(invocation, chatui.AgentShareState{Status: chatui.AgentShareRemoving, PostID: shared.PostID})
	stillShared := func() {
		setAgentShare(invocation, chatui.AgentShareState{Status: chatui.AgentShareShared, PostID: shared.PostID, RemoveFailed: true})
	}
	client := chatBrowser.conversationClient()
	if client == nil {
		stillShared()
		return
	}
	active := chatBrowser.config(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := client.DeletePost(chatRPCContext(ctx, active), &chatv1.DeletePostRequest{
		TenantId: active.Tenant, ConversationId: model.SelectedID, PostId: shared.PostID, ExpectedRevision: chatux026CopyRevision(model, shared.PostID),
	})
	if err != nil {
		stillShared()
		return
	}
	chatBrowser.mutate(func(model *chatui.Model) { chatDropDeletedRow(model, shared.PostID) })
	setAgentShare(invocation, chatui.AgentShareState{})
	invalidateChatRecipientProjection()
	refreshChatRoute()
}
