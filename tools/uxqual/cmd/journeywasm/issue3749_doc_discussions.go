package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3749: the browser side of a document's "Discussed in" list. The call is
// plain JSON over GET /api/chat/documents/{id}/discussions; nothing here
// decides which conversations a person may see, the server does, and it
// answers only with the ones this person can open.

type documentDiscussionJSON struct {
	ConversationID string `json:"conversation_id"`
	Name           string `json:"name"`
	Direct         bool   `json:"direct"`
	Messages       int    `json:"messages"`
	LatestAt       string `json:"latest_at"`
	Href           string `json:"href"`
}

type documentDiscussionsReply struct {
	Items      []documentDiscussionJSON `json:"items"`
	Incomplete bool                     `json:"incomplete"`
}

// documentDiscussionsCall reads one document's discussions. A refusal is
// reduced to the server's short code and its body is never shown.
func documentDiscussionsCall(ctx context.Context, client *http.Client, cfg journeyclient.Config, documentID string) (productui.DocumentDiscussions, error) {
	var out productui.DocumentDiscussions
	if strings.TrimSpace(documentID) == "" || strings.Contains(documentID, "/") {
		return out, chatworkError("invalid_argument")
	}
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" || client == nil {
		return out, chatworkError("unavailable")
	}
	endpoint.Path, endpoint.RawPath, endpoint.RawQuery, endpoint.Fragment = "/api/chat/documents/"+documentID+"/discussions", "/api/chat/documents/"+url.PathEscape(documentID)+"/discussions", "", ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	response, err := client.Do(req)
	if err != nil {
		return out, chatworkError("unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var refusal struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&refusal)
		if refusal.Code == "" {
			refusal.Code = "unavailable"
		}
		return out, chatworkError(refusal.Code)
	}
	var reply documentDiscussionsReply
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&reply); err != nil {
		return out, chatworkError("unavailable")
	}
	out.Incomplete = reply.Incomplete
	for _, item := range reply.Items {
		out.Items = append(out.Items, productui.DocumentDiscussion{
			ConversationID: item.ConversationID, Name: item.Name, Direct: item.Direct,
			Messages: item.Messages, LatestAt: item.LatestAt, Href: item.Href,
		})
	}
	return out, nil
}
