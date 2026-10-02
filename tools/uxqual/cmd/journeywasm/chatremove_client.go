package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

type chatremoveClientInput struct {
	HostTenantID                                                    string
	Removal                                                         chat.RemovalRequest
	ConversationID, PostID, CaseID, Action, ReasonCode, Note, Query string
	// Role, Permission and Allowed are one cell of the permissions table
	// (chatmod005_permissions.go); they are left out of every other request.
	Role       string `json:",omitempty"`
	Permission string `json:",omitempty"`
	Allowed    bool   `json:",omitempty"`
}

func chatremovePageRequest(ctx context.Context, client *http.Client, cfg journeyclient.Config, href string) (string, error) {
	if client == nil || cfg.Bearer == "" {
		return "", chat.ErrUnavailable
	}
	page, err := url.Parse(href)
	if err != nil || page.IsAbs() || page.Host != "" || page.Path != "/api/chat/moderation/page" || len(page.RawQuery) > 8000 {
		return "", chat.ErrInvalidArgument
	}
	endpoint, err := personaChatURL(cfg, page.Path, "")
	if err != nil {
		return "", chat.ErrUnavailable
	}
	target, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	target.RawQuery = page.RawQuery
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	response, err := client.Do(request)
	if err != nil {
		return "", chat.ErrUnavailable
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
	case 401, 403:
		return "", chat.ErrPermissionDenied
	case 404:
		return "", chat.ErrNotFound
	default:
		return "", chat.ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 || strings.Contains(strings.ToLower(string(body)), "<style") {
		return "", chat.ErrUnavailable
	}
	return string(body), nil
}

func chatremoveRequest(ctx context.Context, client *http.Client, cfg journeyclient.Config, action, query string, input any) ([]byte, error) {
	endpoint, err := url.Parse(cfg.TunnelURL)
	if err != nil || endpoint.Host == "" || cfg.Bearer == "" {
		return nil, chat.ErrUnavailable
	}
	switch endpoint.Scheme {
	case "ws":
		endpoint.Scheme = "http"
	case "wss":
		endpoint.Scheme = "https"
	case "http", "https":
	default:
		return nil, chat.ErrUnavailable
	}
	endpoint.Path, endpoint.RawQuery, endpoint.Fragment = "/api/chat/moderation", "", ""
	method := http.MethodPost
	switch action {
	case "", "notices", "summary":
		method = http.MethodGet
	case "preview", "apply", "report", "appeal", "resolve", "review", "permissions":
	default:
		return nil, chat.ErrInvalidArgument
	}
	if action != "" {
		endpoint.Path += "/" + action
	}
	if query != "" {
		endpoint.RawQuery = url.Values{"query": {query}}.Encode()
	}
	var body io.Reader
	if method == http.MethodPost {
		encoded, e := json.Marshal(input)
		if e != nil {
			return nil, e
		}
		body = bytes.NewReader(encoded)
	}
	r, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(r)
	if err != nil {
		return nil, chat.ErrUnavailable
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case 200:
	case 400:
		return nil, chat.ErrInvalidArgument
	case 401, 403:
		return nil, chat.ErrPermissionDenied
	case 409:
		return nil, chat.ErrConflict
	case 404:
		return nil, chat.ErrNotFound
	default:
		return nil, chat.ErrUnavailable
	}
	b, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil || !json.Valid(b) {
		return nil, chat.ErrUnavailable
	}
	return b, nil
}

func chatremoveFormInput(action string, values map[string]string, ids []string, location *time.Location) (chatremoveClientInput, error) {
	input := chatremoveClientInput{HostTenantID: values["tenant"], ConversationID: values["conversation"], PostID: values["post"], CaseID: values["case"], Action: values["decision"], ReasonCode: values["reason"], Note: values["note"], Query: values["query"]}
	if action != "preview" && action != "apply" && action != "quick" {
		return input, nil
	}
	selection := chat.RemovalSelection{ConversationID: input.ConversationID, PostIDs: ids}
	if values["author"] == "" && len(ids) == 0 {
		// The form of ticked messages with nothing ticked: say so here, rather
		// than send a selection the server can only call invalid.
		return input, errChatmod004NothingSelected
	}
	if values["author"] != "" {
		selection.PostIDs = nil
		selection.AuthorID, selection.AuthorHomeTenantID = values["author"], values["author_home"]
		if location == nil {
			location = time.UTC
		}
		var err error
		selection.From, err = time.ParseInLocation("2006-01-02T15:04", values["from"], location)
		if err != nil {
			return input, chat.ErrInvalidArgument
		}
		selection.Until, err = time.ParseInLocation("2006-01-02T15:04", values["until"], location)
		if err != nil || !selection.Until.After(selection.From) {
			return input, chat.ErrInvalidArgument
		}
	}
	removalAction := values["removal_action"]
	if removalAction == "" {
		removalAction = "remove"
	}
	count, _ := strconv.Atoi(values["confirmed_count"])
	input.Removal = chat.RemovalRequest{Selection: selection, ReasonCode: input.ReasonCode, Note: input.Note, Action: removalAction, Confirmation: values["confirmation"], ConfirmedCount: count}
	return input, nil
}

func chatremoveErrorKey(err error) string {
	switch {
	case errors.Is(err, chat.ErrConflict):
		return "conflict"
	case errors.Is(err, errChatmod005NoteRequired):
		return "note_required"
	case errors.Is(err, errChatmod004NothingSelected):
		return "none_selected"
	case errors.Is(err, chat.ErrPermissionDenied):
		return "forbidden"
	case errors.Is(err, chat.ErrInvalidArgument):
		return "failed"
	}
	return "error"
}

func chatremoveRoute(path string) bool { return strings.TrimSuffix(path, "/") == "/chat/moderation" }
