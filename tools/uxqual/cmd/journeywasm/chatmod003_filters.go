package main

import (
	"context"
	"errors"
	"net/url"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// filterBaseURL is the origin the filter service is asked at: the tunnel's
// address with its scheme made plain http(s) and no path.
func filterBaseURL(tunnel string) string {
	endpoint, err := url.Parse(tunnel)
	if err != nil {
		endpoint = &url.URL{}
	}
	switch endpoint.Scheme {
	case "ws":
		endpoint.Scheme = "http"
	case "wss":
		endpoint.Scheme = "https"
	}
	endpoint.Path, endpoint.RawQuery, endpoint.Fragment = "", "", ""
	return endpoint.String()
}

// filterErrorCode is the service's own code for a refusal, or "network" for
// anything that never got an answer. It is the only thing about an error that
// reaches chatui.ModErrorKey, so no internal text ("context canceled", a status
// line, an identifier) is ever shown.
func filterErrorCode(err error) string {
	var typed *FilterAPIError
	if errors.As(err, &typed) && typed.Code != "" {
		return typed.Code
	}
	return "network"
}

// filterSnapshot is one reading of what the panel shows.
type filterSnapshot struct {
	Definitions []chatfilter.Definition
	Enablements []chatfilter.Enablement
}

// Snapshot reads the definitions that apply at channel and the enablement rows
// of the workspace and that channel. Either failing fails the reading, and the
// caller keeps what it already had.
func (c FilterAPIClient) Snapshot(ctx context.Context, channel string) (filterSnapshot, error) {
	definitions, err := c.ListForChannel(ctx, channel)
	if err != nil {
		return filterSnapshot{}, err
	}
	rows, err := c.EnablementsForChannel(ctx, channel)
	if err != nil {
		return filterSnapshot{}, err
	}
	return filterSnapshot{Definitions: definitions, Enablements: rows}, nil
}

// Apply writes one switch of the panel: the row at the level the panel chose.
func (c FilterAPIClient) Apply(ctx context.Context, request chatui.ModSwitch) error {
	return c.EnableAction(ctx, request.RuleID, request.Channel, request.On, false, request.Action)
}

// SaveFilter creates the definition's version and switches it on, as a filter
// of one channel at that channel and any other at the workspace. created is true
// once the version exists, even when switching it on then failed.
func (c FilterAPIClient) SaveFilter(ctx context.Context, d chatfilter.Definition, dryRun bool) (created bool, err error) {
	if err = c.CreateVersion(ctx, d); err != nil {
		return false, err
	}
	channel := ""
	if len(d.Channels) == 1 {
		channel = d.Channels[0]
	}
	return true, c.Enable(ctx, d.ID, channel, true, dryRun)
}
