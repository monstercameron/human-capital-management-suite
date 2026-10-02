package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/ambientagents"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

var errAgentUXAmbientRequest = errors.New("ambient agent request failed")

func agentUXAmbientRequest(ctx context.Context, client *http.Client, cfg journeyclient.Config, conversation, action string, input any) (ambientagents.Snapshot, error) {
	var snapshot ambientagents.Snapshot
	endpoint, err := url.Parse(cfg.TunnelURL)
	if err != nil || endpoint.Host == "" || cfg.Bearer == "" {
		return snapshot, errAgentUXAmbientRequest
	}
	switch endpoint.Scheme {
	case "ws":
		endpoint.Scheme = "http"
	case "wss":
		endpoint.Scheme = "https"
	case "http", "https":
	default:
		return snapshot, errAgentUXAmbientRequest
	}
	endpoint.Path = ambientagents.Path
	endpoint.RawPath = ""
	endpoint.RawQuery = url.Values{"conversation": {conversation}}.Encode()
	endpoint.Fragment = ""
	method := http.MethodGet
	var body io.Reader
	if action != "" {
		if action != "control" && action != "grant" && action != "opt-out" {
			return snapshot, errAgentUXAmbientRequest
		}
		endpoint.Path += "/" + action
		method = http.MethodPost
		raw, err := json.Marshal(input)
		if err != nil {
			return snapshot, err
		}
		body = bytes.NewReader(raw)
	}
	r, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return snapshot, err
	}
	r.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	r.Header.Set("Content-Type", "application/json")
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(r)
	if err != nil {
		return snapshot, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return snapshot, errAgentUXAmbientRequest
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&snapshot); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

// A failed read preserves all server-delivered offers and leaves the chat
// timeline/composer outside this state untouched. Retry is an explicit action.
type agentUXAmbientClientState struct {
	Conversation string
	Snapshot     ambientagents.Snapshot
	Failed       bool
	Generation   uint64
}

func (s *agentUXAmbientClientState) begin(conversation string) uint64 {
	if s.Conversation != conversation {
		s.Snapshot = ambientagents.Snapshot{}
		s.Failed = false
	}
	s.Conversation = conversation
	s.Generation++
	return s.Generation
}

func (s *agentUXAmbientClientState) finish(conversation string, generation uint64, snapshot ambientagents.Snapshot, err error) bool {
	if s.Conversation != conversation || s.Generation != generation {
		return false
	}
	s.apply(conversation, snapshot, err)
	return true
}

func (s *agentUXAmbientClientState) apply(conversation string, snapshot ambientagents.Snapshot, err error) {
	if s.Conversation != conversation {
		return
	}
	if err != nil {
		s.Failed = true
		return
	}
	s.Snapshot = snapshot
	s.Failed = false
}
