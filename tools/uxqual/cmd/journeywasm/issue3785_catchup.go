package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3785: the browser client of "Catch me up". The brief is read with one
// POST to /api/chat/catch-up/v1. The server takes the person from the session,
// never from this request, reads only what that person may read, and neither
// writes a message nor moves the read position. A failure is a failed brief,
// never an empty one: "nothing missed" is only ever the server's own answer.

type chatCatchUpLineJSON struct {
	PostID       string `json:"post_id"`
	Sequence     uint64 `json:"sequence"`
	ThreadRootID string `json:"thread_root_id"`
	AuthorID     string `json:"author_id"`
	Text         string `json:"text"`
}

type chatCatchUpReply struct {
	ConversationID string    `json:"conversation_id"`
	ThreadRootID   string    `json:"thread_root_id"`
	Cursor         uint64    `json:"cursor"`
	After          uint64    `json:"after"`
	Through        uint64    `json:"through"`
	CoveredFrom    uint64    `json:"covered_from"`
	Total          int       `json:"total"`
	Covered        int       `json:"covered"`
	Omitted        int       `json:"omitted"`
	GeneratedAt    time.Time `json:"generated_at"`
	Sections       []struct {
		Kind  string                `json:"kind"`
		More  int                   `json:"more"`
		Lines []chatCatchUpLineJSON `json:"lines"`
	} `json:"sections"`
}

type chatCatchUpCommand struct {
	Host           string `json:"host"`
	ConversationID string `json:"conversation_id"`
	ThreadRootID   string `json:"thread_root_id,omitempty"`
	SincePostID    string `json:"since_post_id,omitempty"`
}

// chatCatchUpFailure names a failed request for the card: "denied" (the person
// may no longer open the conversation), "gone" (the message the range starts
// from can no longer be read) or "unavailable".
func chatCatchUpFailure(err error) string {
	switch chatsaveErrorCode(err) {
	case "denied", "gone":
		return chatsaveErrorCode(err)
	}
	return "unavailable"
}

func chatCatchUpRequest(ctx context.Context, client *http.Client, cfg journeyclient.Config, conversation, threadRootID, sincePostID string) (chatCatchUpReply, error) {
	var reply chatCatchUpReply
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" || client == nil {
		return reply, chatsaveError("unavailable")
	}
	if strings.TrimSpace(conversation) == "" {
		return reply, chatsaveError("unavailable")
	}
	endpoint.Path, endpoint.Fragment, endpoint.RawQuery = "/api/chat/catch-up/v1", "", ""
	payload, err := json.Marshal(chatCatchUpCommand{Host: cfg.Tenant, ConversationID: conversation, ThreadRootID: threadRootID, SincePostID: sincePostID})
	if err != nil {
		return reply, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return reply, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return reply, chatsaveError("unavailable")
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden:
		return reply, chatsaveError("denied")
	case http.StatusNotFound:
		return reply, chatsaveError("gone")
	default:
		return reply, chatsaveError("unavailable")
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&reply); err != nil {
		return chatCatchUpReply{}, chatsaveError("unavailable")
	}
	return reply, nil
}

// chatCatchUpView turns the server's reply into the card's brief. A line that
// names no message, and a kind the page has no heading for, are left out: every
// line the card shows can be opened.
func chatCatchUpView(reply chatCatchUpReply) chatui.CatchUpBrief {
	out := chatui.CatchUpBrief{Phase: chatui.CatchUpReady, ConversationID: reply.ConversationID, ThreadRootID: reply.ThreadRootID,
		Through: reply.Through, Cursor: reply.Cursor, After: reply.After, CoveredFrom: reply.CoveredFrom,
		Total: reply.Total, Covered: reply.Covered, Omitted: reply.Omitted, GeneratedAt: reply.GeneratedAt}
	for _, section := range reply.Sections {
		switch section.Kind {
		case "asked_you", "decisions", "work":
		default:
			continue
		}
		next := chatui.CatchUpSection{Kind: section.Kind, More: section.More}
		for _, line := range section.Lines {
			if line.PostID == "" || line.Sequence == 0 || strings.TrimSpace(line.Text) == "" {
				continue
			}
			next.Lines = append(next.Lines, chatui.CatchUpLine{PostID: line.PostID, Sequence: line.Sequence, ThreadRootID: line.ThreadRootID, AuthorID: line.AuthorID, Text: line.Text})
		}
		if len(next.Lines) > 0 || next.More > 0 {
			out.Sections = append(out.Sections, next)
		}
	}
	return out
}

// chatCatchUpFailed is the card of a brief that could not be made. It holds no
// content from an earlier brief.
func chatCatchUpFailed(conversation, threadRootID, failure string) chatui.CatchUpBrief {
	return chatui.CatchUpBrief{Phase: chatui.CatchUpFailed, ConversationID: conversation, ThreadRootID: threadRootID, Error: failure}
}
