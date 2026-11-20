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

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient"
)

// Issue 3733: the browser client of the bell's chat items. The list is read from
// GET /api/chat/notifications and a read choice is posted to the same route; the
// server takes the person from the session, never from this request, and keeps
// the read state, so a read in one session shows in every other.

type chatNotificationJSON struct {
	ID                 string    `json:"id"`
	Kind               string    `json:"kind"`
	ConversationID     string    `json:"conversation_id"`
	ConversationName   string    `json:"conversation_name"`
	ConversationKind   string    `json:"conversation_kind"`
	PostID             string    `json:"post_id"`
	Sequence           uint64    `json:"sequence"`
	ThreadRootID       string    `json:"thread_root_id"`
	ThreadRootSequence uint64    `json:"thread_root_sequence"`
	AuthorID           string    `json:"author_id"`
	CoveringID         string    `json:"covering_id"`
	Body               string    `json:"body"`
	At                 time.Time `json:"at"`
	Read               bool      `json:"read"`
}

type chatNotificationsReply struct {
	HostTenantID string                 `json:"host_tenant_id"`
	Unread       int                    `json:"unread"`
	Items        []chatNotificationJSON `json:"items"`
}

type chatNotificationsReadBody struct {
	Host string   `json:"host"`
	IDs  []string `json:"ids,omitempty"`
	All  bool     `json:"all,omitempty"`
}

func chatNotificationsEndpoint(cfg journeyclient.Config) (*url.URL, error) {
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" {
		return nil, chatsaveError("unavailable")
	}
	endpoint.Path, endpoint.Fragment, endpoint.RawQuery = "/api/chat/notifications", "", ""
	return endpoint, nil
}

func chatNotificationsRequest(ctx context.Context, client *http.Client, cfg journeyclient.Config) (chatNotificationsReply, error) {
	var reply chatNotificationsReply
	endpoint, err := chatNotificationsEndpoint(cfg)
	if err != nil || client == nil {
		return reply, chatsaveError("unavailable")
	}
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
		return chatNotificationsReply{}, chatsaveError("unavailable")
	}
	return reply, nil
}

// chatNotificationsMarkRead posts the person's read choice: the named items, or
// every item when all is true.
func chatNotificationsMarkRead(ctx context.Context, client *http.Client, cfg journeyclient.Config, ids []string, all bool) error {
	endpoint, err := chatNotificationsEndpoint(cfg)
	if err != nil || client == nil {
		return chatsaveError("unavailable")
	}
	payload, err := json.Marshal(chatNotificationsReadBody{Host: cfg.Tenant, IDs: ids, All: all})
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

// chatNotificationItems turns the server's list into the bell's items. name
// gives the name the person sees for an author id, or "" when it is not known;
// an identifier is never written in a name's place.
func chatNotificationItems(reply chatNotificationsReply, name func(string) string) []productui.ChatNotification {
	items := make([]productui.ChatNotification, 0, len(reply.Items))
	for _, item := range reply.Items {
		if item.ID == "" || item.ConversationID == "" {
			continue
		}
		author := ""
		if name != nil {
			author = strings.TrimSpace(name(item.AuthorID))
		}
		covering := ""
		if name != nil && item.CoveringID != "" {
			covering = strings.TrimSpace(name(item.CoveringID))
			if chatsaveLooksLikeIdentifier(covering) {
				covering = ""
			}
		}
		// A cover notice says where and who, never what (issue 3789).
		body := item.Body
		if item.Kind == "cover" {
			body = ""
		}
		if chatsaveLooksLikeIdentifier(author) {
			author = ""
		}
		place := strings.TrimSpace(item.ConversationName)
		if chatsaveLooksLikeIdentifier(place) || item.ConversationKind == "DIRECT" {
			place = ""
		}
		items = append(items, productui.ChatNotification{
			ID: item.ID, Kind: item.Kind, ConversationID: item.ConversationID, ConversationName: place,
			PostID: item.PostID, Sequence: item.Sequence, ThreadRootID: item.ThreadRootID, ThreadRootSequence: item.ThreadRootSequence,
			AuthorID: item.AuthorID, AuthorName: author, CoveringID: item.CoveringID, CoveringName: covering, Body: body, CreatedAt: item.At, Read: item.Read,
		})
	}
	return items
}

// chatNotificationFeed is the bell's feed over the chat route. active gives the
// connection settings at the time of each call, so a refreshed session token is
// used; directory gives the names the person sees, by author id.
func chatNotificationFeed(active func() journeyclient.Config, directory func() map[string]string) *productclient.ChatNotificationFeed {
	return &productclient.ChatNotificationFeed{
		List: func(ctx context.Context) ([]productui.ChatNotification, error) {
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			reply, err := chatNotificationsRequest(ctx, http.DefaultClient, active())
			if err != nil {
				return nil, err
			}
			var names map[string]string
			if directory != nil {
				names = directory()
			}
			return chatNotificationItems(reply, func(id string) string { return names[id] }), nil
		},
		MarkRead: func(ctx context.Context, ids []string, all bool) error {
			return chatNotificationsMarkRead(ctx, http.DefaultClient, active(), ids, all)
		},
	}
}
