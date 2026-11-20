package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3811: the browser side of the new-hire welcome card. One plain JSON
// read: what the onboarding owner says of this reader's own checklist for each
// card on screen. Nothing here decides who may see what or whether anything is
// done: the server does, and the client shows its word. A card the server does
// not describe comes back loaded with nothing offered.

type onboardingItemJSON struct {
	Kind        string `json:"kind"`
	Owner       string `json:"owner"`
	Status      string `json:"status"`
	ConfirmedOn string `json:"confirmed_on"`
	Required    bool   `json:"required"`
}

type onboardingViewJSON struct {
	PostID    string               `json:"post_id"`
	State     string               `json:"state"`
	StartDate string               `json:"start_date"`
	Items     []onboardingItemJSON `json:"items"`
}

func onboardingView(raw onboardingViewJSON) chatui.OnboardingCardView {
	view := chatui.OnboardingCardView{Loaded: true, State: raw.State, StartDate: raw.StartDate}
	for _, item := range raw.Items {
		view.Items = append(view.Items, chatui.OnboardingItemView{Kind: item.Kind, Owner: item.Owner, Status: item.Status, ConfirmedOn: item.ConfirmedOn, Required: item.Required})
	}
	return view
}

type onboardingError string

func (e onboardingError) Error() string { return string(e) }

const errOnboardingUnavailable = onboardingError("unavailable")

// onboardingRead asks about the given cards. A failed read is an error, not an
// empty answer: the caller shows the card as unreadable.
func onboardingRead(ctx context.Context, client *http.Client, cfg journeyclient.Config, conversation string, posts []string) (map[string]chatui.OnboardingCardView, error) {
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" || client == nil {
		return nil, errOnboardingUnavailable
	}
	endpoint.Path = "/api/chat/conversations/" + conversation + "/onboarding"
	endpoint.RawPath = "/api/chat/conversations/" + url.PathEscape(conversation) + "/onboarding"
	endpoint.RawQuery, endpoint.Fragment = url.Values{"post": posts}.Encode(), ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	response, err := client.Do(req)
	if err != nil {
		return nil, errOnboardingUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errOnboardingUnavailable
	}
	var reply struct {
		Views map[string]onboardingViewJSON `json:"views"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&reply); err != nil {
		return nil, errOnboardingUnavailable
	}
	out := map[string]chatui.OnboardingCardView{}
	for _, id := range posts {
		out[id] = chatui.OnboardingCardView{Loaded: true}
	}
	for id, view := range reply.Views {
		if _, asked := out[id]; asked {
			out[id] = onboardingView(view)
		}
	}
	return out, nil
}
