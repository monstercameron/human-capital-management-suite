package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3791: the browser side of "Work from this conversation". One plain JSON
// read of GET /api/chat/conversations/{id}/work; nothing here decides who may
// see a piece of work, the server asks each owner as the person reading.

type linkedworkItemJSON struct {
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	Title      string `json:"title"`
	State      string `json:"state"`
	Source     string `json:"source"`
	ObservedAt string `json:"observed_at"`
	Revision   uint64 `json:"revision"`
	Href       string `json:"href"`
	Detail     string `json:"detail"`
	OwnerID    string `json:"owner_id"`
	DueDate    string `json:"due_date"`
	ProjectID  string `json:"project_id"`
	TaskID     string `json:"task_id"`
}

type linkedworkReply struct {
	Items      []linkedworkItemJSON `json:"items"`
	Incomplete []string             `json:"incomplete"`
	AsOf       string               `json:"as_of"`
}

// linkedworkHides says a failed read means the panel is not offered here at
// all (no such conversation for this person, no permission, or no such route),
// as opposed to a temporary failure, which is shown as unknown with a retry.
func linkedworkHides(err error) bool {
	switch chatworkErrorCode(err) {
	case "not_found", "permission_denied", "method_not_allowed", "invalid_argument":
		return true
	}
	return false
}

func linkedworkTime(raw string) time.Time {
	at, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return at
}

// linkedworkRead reads the work of one conversation as the panel shows it.
func linkedworkRead(ctx context.Context, client *http.Client, cfg journeyclient.Config, conversation string) (chatui.LinkedWorkState, error) {

	reply, err := linkedworkCall(ctx, client, cfg, conversation)
	if err != nil {
		return chatui.LinkedWorkState{}, err
	}
	state := chatui.LinkedWorkState{Enabled: true, State: "ready", AsOf: linkedworkTime(reply.AsOf)}
	for _, item := range reply.Items {
		if item.ID == "" || (item.Kind != "intent" && item.Kind != "task") || strings.TrimSpace(item.Title) == "" && item.Kind == "task" {
			continue
		}
		state.Items = append(state.Items, chatui.LinkedWorkItem{
			Kind: item.Kind, ID: item.ID, Title: item.Title, State: item.State, Source: item.Source,
			ObservedAt: linkedworkTime(item.ObservedAt), Revision: item.Revision, Href: item.Href, Detail: item.Detail,
			OwnerID: item.OwnerID, DueDate: item.DueDate, ProjectID: item.ProjectID, TaskID: item.TaskID,
		})
	}
	for _, source := range reply.Incomplete {
		if source == "approvals" || source == "tasks" {
			state.Incomplete = append(state.Incomplete, source)
		}
	}
	return state, nil
}

// linkedworkCall makes the one GET. A non-200 answer is reduced to the
// server's short code; the body of a refusal is never shown.
func linkedworkCall(ctx context.Context, client *http.Client, cfg journeyclient.Config, conversation string) (linkedworkReply, error) {
	var reply linkedworkReply
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" || client == nil {
		return reply, chatworkError("unavailable")
	}
	endpoint.Path = "/api/chat/conversations/" + conversation + "/work"
	endpoint.RawPath = "/api/chat/conversations/" + url.PathEscape(conversation) + "/work"
	endpoint.RawQuery, endpoint.Fragment = "", ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return reply, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	response, err := client.Do(req)
	if err != nil {
		return reply, chatworkError("unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var refusal struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&refusal)
		if refusal.Code == "" {
			// A server without the route answers 404 with no body: the panel is not offered.
			switch response.StatusCode {
			case http.StatusNotFound:
				refusal.Code = "not_found"
			case http.StatusForbidden:
				refusal.Code = "permission_denied"
			default:
				refusal.Code = "unavailable"
			}
		}
		return linkedworkReply{}, chatworkError(refusal.Code)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&reply); err != nil {
		return linkedworkReply{}, chatworkError("unavailable")
	}
	return reply, nil
}
