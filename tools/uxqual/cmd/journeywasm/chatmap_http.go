package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

type ChatmapRequest struct {
	TenantID, ConversationID, PostID, ID string
	PostRevision                         uint64
	Place                                chat.LocationPlace
	ExpiresAt                            any
	Zoom                                 int
	Size                                 chat.MapSize
	Theme                                chat.MapTheme
	Query                                string
}

func ChatmapRequestHTTP(ctx context.Context, client *http.Client, cfg journeyclient.Config, action string, body ChatmapRequest) ([]byte, error) {
	// The WASM caller supplies window.location.origin; a redirect cannot relay
	// this sensitive body or its bearer credential to a different origin.
	endpoint, err := url.Parse(cfg.TunnelURL)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || cfg.Bearer == "" {
		return nil, chat.ErrUnavailable
	}
	switch action {
	case "attach", "read", "end", "picture", "sites", "lookup", "sharing", "message":
	default:
		return nil, chat.ErrInvalidArgument
	}
	endpoint.Path = "/api/chat/locations/v1/" + action
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	req.Header.Set("Content-Type", "application/json")
	local := *client
	local.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := local.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == 403 || response.StatusCode == 401 {
			return nil, chat.ErrPermissionDenied
		}
		if response.StatusCode == 404 {
			return nil, chat.ErrNotFound
		}
		if response.StatusCode == 400 {
			return nil, chat.ErrInvalidArgument
		}
		if response.StatusCode == 409 {
			return nil, chat.ErrConflict
		}
		return nil, chat.ErrUnavailable
	}
	if action == "picture" && !strings.HasPrefix(response.Header.Get("Content-Type"), "image/svg+xml") {
		return nil, chat.ErrUnavailable
	}
	return io.ReadAll(io.LimitReader(response.Body, 64<<10))
}
