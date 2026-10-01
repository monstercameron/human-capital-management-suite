package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

var errAgentControls = errors.New("agent owner action refused")
var errAgentControlsConflict = errors.New("agent owner revision changed")
var errAgentControlsInvalid = errors.New("agent owner input is invalid")
var errAgentControlsDenied = errors.New("no agent owner operations granted")

type agentControlsReply struct {
	Snapshot productui.AgentControlsSnapshot `json:"snapshot"`
	Export   json.RawMessage                 `json:"export,omitempty"`
}

func agentControlsRequest(ctx context.Context, client *http.Client, cfg journeyclient.Config, action string, input any) (agentControlsReply, error) {
	var reply agentControlsReply
	endpoint, err := url.Parse(cfg.TunnelURL)
	if err != nil || endpoint.Host == "" || cfg.Bearer == "" {
		return reply, errAgentControls
	}
	switch endpoint.Scheme {
	case "ws":
		endpoint.Scheme = "http"
	case "wss":
		endpoint.Scheme = "https"
	case "http", "https":
	default:
		return reply, errAgentControls
	}
	method := http.MethodGet
	var body io.Reader
	endpoint.Path, endpoint.RawQuery, endpoint.Fragment = "/api/agent-controls", "", ""
	if action != "" {
		if action != "control" && action != "draft" && action != "preview" {
			return reply, errAgentControls
		}
		endpoint.Path += "/" + action
		method = http.MethodPost
		encoded, encodeErr := json.Marshal(input)
		if encodeErr != nil {
			return reply, encodeErr
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return reply, err
	}
	request.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return reply, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusForbidden {
		return reply, errAgentControlsDenied
	}
	if response.StatusCode == http.StatusConflict {
		return reply, errAgentControlsConflict
	}
	if response.StatusCode == http.StatusBadRequest {
		return reply, errAgentControlsInvalid
	}
	if response.StatusCode != http.StatusOK {
		return reply, errAgentControls
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&reply); err != nil {
		return reply, err
	}
	return reply, nil
}
