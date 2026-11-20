package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3726: the browser side of a journey's "Discussed in" list. The calls
// are plain JSON over /api/chat/journeys/{id}/discussions; nothing here decides
// which conversations a person may see, the server does, and it answers only
// with the ones this person can open.

type journeyDiscussionJSON struct {
	ConversationID string `json:"conversation_id"`
	Name           string `json:"name"`
	Direct         bool   `json:"direct"`
	SharedAt       string `json:"shared_at"`
	Href           string `json:"href"`
}

type journeyDiscussionsReply struct {
	Items      []journeyDiscussionJSON `json:"items"`
	Incomplete bool                    `json:"incomplete"`
}

// journeyDiscussionsCall reads (GET) or records (POST, with a conversation id)
// one journey's discussions. A refusal is reduced to the server's short code
// and its body is never shown.
func journeyDiscussionsCall(ctx context.Context, client *http.Client, cfg journeyclient.Config, journeyID, recordConversation string) (journeyDiscussionsReply, error) {
	var reply journeyDiscussionsReply
	if strings.TrimSpace(journeyID) == "" || strings.Contains(journeyID, "/") {
		return reply, chatworkError("invalid_argument")
	}
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" || client == nil {
		return reply, chatworkError("unavailable")
	}
	endpoint.Path, endpoint.RawPath, endpoint.RawQuery, endpoint.Fragment = "/api/chat/journeys/"+journeyID+"/discussions", "/api/chat/journeys/"+url.PathEscape(journeyID)+"/discussions", "", ""
	method := http.MethodGet
	var body io.Reader
	if recordConversation != "" {
		encoded, err := json.Marshal(map[string]string{"conversation_id": recordConversation})
		if err != nil {
			return reply, err
		}
		method, body = http.MethodPost, bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return reply, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(req)
	if err != nil {
		return reply, chatworkError("unavailable")
	}
	defer response.Body.Close()
	switch {
	case method == http.MethodPost && response.StatusCode == http.StatusNoContent:
		return reply, nil
	case response.StatusCode != http.StatusOK:
		var refusal struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&refusal)
		if refusal.Code == "" {
			refusal.Code = "unavailable"
		}
		return journeyDiscussionsReply{}, chatworkError(refusal.Code)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&reply); err != nil {
		return journeyDiscussionsReply{}, chatworkError("unavailable")
	}
	return reply, nil
}

// journeyIDFromHref is the journey a same-origin journey address names, or "".
func journeyIDFromHref(href string) string {
	parsed, err := url.Parse(href)
	if err != nil || parsed.Path != "/workspace/app/journeys" {
		return ""
	}
	return strings.TrimSpace(parsed.Query().Get("journey"))
}

// journeyDiscussedRow is what one "Discussed in" link says and where it goes.
type journeyDiscussedRow struct{ Label, Aria, Href string }

// journeyDiscussedRows turns the server's answer into link rows. Only a
// same-origin chat address is ever linked; a direct message carries no stored
// title, so it is named by the page's own word for it.
func journeyDiscussedRows(items []journeyDiscussionJSON, directLabel, openTemplate string) []journeyDiscussedRow {
	rows := make([]journeyDiscussedRow, 0, len(items))
	for _, item := range items {
		if !strings.HasPrefix(item.Href, "/workspace/app/chat") || strings.HasPrefix(item.Href, "//") {
			continue
		}
		label := strings.TrimSpace(item.Name)
		if item.Direct || label == "" {
			label = directLabel
		}
		rows = append(rows, journeyDiscussedRow{Label: label, Aria: strings.ReplaceAll(openTemplate, "{name}", label), Href: item.Href})
	}
	return rows
}
