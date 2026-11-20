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

// Issue 3787: the browser client of the Follow-ups panel. The list is read from
// GET /api/chat/follow-ups and a choice is posted to the same route; the server
// takes the person from the session, never from this request.

type followUpJSON struct {
	ConversationID string     `json:"conversation_id"`
	PostID         string     `json:"post_id"`
	Sequence       uint64     `json:"sequence"`
	Direction      string     `json:"direction"`
	AuthorID       string     `json:"author_id"`
	ThreadRootID   string     `json:"thread_root_id"`
	Body           string     `json:"body"`
	AskedAt        time.Time  `json:"asked_at"`
	WaitingOn      []string   `json:"waiting_on"`
	Evidence       string     `json:"evidence"`
	EvidenceAt     *time.Time `json:"evidence_at"`
	Kept           bool       `json:"kept"`
}

type followUpsReply struct {
	HostTenantID string         `json:"host_tenant_id"`
	Owed         []followUpJSON `json:"owed"`
	Waiting      []followUpJSON `json:"waiting"`
}

type followUpChoiceBody struct {
	Host           string `json:"host"`
	ConversationID string `json:"conversation_id"`
	PostID         string `json:"post_id"`
	Direction      string `json:"direction"`
	Action         string `json:"action"`
	Until          string `json:"until,omitempty"`
}

// chatFollowUpSnooze is how long Snooze a day hides a row.
const chatFollowUpSnooze = 24 * time.Hour

func chatFollowUpsRequest(ctx context.Context, client *http.Client, cfg journeyclient.Config) (followUpsReply, error) {
	var reply followUpsReply
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" || client == nil {
		return reply, chatsaveError("unavailable")
	}
	endpoint.Path, endpoint.Fragment = "/api/chat/follow-ups", ""
	endpoint.RawQuery = url.Values{"host": {cfg.Tenant}}.Encode()
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
		return followUpsReply{}, chatsaveError("unavailable")
	}
	return reply, nil
}

// chatFollowUpChoose records the person's choice. action is the panel's word
// (snooze, done, dismiss or keep); done is "resolve" on the server.
func chatFollowUpChoose(ctx context.Context, client *http.Client, cfg journeyclient.Config, conversation, post, direction, action string, now time.Time) error {
	body := followUpChoiceBody{Host: cfg.Tenant, ConversationID: conversation, PostID: post, Direction: direction}
	switch action {
	case "snooze":
		body.Action, body.Until = "snooze", now.Add(chatFollowUpSnooze).UTC().Format(time.RFC3339)
	case "done":
		body.Action = "resolve"
	case "dismiss", "keep":
		body.Action = action
	default:
		return chatsaveError("invalid_argument")
	}
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" || client == nil {
		return chatsaveError("unavailable")
	}
	endpoint.Path, endpoint.Fragment, endpoint.RawQuery = "/api/chat/follow-ups", "", ""
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return chatsaveError("unavailable")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<16))
	if response.StatusCode != http.StatusOK {
		return chatsaveError("unavailable")
	}
	return nil
}

// chatFollowUpRows turns the server's two lists into panel rows. Names are the
// ones the person sees (directory, open conversation members); an identifier is
// never written in a name's place, so an unknown person reads as "Someone".
func chatFollowUpRows(reply followUpsReply, cfg journeyclient.Config, model chatui.Model, directory map[string]string, now time.Time) []chatui.FollowUpRow {
	name := func(id string) string {
		if id == cfg.Subject && model.CurrentUserName != "" {
			return model.CurrentUserName
		}
		for _, member := range model.Members {
			if member.ID == id && strings.TrimSpace(member.Name) != "" && !chatsaveLooksLikeIdentifier(member.Name) {
				return strings.TrimSpace(member.Name)
			}
		}
		if n := strings.TrimSpace(directory[id]); n != "" && !chatsaveLooksLikeIdentifier(n) {
			return n
		}
		return ""
	}
	rows := make([]chatui.FollowUpRow, 0, len(reply.Owed)+len(reply.Waiting))
	for _, list := range [][]followUpJSON{reply.Owed, reply.Waiting} {
		for _, item := range list {
			if item.ConversationID == "" || item.PostID == "" {
				continue
			}
			row := chatui.FollowUpRow{ConversationID: item.ConversationID, PostID: item.PostID, Direction: item.Direction, Body: item.Body,
				Evidence: item.Evidence, Kept: item.Kept, Sequence: item.Sequence, ThreadRootID: item.ThreadRootID,
				InChannel: chatsaveIsChannel(item.ConversationID, model)}
			if hours := int(now.Sub(item.AskedAt) / time.Hour); hours > 0 {
				row.AgeHours = hours
			}
			for _, conversation := range model.Conversations {
				if conversation.ID == item.ConversationID && strings.TrimSpace(conversation.Name) != "" && !chatsaveLooksLikeIdentifier(conversation.Name) {
					row.Channel = conversation.Name
					break
				}
			}
			if item.Direction == "owed" {
				row.Who = name(item.AuthorID)
			} else {
				names := []string{}
				for _, id := range item.WaitingOn {
					if n := name(id); n != "" {
						names = append(names, n)
					}
				}
				row.Who = strings.Join(names, ", ")
			}
			rows = append(rows, row)
		}
	}
	return rows
}

// chatFollowUpsWithout is the list with one row taken out, which is what the
// panel shows the moment a choice that hides the row succeeds.
func chatFollowUpsWithout(rows []chatui.FollowUpRow, conversation, post, direction string) []chatui.FollowUpRow {
	out := make([]chatui.FollowUpRow, 0, len(rows))
	for _, row := range rows {
		if row.ConversationID == conversation && row.PostID == post && row.Direction == direction {
			continue
		}
		out = append(out, row)
	}
	return out
}

// chatFollowUpsMarkKept marks one row as kept open, which is what the panel
// shows the moment Keep open succeeds.
func chatFollowUpsMarkKept(rows []chatui.FollowUpRow, conversation, post, direction string) []chatui.FollowUpRow {
	out := append([]chatui.FollowUpRow(nil), rows...)
	for i := range out {
		if out[i].ConversationID == conversation && out[i].PostID == post && out[i].Direction == direction {
			out[i].Kept = true
		}
	}
	return out
}
