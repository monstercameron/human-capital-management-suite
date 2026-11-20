package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3815: the browser side of acknowledgements. Three plain JSON calls:
// read what this reader may do with the posts on screen, mark a post of their
// own, and acknowledge one. Nothing here decides who may do what: the server
// does, and the client shows its word.

type ackViewJSON struct {
	PostID         string   `json:"post_id"`
	CanAcknowledge bool     `json:"can_acknowledge"`
	Acknowledged   bool     `json:"acknowledged"`
	AcknowledgedAt string   `json:"acknowledged_at"`
	CanManage      bool     `json:"can_manage"`
	Total          int      `json:"total"`
	AckCount       int      `json:"ack_count"`
	Pending        []string `json:"pending"`
	PendingMore    int      `json:"pending_more"`
}

type ackReply struct {
	Views      map[string]ackViewJSON `json:"views"`
	CanRequire bool                   `json:"can_require"`
	View       *ackViewJSON           `json:"view"`
}

// ackError is a refusal reduced to the server's short code.
type ackError struct{ Code string }

func (e *ackError) Error() string { return e.Code }

func ackView(raw ackViewJSON) chatui.AckView {
	view := chatui.AckView{Loaded: true, CanAcknowledge: raw.CanAcknowledge && !raw.Acknowledged, Acknowledged: raw.Acknowledged,
		CanManage: raw.CanManage, Total: raw.Total, Count: raw.AckCount, Pending: raw.Pending, PendingMore: raw.PendingMore}
	if at, err := time.Parse(time.RFC3339, raw.AcknowledgedAt); err == nil {
		view.AcknowledgedAt = at
	}
	return view
}

func ackPaths(conversation string) (string, string) {
	return "/api/chat/conversations/" + conversation + "/acks", "/api/chat/conversations/" + url.PathEscape(conversation) + "/acks"
}

func ackCall(ctx context.Context, client *http.Client, cfg journeyclient.Config, method, path, rawPath string, query url.Values) (ackReply, error) {
	var reply ackReply
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" || client == nil {
		return reply, &ackError{Code: "unavailable"}
	}
	endpoint.Path, endpoint.RawPath, endpoint.RawQuery, endpoint.Fragment = path, rawPath, query.Encode(), ""
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), nil)
	if err != nil {
		return reply, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	response, err := client.Do(req)
	if err != nil {
		return reply, &ackError{Code: "unavailable"}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var refusal struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(&refusal)
		if refusal.Code == "" {
			switch response.StatusCode {
			case http.StatusNotFound:
				refusal.Code = "not_found"
			case http.StatusForbidden:
				refusal.Code = "permission_denied"
			default:
				refusal.Code = "unavailable"
			}
		}
		return ackReply{}, &ackError{Code: refusal.Code}
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&reply); err != nil {
		return ackReply{}, &ackError{Code: "unavailable"}
	}
	return reply, nil
}

// ackRead asks what the reader may do with each of the given posts. A post the
// server does not describe does not require acknowledgement (or is not one this
// reader can open): it is absent from the answer.
func ackRead(ctx context.Context, client *http.Client, cfg journeyclient.Config, conversation string, posts []string) (map[string]chatui.AckView, bool, error) {
	path, raw := ackPaths(conversation)
	reply, err := ackCall(ctx, client, cfg, http.MethodGet, path, raw, url.Values{"post": posts})
	if err != nil {
		return nil, false, err
	}
	out := map[string]chatui.AckView{}
	for id, view := range reply.Views {
		out[id] = ackView(view)
	}
	return out, reply.CanRequire, nil
}

func ackAct(ctx context.Context, client *http.Client, cfg journeyclient.Config, conversation, post, action string) (chatui.AckView, error) {
	path, raw := ackPaths(conversation)
	reply, err := ackCall(ctx, client, cfg, http.MethodPost, path+"/"+post+"/"+action, raw+"/"+url.PathEscape(post)+"/"+action, nil)
	if err != nil {
		return chatui.AckView{}, err
	}
	if reply.View == nil {
		return chatui.AckView{}, &ackError{Code: "unavailable"}
	}
	return ackView(*reply.View), nil
}

// ackRequire marks a post of the reader's own as needing acknowledgement.
func ackRequire(ctx context.Context, client *http.Client, cfg journeyclient.Config, conversation, post string) (chatui.AckView, error) {
	return ackAct(ctx, client, cfg, conversation, post, "require")
}

// ackAcknowledge acknowledges one post.
func ackAcknowledge(ctx context.Context, client *http.Client, cfg journeyclient.Config, conversation, post string) (chatui.AckView, error) {
	return ackAct(ctx, client, cfg, conversation, post, "acknowledge")
}
