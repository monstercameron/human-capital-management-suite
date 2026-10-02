//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// setChannelAgentPrivacy asks the server to require, or stop requiring, private
// agent answers in the open channel (AGENTUX-070). The switch shows what was
// asked for while the request goes through; the server's answer, or the state
// from before with the failure said, is what stays.
func setChannelAgentPrivacy(cfg journeyclient.Config, conversation string, private bool) {
	before := chatBrowser.snapshot().ChannelAgentPrivacy.Private
	chatBrowser.mutate(func(model *chatui.Model) {
		model.ChannelAgentPrivacy = agentux070ChannelPrivacySaving(model.ChannelAgentPrivacy, conversation, private)
	})
	chatStreamRender.Schedule()
	var saved *bool
	active := chatBrowser.config(cfg)
	if endpoint, err := personaChatURL(personaChatHTTPConfig(active), personachat.Path+"/channel-privacy", ""); err == nil {
		encoded, _ := json.Marshal(map[string]any{"conversation_id": conversation, "private": private})
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		request, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(encoded)))
		request.Header.Set("Authorization", "Bearer "+active.Bearer)
		request.Header.Set("Content-Type", "application/json")
		if response, err := http.DefaultClient.Do(request); err == nil && response != nil {
			var result personachat.ChannelPrivacy
			if response.StatusCode == http.StatusOK && json.NewDecoder(response.Body).Decode(&result) == nil {
				saved = &result.Private
			}
			response.Body.Close()
		}
	}
	chatBrowser.mutate(func(model *chatui.Model) {
		model.ChannelAgentPrivacy = agentux070ChannelPrivacySaved(model.ChannelAgentPrivacy, conversation, saved, before)
	})
	chatStreamRender.Schedule()
}
