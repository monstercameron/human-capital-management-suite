package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

type chatstateRefusal struct {
	Code string `json:"code"`
}

func (e chatstateRefusal) Error() string { return e.Code }

// chatstateClient uses the session already held by the chat client. Its HTTP
// transport is injectable, so tests never contact a server or model provider.
type chatstateClient struct {
	HTTP                    *http.Client
	BaseURL, Bearer, Tenant string
}

func (c chatstateClient) request(ctx context.Context, method, conversation string, body []byte) ([]byte, error) {
	if c.HTTP == nil || conversation == "" || c.Tenant == "" {
		return nil, chatstateRefusal{Code: "unavailable"}
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+"/api/chat/v1/channel-status/"+url.PathEscape(conversation), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.Bearer)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Cache-Control", "no-store")
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, chatstateRefusal{Code: "unavailable"}
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(payload) > 64<<10 {
		return nil, chatstateRefusal{Code: "unavailable"}
	}
	if response.StatusCode != http.StatusOK {
		refusal := chatstateRefusal{Code: "unavailable"}
		_ = json.Unmarshal(payload, &refusal)
		return nil, refusal
	}
	return payload, nil
}

func (c chatstateClient) Load(ctx context.Context, conversation string, previous chatui.ChannelStatusView) (chatui.ChannelStatusView, error) {
	view := previous
	fail := func(err error) (chatui.ChannelStatusView, error) {
		view.Unavailable = true
		view.Loading = false
		view.CanPost = false
		view.Transitions = nil
		return view, err
	}
	payload, err := c.request(ctx, http.MethodGet, conversation, nil)
	if err != nil {
		return fail(err)
	}
	var snapshot chat.ChannelStatusSnapshot
	if json.Unmarshal(payload, &snapshot) != nil || snapshot.Status.TenantID != c.Tenant || snapshot.Status.ConversationID != conversation || snapshot.Status.Revision == 0 {
		return fail(errChatstateEvent)
	}
	statusPayload, err := json.Marshal(snapshot.Status)
	if err != nil {
		return fail(err)
	}
	validated, err := applyChatstateEvent(chatui.ChannelStatusView{}, c.Tenant, conversation, statusPayload, time.Now())
	if err != nil {
		return fail(err)
	}
	view.Status = validated.Status
	view.Updated = previous.Status.Revision > 0 && previous.Status.Revision != view.Status.Revision
	if previous.Status.ChangedBy != view.Status.ChangedBy {
		view.ActorName = ""
	}
	view.CanPost = snapshot.CanPost
	view.Transitions = append([]chat.StatusTransition(nil), snapshot.Transitions...)
	view.Loading = false
	view.Unavailable = false
	view.Error = ""
	return view, nil
}

func (c chatstateClient) Change(ctx context.Context, request chat.ChangeChannelStatusRequest, previous chatui.ChannelStatusView) (chatui.ChannelStatusView, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return previous, err
	}
	_, err = c.request(ctx, http.MethodPost, request.ConversationID, payload)
	if err != nil {
		var refusal chatstateRefusal
		if errors.As(err, &refusal) {
			previous.Error = refusal.Code
		}
		return previous, err
	}
	return c.Load(ctx, request.ConversationID, previous)
}

// Watch refreshes the authorized status without reloading the workspace. It
// ends with the room context, including navigation, sign-out and disconnection.
func (c chatstateClient) Watch(ctx context.Context, conversation string, previous chatui.ChannelStatusView, apply func(chatui.ChannelStatusView)) error {
	if apply == nil {
		return chatstateRefusal{Code: "unavailable"}
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		view, err := c.Load(ctx, conversation, previous)
		apply(view)
		if err != nil {
			return err
		}
		previous = view
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
