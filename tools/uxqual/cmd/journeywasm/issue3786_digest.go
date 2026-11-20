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

// Issue 3786: the browser client of the bell's digest. The choice and the
// digest are read from GET /api/chat/digest; a new choice or "seen" is posted to
// the same route. The server takes the person from the session, never from this
// request, and sends names of people and places but no message text.

type chatDigestItemJSON struct {
	Why                string    `json:"why"`
	ConversationID     string    `json:"conversation_id"`
	ConversationName   string    `json:"conversation_name"`
	ConversationKind   string    `json:"conversation_kind"`
	PostID             string    `json:"post_id"`
	Sequence           uint64    `json:"sequence"`
	ThreadRootID       string    `json:"thread_root_id"`
	ThreadRootSequence uint64    `json:"thread_root_sequence"`
	AuthorID           string    `json:"author_id"`
	At                 time.Time `json:"at"`
	Count              int       `json:"count"`
}

type chatDigestReply struct {
	HostTenantID string `json:"host_tenant_id"`
	Pref         struct {
		Cadence  string `json:"cadence"`
		Minute   int    `json:"minute"`
		Timezone string `json:"timezone"`
	} `json:"pref"`
	Due     bool                 `json:"due"`
	Through time.Time            `json:"through"`
	Items   []chatDigestItemJSON `json:"items"`
}

type chatDigestCommand struct {
	Host     string     `json:"host"`
	Action   string     `json:"action"`
	Cadence  string     `json:"cadence,omitempty"`
	Minute   int        `json:"minute,omitempty"`
	Timezone string     `json:"timezone,omitempty"`
	Through  *time.Time `json:"through,omitempty"`
}

func chatDigestEndpoint(cfg journeyclient.Config) (*url.URL, error) {
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" {
		return nil, chatsaveError("unavailable")
	}
	endpoint.Path, endpoint.Fragment, endpoint.RawQuery = "/api/chat/digest", "", ""
	return endpoint, nil
}

func chatDigestRequest(ctx context.Context, client *http.Client, cfg journeyclient.Config) (chatDigestReply, error) {
	var reply chatDigestReply
	endpoint, err := chatDigestEndpoint(cfg)
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
		return chatDigestReply{}, chatsaveError("unavailable")
	}
	return reply, nil
}

func chatDigestPost(ctx context.Context, client *http.Client, cfg journeyclient.Config, command chatDigestCommand) error {
	endpoint, err := chatDigestEndpoint(cfg)
	if err != nil || client == nil {
		return chatsaveError("unavailable")
	}
	command.Host = cfg.Tenant
	payload, err := json.Marshal(command)
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

// chatDigestView turns the server's reply into the bell's digest. name gives
// the name the person sees for an author id, or "" when it is not known; an
// identifier is never written in a name's place.
func chatDigestView(reply chatDigestReply, name func(string) string) *productui.ChatDigest {
	out := &productui.ChatDigest{Cadence: reply.Pref.Cadence, Minute: reply.Pref.Minute, Timezone: reply.Pref.Timezone, Due: reply.Due, Through: reply.Through}
	for _, item := range reply.Items {
		if item.ConversationID == "" || item.PostID == "" {
			continue
		}
		author := ""
		if name != nil {
			author = strings.TrimSpace(name(item.AuthorID))
		}
		if chatsaveLooksLikeIdentifier(author) {
			author = ""
		}
		place := strings.TrimSpace(item.ConversationName)
		if chatsaveLooksLikeIdentifier(place) || item.ConversationKind == "DIRECT" {
			place = ""
		}
		out.Items = append(out.Items, productui.ChatDigestItem{Why: item.Why, ConversationID: item.ConversationID, ConversationName: place,
			PostID: item.PostID, Sequence: item.Sequence, ThreadRootID: item.ThreadRootID, ThreadRootSequence: item.ThreadRootSequence,
			AuthorID: item.AuthorID, AuthorName: author, At: item.At, Count: item.Count})
	}
	return out
}

// chatDigestFeed is the bell's digest over the chat route. zone gives the
// browser's time zone, sent with a new daily choice so "8 o'clock" means the
// person's own.
func chatDigestFeed(active func() journeyclient.Config, directory func() map[string]string, zone func() string) *productclient.ChatDigestFeed {
	return &productclient.ChatDigestFeed{
		Read: func(ctx context.Context) (*productui.ChatDigest, error) {
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			reply, err := chatDigestRequest(ctx, http.DefaultClient, active())
			if err != nil {
				return nil, err
			}
			var names map[string]string
			if directory != nil {
				names = directory()
			}
			return chatDigestView(reply, func(id string) string { return names[id] }), nil
		},
		Save: func(ctx context.Context, cadence string, minute int) error {
			tz := "UTC"
			if zone != nil {
				if z := strings.TrimSpace(zone()); z != "" {
					tz = z
				}
			}
			return chatDigestPost(ctx, http.DefaultClient, active(), chatDigestCommand{Action: "set", Cadence: cadence, Minute: minute, Timezone: tz})
		},
		Seen: func(ctx context.Context, through time.Time) error {
			return chatDigestPost(ctx, http.DefaultClient, active(), chatDigestCommand{Action: "seen", Through: &through})
		},
	}
}
