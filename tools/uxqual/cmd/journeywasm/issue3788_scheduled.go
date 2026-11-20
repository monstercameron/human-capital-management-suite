package main

import (
	"bytes"
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

// Issue 3788: the browser client of Send later. Messages are read from, kept
// in, changed in and removed from /api/chat/scheduled-posts; the server takes
// the person from the session, never from this request, and sends nothing
// until the time comes.

type scheduledPostJSON struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Body           string    `json:"body"`
	SendAt         time.Time `json:"send_at"`
	Zone           string    `json:"zone"`
	State          string    `json:"state"`
	Refusal        string    `json:"refusal"`
	Revision       uint64    `json:"revision"`
	QuietPeople    int       `json:"quiet_people"`
}

type scheduledPostsReply struct {
	HostTenantID string              `json:"host_tenant_id"`
	Posts        []scheduledPostJSON `json:"posts"`
}

type scheduledPostReply struct {
	Post scheduledPostJSON `json:"post"`
}

type scheduledPostBody struct {
	Host           string `json:"host"`
	ID             string `json:"id,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	Body           string `json:"body,omitempty"`
	SendAt         string `json:"send_at,omitempty"`
	Zone           string `json:"zone,omitempty"`
	Revision       uint64 `json:"revision,omitempty"`
}

const scheduledPostsPath = "/api/chat/scheduled-posts"

func chatScheduledEndpoint(cfg journeyclient.Config, withHost bool) (*url.URL, bool) {
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" {
		return nil, false
	}
	endpoint.Path, endpoint.Fragment, endpoint.RawQuery = scheduledPostsPath, "", ""
	if withHost {
		endpoint.RawQuery = url.Values{"host": {cfg.Tenant}}.Encode()
	}
	return endpoint, true
}

// chatScheduledList reads the person's own messages. A refusal is an error, not
// an empty list.
func chatScheduledList(ctx context.Context, client *http.Client, cfg journeyclient.Config) (scheduledPostsReply, error) {
	var reply scheduledPostsReply
	endpoint, ok := chatScheduledEndpoint(cfg, true)
	if !ok || client == nil {
		return reply, chatsaveError("unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return reply, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	response, err := client.Do(req)
	if err != nil {
		return reply, chatsaveError("unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return reply, chatsaveError("unavailable")
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&reply); err != nil {
		return scheduledPostsReply{}, chatsaveError("unavailable")
	}
	return reply, nil
}

// chatScheduledOutcome sorts the server's answer into the words the page
// uses: "saved", "limit" (the person holds the most allowed), "time" (the time
// is outside the window) or "failed".
func chatScheduledOutcome(status int) string {
	switch status {
	case http.StatusOK, http.StatusCreated:
		return "saved"
	case http.StatusUnprocessableEntity:
		return "limit"
	}
	return "failed"
}

// chatScheduledSend keeps (POST), changes (PUT) or removes (DELETE) one message.
func chatScheduledSend(ctx context.Context, client *http.Client, cfg journeyclient.Config, method string, body scheduledPostBody) (scheduledPostJSON, string) {
	endpoint, ok := chatScheduledEndpoint(cfg, false)
	if !ok || client == nil {
		return scheduledPostJSON{}, "failed"
	}
	body.Host = cfg.Tenant
	payload, err := json.Marshal(body)
	if err != nil {
		return scheduledPostJSON{}, "failed"
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return scheduledPostJSON{}, "failed"
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return scheduledPostJSON{}, "failed"
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	outcome := chatScheduledOutcome(response.StatusCode)
	if response.StatusCode == http.StatusBadRequest && strings.Contains(string(data), `"invalid_time"`) {
		outcome = "time"
	}
	if outcome != "saved" {
		return scheduledPostJSON{}, outcome
	}
	var reply scheduledPostReply
	_ = json.Unmarshal(data, &reply)
	return reply.Post, outcome
}

// chatScheduledRows turns the server's list into tray rows. A conversation is
// named only by what the person's own conversation list calls it; an
// identifier is never written in a name's place.
func chatScheduledRows(reply scheduledPostsReply, model chatui.Model) []chatui.ScheduledRow {
	rows := make([]chatui.ScheduledRow, 0, len(reply.Posts))
	for _, post := range reply.Posts {
		if post.ID == "" {
			continue
		}
		row := chatui.ScheduledRow{ID: post.ID, ConversationID: post.ConversationID, Body: post.Body, SendAt: post.SendAt, State: post.State,
			Refusal: post.Refusal, Revision: post.Revision, QuietPeople: post.QuietPeople, InChannel: chatsaveIsChannel(post.ConversationID, model)}
		for _, conversation := range model.Conversations {
			if conversation.ID == post.ConversationID && strings.TrimSpace(conversation.Name) != "" && !chatsaveLooksLikeIdentifier(conversation.Name) {
				row.Channel = conversation.Name
				break
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// chatScheduledWithout is the list with one row taken out, which is what the
// tray shows the moment a cancel succeeds.
func chatScheduledWithout(rows []chatui.ScheduledRow, id string) []chatui.ScheduledRow {
	out := make([]chatui.ScheduledRow, 0, len(rows))
	for _, row := range rows {
		if row.ID != id {
			out = append(out, row)
		}
	}
	return out
}
