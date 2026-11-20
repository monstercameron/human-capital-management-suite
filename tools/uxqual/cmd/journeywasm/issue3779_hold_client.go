package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3779: the browser side of a conversation's legal hold badge. One plain
// JSON call to the hold route. Nothing here decides who may know: the server
// answers only a workspace administrator who can open the conversation, and
// everyone else gets a refusal, which shows nothing.

const holdRoute = "/api/chat/holds/v1/conversation"

type holdViewJSON struct {
	Held  bool `json:"held"`
	Count int  `json:"count"`
}

// holdRead asks whether the conversation is under an active hold. Any refusal or
// failure is an error, and an error shows no badge.
func holdRead(ctx context.Context, client *http.Client, cfg journeyclient.Config, conversation string) (chatui.HoldState, error) {
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" || client == nil || conversation == "" {
		return chatui.HoldState{}, errHoldUnavailable
	}
	payload, err := json.Marshal(map[string]string{"conversation_id": conversation})
	if err != nil {
		return chatui.HoldState{}, errHoldUnavailable
	}
	endpoint.Path, endpoint.RawPath, endpoint.RawQuery, endpoint.Fragment = holdRoute, "", "", ""
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return chatui.HoldState{}, errHoldUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return chatui.HoldState{}, errHoldUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return chatui.HoldState{}, errHoldUnavailable
	}
	var view holdViewJSON
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<16)).Decode(&view); err != nil {
		return chatui.HoldState{}, errHoldUnavailable
	}
	return chatui.HoldState{ConversationID: conversation, Loaded: true, Held: view.Held && view.Count > 0, Count: view.Count}, nil
}

type holdError struct{}

func (holdError) Error() string { return "unavailable" }

var errHoldUnavailable error = holdError{}
