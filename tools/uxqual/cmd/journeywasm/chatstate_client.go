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
	"sync"
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
	if json.Unmarshal(payload, &snapshot) != nil {
		return fail(errChatstateEvent)
	}
	return c.view(conversation, snapshot, previous)
}

// view turns one authorized snapshot into the view the page draws, on top of
// the view it had. A snapshot that is not this tenant's, or this
// conversation's, is refused.
func (c chatstateClient) view(conversation string, snapshot chat.ChannelStatusSnapshot, previous chatui.ChannelStatusView) (chatui.ChannelStatusView, error) {
	view := previous
	fail := func(err error) (chatui.ChannelStatusView, error) {
		view.Unavailable = true
		view.Loading = false
		view.CanPost = false
		view.Transitions = nil
		return view, err
	}
	if snapshot.Status.TenantID != c.Tenant || snapshot.Status.ConversationID != conversation || snapshot.Status.Revision == 0 {
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

// chatstateFallbackInterval is how often an open conversation's status is read
// again when nothing else has said it changed (CHATBUG-075). The status is read
// when the conversation opens and after the person's own change; this is the
// fallback for a change somebody else made. It was five seconds, which is a
// request every five seconds from every open tab for a value that changes a
// few times in a channel's life.
const chatstateFallbackInterval = time.Minute

// chatstateSignal carries "the event stream says this conversation changed"
// from the stream reader to the status watch of the open conversation
// (CHATBUG-075). The stream delivers a status change as a conversation update;
// what the reader may do afterwards (post, change the status back) is still
// decided by the server, so the watch reads the status again rather than
// trusting the event's own value.
type chatstateSignal struct {
	mu   sync.Mutex
	room string
	wake chan struct{}
}

// chatstateChanged is the signal of the one conversation the page has open.
var chatstateChanged chatstateSignal

// Listen returns the channel that receives one value per burst of changes to
// the conversation. Only one conversation is open at a time: listening to
// another one ends the delivery to the earlier listener.
func (s *chatstateSignal) Listen(conversation string) <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.room != conversation || s.wake == nil {
		s.room, s.wake = conversation, make(chan struct{}, 1)
	}
	return s.wake
}

// Changed wakes the listener of the conversation, if there is one. Several
// changes before the listener reads are one wake: the read that follows
// returns the latest status either way.
func (s *chatstateSignal) Changed(conversation string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.wake == nil || s.room != conversation {
		return
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Watch reads the authorized status when it starts, again whenever the event
// stream reports a change to the conversation, and once per fallback interval
// for the case where the stream is down, without reloading the workspace. It
// ends with the room context, including navigation, sign-out and disconnection.
func (c chatstateClient) Watch(ctx context.Context, conversation string, previous chatui.ChannelStatusView, apply func(chatui.ChannelStatusView)) error {
	ticker := time.NewTicker(chatstateFallbackInterval)
	defer ticker.Stop()
	return c.watch(ctx, conversation, previous, apply, ticker.C, chatstateChanged.Listen(conversation))
}

// watch is Watch with the clock and the change signal handed in: one read at
// once, then one per tick and one per signalled change.
func (c chatstateClient) watch(ctx context.Context, conversation string, previous chatui.ChannelStatusView, apply func(chatui.ChannelStatusView), tick <-chan time.Time, changed <-chan struct{}) error {
	if apply == nil {
		return chatstateRefusal{Code: "unavailable"}
	}
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
		case <-tick:
		case <-changed:
		}
	}
}
