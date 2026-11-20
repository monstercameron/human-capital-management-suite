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

// Issue 3916: the browser client of the Followed threads list. The list is
// read from GET /api/chat/followed-threads; following and unfollowing stay on
// the thread-follow RPC the thread header uses.

type followedThreadJSON struct {
	ConversationID string    `json:"conversation_id"`
	RootPostID     string    `json:"root_post_id"`
	RootSequence   uint64    `json:"root_sequence"`
	RootAuthorID   string    `json:"root_author_id"`
	RootBody       string    `json:"root_body"`
	RootRemoved    bool      `json:"root_removed"`
	ReplyCount     uint64    `json:"reply_count"`
	LastActivity   time.Time `json:"last_activity"`
	Revision       uint64    `json:"revision"`
}

type followedThreadsReply struct {
	HostTenantID string               `json:"host_tenant_id"`
	Threads      []followedThreadJSON `json:"threads"`
}

func chatFollowedRequest(ctx context.Context, client *http.Client, cfg journeyclient.Config) (followedThreadsReply, error) {
	var reply followedThreadsReply
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" || client == nil {
		return reply, chatsaveError("unavailable")
	}
	endpoint.Path, endpoint.Fragment = "/api/chat/followed-threads", ""
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
		return followedThreadsReply{}, chatsaveError("unavailable")
	}
	return reply, nil
}

// chatFollowedRows turns the server's list into panel rows. The channel name is
// the one the rail shows; an identifier is never written in its place. The
// author is a name from the directory or the open conversation's members, and
// "Someone" otherwise.
func chatFollowedRows(reply followedThreadsReply, cfg journeyclient.Config, model chatui.Model, directory map[string]string, now time.Time) []chatui.FollowedThreadRow {
	rows := make([]chatui.FollowedThreadRow, 0, len(reply.Threads))
	for _, thread := range reply.Threads {
		if thread.ConversationID == "" || thread.RootPostID == "" {
			continue
		}
		row := chatui.FollowedThreadRow{ConversationID: thread.ConversationID, RootPostID: thread.RootPostID, Body: thread.RootBody, Removed: thread.RootRemoved,
			Replies: int(thread.ReplyCount), Sequence: thread.RootSequence, InChannel: chatsaveIsChannel(thread.ConversationID, model)}
		row.ActivityLabel = chatui.SavedTimeLabel(cfg.Locale, thread.LastActivity, now)
		for _, conversation := range model.Conversations {
			if conversation.ID == thread.ConversationID && strings.TrimSpace(conversation.Name) != "" && !chatsaveLooksLikeIdentifier(conversation.Name) {
				row.Channel = conversation.Name
				break
			}
		}
		if name := strings.TrimSpace(directory[thread.RootAuthorID]); name != "" && !chatsaveLooksLikeIdentifier(name) {
			row.Author = name
		}
		for _, member := range model.Members {
			if member.ID == thread.RootAuthorID && model.SelectedID == thread.ConversationID && strings.TrimSpace(member.Name) != "" {
				row.Author = member.Name
				break
			}
		}
		if thread.RootAuthorID == cfg.Subject && model.CurrentUserName != "" {
			row.Author = model.CurrentUserName
		}
		rows = append(rows, row)
	}
	return rows
}

// chatFollowedWithout is the list with one thread taken out, which is what the
// panel shows the moment Stop following succeeds.
func chatFollowedWithout(rows []chatui.FollowedThreadRow, conversation, root string) []chatui.FollowedThreadRow {
	out := make([]chatui.FollowedThreadRow, 0, len(rows))
	for _, row := range rows {
		if row.ConversationID == conversation && row.RootPostID == root {
			continue
		}
		out = append(out, row)
	}
	return out
}

// chatFollowedRevision is the follow row's revision the last list carried, which
// an unfollow from the list needs when the header has not loaded that thread.
func chatFollowedRevision(reply followedThreadsReply, conversation, root string) (uint64, bool) {
	for _, thread := range reply.Threads {
		if thread.ConversationID == conversation && thread.RootPostID == root {
			return thread.Revision, true
		}
	}
	return 0, false
}
