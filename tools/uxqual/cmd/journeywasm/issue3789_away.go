package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3789: the browser client of the away setting. The person's own setting
// is read from and kept in /api/chat/away; what colleagues may see of other
// people's absences is read from /api/chat/away/lookup. The server takes the
// person from the session, never from these requests, and decides what a
// colleague may see: a hidden or ended absence is simply not in the answer.
// A save carries the revision the page last read.

const (
	awayPath       = "/api/chat/away"
	awayLookupPath = "/api/chat/away/lookup"
	// awayLookupMax is how many people one lookup names; the server refuses more.
	awayLookupMax = 50
)

type awayReplyJSON struct {
	Revision    uint64     `json:"revision"`
	Active      bool       `json:"active"`
	Until       *time.Time `json:"until"`
	CoverID     string     `json:"cover_id"`
	Hidden      bool       `json:"hidden"`
	NotifyCover bool       `json:"notify_cover"`
}

type awayBodyJSON struct {
	ExpectedRevision uint64     `json:"expected_revision"`
	Until            *time.Time `json:"until,omitempty"`
	CoverID          string     `json:"cover_id,omitempty"`
	Hidden           bool       `json:"hidden"`
	NotifyCover      bool       `json:"notify_cover"`
}

type awayLookupReplyJSON struct {
	People []struct {
		MemberID string    `json:"member_id"`
		Until    time.Time `json:"until"`
		CoverID  string    `json:"cover_id"`
	} `json:"people"`
}

func chatAwayEndpoint(cfg journeyclient.Config, path string) (*url.URL, bool) {
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" {
		return nil, false
	}
	endpoint.Path, endpoint.Fragment, endpoint.RawQuery = path, "", ""
	return endpoint, true
}

// chatAwayView turns the server's answer into what the page draws.
func chatAwayView(reply awayReplyJSON, now time.Time) chatui.AwayView {
	view := chatui.AwayView{Loaded: true, Revision: reply.Revision, Hidden: reply.Hidden}
	if reply.Active && reply.Until != nil && reply.Until.After(now) {
		view.Active, view.Until, view.CoverID, view.NotifyCover = true, reply.Until.UTC(), reply.CoverID, reply.NotifyCover
	}
	return view
}

// chatAwayBody is the save the server is asked for. An ended absence is sent
// as no date, no cover and no notices.
func chatAwayBody(edit chatui.AwayEdit, revision uint64) awayBodyJSON {
	body := awayBodyJSON{ExpectedRevision: revision, Hidden: edit.Hidden}
	if !edit.Until.IsZero() {
		until := edit.Until.UTC()
		body.Until, body.CoverID, body.NotifyCover = &until, edit.CoverID, edit.NotifyCover && edit.CoverID != ""
	}
	return body
}

// chatAwayRead reads the person's own setting. A refusal is an error, not "not away".
func chatAwayRead(ctx context.Context, client *http.Client, cfg journeyclient.Config, now time.Time) (chatui.AwayView, error) {
	endpoint, ok := chatAwayEndpoint(cfg, awayPath)
	if !ok || client == nil {
		return chatui.AwayView{}, chatsaveError("unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return chatui.AwayView{}, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	response, err := client.Do(req)
	if err != nil {
		return chatui.AwayView{}, chatsaveError("unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return chatui.AwayView{}, chatsaveError("unavailable")
	}
	var reply awayReplyJSON
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&reply); err != nil || reply.Revision == 0 {
		return chatui.AwayView{}, chatsaveError("unavailable")
	}
	return chatAwayView(reply, now), nil
}

// chatAwayWrite keeps one setting. The outcome is "saved" (with the setting the
// server now holds), "conflict" (changed elsewhere), "invalid" (the server
// refused the cover or the date) or "failed".
func chatAwayWrite(ctx context.Context, client *http.Client, cfg journeyclient.Config, edit chatui.AwayEdit, revision uint64, now time.Time) (view chatui.AwayView, outcome string) {
	endpoint, ok := chatAwayEndpoint(cfg, awayPath)
	if !ok || client == nil {
		return view, "failed"
	}
	payload, err := json.Marshal(chatAwayBody(edit, revision))
	if err != nil {
		return view, "failed"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return view, "failed"
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return view, "failed"
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	switch response.StatusCode {
	case http.StatusOK:
		var reply awayReplyJSON
		if json.Unmarshal(data, &reply) != nil || reply.Revision == 0 {
			return view, "failed"
		}
		return chatAwayView(reply, now), "saved"
	case http.StatusConflict:
		return view, "conflict"
	case http.StatusBadRequest:
		return view, "invalid"
	}
	return view, "failed"
}

// chatAwayLookup reads what the named colleagues share of their absences, at
// most awayLookupMax people per call. A person who is not in the answer has
// nothing to show.
func chatAwayLookup(ctx context.Context, client *http.Client, cfg journeyclient.Config, ids []string, now time.Time) (map[string]chatui.AwayPersonView, error) {
	endpoint, ok := chatAwayEndpoint(cfg, awayLookupPath)
	if !ok || client == nil {
		return nil, chatsaveError("unavailable")
	}
	out := map[string]chatui.AwayPersonView{}
	for start := 0; start < len(ids); start += awayLookupMax {
		end := min(start+awayLookupMax, len(ids))
		payload, err := json.Marshal(map[string][]string{"members": ids[start:end]})
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
		req.Header.Set("Content-Type", "application/json")
		response, err := client.Do(req)
		if err != nil {
			return nil, chatsaveError("unavailable")
		}
		var reply awayLookupReplyJSON
		err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&reply)
		status := response.StatusCode
		response.Body.Close()
		if status != http.StatusOK || err != nil {
			return nil, chatsaveError("unavailable")
		}
		for _, p := range reply.People {
			if p.MemberID != "" && p.Until.After(now) {
				out[p.MemberID] = chatui.AwayPersonView{Until: p.Until.UTC(), CoverID: p.CoverID}
			}
		}
	}
	return out, nil
}

// chatAwayIDs are the people whose absences the page should look up: the people
// of the conversation in view and the person pane's person, once each, without
// the signed-in person.
func chatAwayIDs(model chatui.Model, extra ...string) []string {
	seen := map[string]bool{model.CurrentUser: true, "": true}
	out := []string{}
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, id := range extra {
		add(id)
	}
	if model.PersonDetails != nil {
		add(model.PersonDetails.ID)
	}
	for _, p := range model.Members {
		if !p.Agent {
			add(p.ID)
		}
	}
	return out
}
