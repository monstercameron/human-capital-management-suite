//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"syscall/js"
	"time"

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
	status, reason := personaChatShareRequest(cfg, invocation)
	switch status {
	case chatui.AgentShareShared:
		setAgentShare(invocation, chatui.AgentShareState{Status: chatui.AgentShareShared})
		refreshChatRoute()
	case chatui.AgentShareRefused:
		setAgentShare(invocation, chatui.AgentShareState{Status: chatui.AgentShareRefused, Reason: reason})
	default:
		setAgentShare(invocation, chatui.AgentShareState{Status: chatui.AgentShareFailed})
	}
}

// personaChatShareRequest posts the share action. A 409 whose state names a
// reason is a refusal the person is told about; anything else is a failure to
// try again.
func personaChatShareRequest(cfg journeyclient.Config, invocation string) (chatui.AgentShareStatus, string) {
	active := chatBrowser.config(cfg)
	endpoint, err := personaChatURL(personaChatHTTPConfig(active), personachat.Path+"/invocations/"+url.PathEscape(invocation)+"/share", "")
	if err != nil {
		return chatui.AgentShareFailed, ""
	}
	payload, err := json.Marshal(map[string]string{"idempotency_key": js.Global().Get("crypto").Call("randomUUID").String()})
	if err != nil {
		return chatui.AgentShareFailed, ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	request.Header.Set("Authorization", "Bearer "+active.Bearer)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil || response == nil {
		return chatui.AgentShareFailed, ""
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
		return chatui.AgentShareShared, ""
	case http.StatusConflict:
		var body struct {
			State string `json:"state"`
		}
		if json.NewDecoder(response.Body).Decode(&body) == nil && (body.State == "agent" || body.State == "audience") {
			return chatui.AgentShareRefused, body.State
		}
	}
	return chatui.AgentShareFailed, ""
}
