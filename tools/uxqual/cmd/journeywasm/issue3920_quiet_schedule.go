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

// Issue 3920: the browser client of the weekly quiet-hours schedule. The
// schedule and the one-off pause are read from and kept in
// /api/chat/quiet-schedule; the server takes the person from the session, never
// from this request, and decides what is quiet. A save carries the revision the
// page last read, so a schedule changed in another window is never overwritten.

const quietSchedulePath = "/api/chat/quiet-schedule"

type quietScheduleDayJSON struct {
	Mode  string `json:"mode"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

type quietSchedulePauseJSON struct {
	Kind  string     `json:"kind"`
	Until *time.Time `json:"until,omitempty"`
}

type quietScheduleReplyJSON struct {
	Revision   uint64                 `json:"revision"`
	WeekActive bool                   `json:"week_active"`
	Days       []quietScheduleDayJSON `json:"days"`
	Pause      quietSchedulePauseJSON `json:"pause"`
	QuietNow   bool                   `json:"quiet_now"`
	Next       *struct {
		At    time.Time `json:"at"`
		Quiet bool      `json:"quiet"`
	} `json:"next"`
}

type quietScheduleBodyJSON struct {
	ExpectedRevision uint64                 `json:"expected_revision"`
	WeekActive       bool                   `json:"week_active"`
	Days             []quietScheduleDayJSON `json:"days"`
	Pause            quietSchedulePauseJSON `json:"pause"`
}

type quietScheduleRefusalJSON struct {
	Code   string `json:"code"`
	Day    *int   `json:"day"`
	Reason string `json:"reason"`
}

func chatQuietScheduleEndpoint(cfg journeyclient.Config) (*url.URL, bool) {
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" {
		return nil, false
	}
	endpoint.Path, endpoint.Fragment, endpoint.RawQuery = quietSchedulePath, "", ""
	return endpoint, true
}

// chatQuietScheduleView turns the server's answer into what the page draws.
func chatQuietScheduleView(reply quietScheduleReplyJSON) chatui.QuietScheduleView {
	view := chatui.QuietScheduleView{Loaded: true, Revision: reply.Revision, WeekActive: reply.WeekActive, QuietNow: reply.QuietNow, PauseKind: reply.Pause.Kind}
	for i := 0; i < 7 && i < len(reply.Days); i++ {
		view.Days[i] = chatui.QuietDayView{Mode: reply.Days[i].Mode, StartMinute: reply.Days[i].Start, EndMinute: reply.Days[i].End}
	}
	if reply.Pause.Kind != "" && reply.Pause.Until != nil {
		view.PauseUntil = reply.Pause.Until.UTC()
	} else {
		view.PauseKind = ""
	}
	if reply.Next != nil {
		view.HasNext, view.NextAt, view.NextQuiet = true, reply.Next.At, reply.Next.Quiet
	}
	return view
}

// chatQuietScheduleBody is the save the server is asked for. A pause the person
// just chose runs from now for the minutes they chose; one already running keeps
// its own end; an ended or cleared one is sent as none.
func chatQuietScheduleBody(edit chatui.QuietScheduleEdit, revision uint64, now time.Time) quietScheduleBodyJSON {
	body := quietScheduleBodyJSON{ExpectedRevision: revision, WeekActive: edit.WeekActive, Days: make([]quietScheduleDayJSON, 7)}
	for i, d := range edit.Days {
		mode := d.Mode
		if mode == "" {
			mode = chatui.QuietDayNone
		}
		body.Days[i] = quietScheduleDayJSON{Mode: mode, Start: d.StartMinute, End: d.EndMinute}
	}
	switch {
	case edit.PauseMinutes > 0 && edit.PauseKind != "":
		until := now.UTC().Add(time.Duration(edit.PauseMinutes) * time.Minute)
		body.Pause = quietSchedulePauseJSON{Kind: edit.PauseKind, Until: &until}
	case edit.PauseKind != "" && edit.PauseUntil.After(now):
		until := edit.PauseUntil.UTC()
		body.Pause = quietSchedulePauseJSON{Kind: edit.PauseKind, Until: &until}
	}
	return body
}

// chatQuietScheduleRead reads the person's own schedule. A refusal is an error,
// not an empty schedule.
func chatQuietScheduleRead(ctx context.Context, client *http.Client, cfg journeyclient.Config) (chatui.QuietScheduleView, error) {
	endpoint, ok := chatQuietScheduleEndpoint(cfg)
	if !ok || client == nil {
		return chatui.QuietScheduleView{}, chatsaveError("unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return chatui.QuietScheduleView{}, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	response, err := client.Do(req)
	if err != nil {
		return chatui.QuietScheduleView{}, chatsaveError("unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return chatui.QuietScheduleView{}, chatsaveError("unavailable")
	}
	var reply quietScheduleReplyJSON
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&reply); err != nil || len(reply.Days) != 7 {
		return chatui.QuietScheduleView{}, chatsaveError("unavailable")
	}
	return chatQuietScheduleView(reply), nil
}

// chatQuietScheduleWrite keeps one schedule. The outcome is "saved" (with the
// schedule the server now holds), "conflict" (changed elsewhere), "overlap" or
// "empty_interval" (with the weekday it is about), or "failed".
func chatQuietScheduleWrite(ctx context.Context, client *http.Client, cfg journeyclient.Config, edit chatui.QuietScheduleEdit, revision uint64, now time.Time) (view chatui.QuietScheduleView, outcome string, day int) {
	endpoint, ok := chatQuietScheduleEndpoint(cfg)
	if !ok || client == nil {
		return view, "failed", 0
	}
	payload, err := json.Marshal(chatQuietScheduleBody(edit, revision, now))
	if err != nil {
		return view, "failed", 0
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return view, "failed", 0
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return view, "failed", 0
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	switch response.StatusCode {
	case http.StatusOK:
		var reply quietScheduleReplyJSON
		if json.Unmarshal(data, &reply) != nil || len(reply.Days) != 7 {
			return view, "failed", 0
		}
		return chatQuietScheduleView(reply), "saved", 0
	case http.StatusConflict:
		return view, "conflict", 0
	case http.StatusBadRequest:
		var refusal quietScheduleRefusalJSON
		if json.Unmarshal(data, &refusal) == nil && refusal.Day != nil && (refusal.Reason == "overlap" || refusal.Reason == "empty_interval") {
			return view, refusal.Reason, *refusal.Day
		}
	}
	return view, "failed", 0
}

// chatQuietNow is whether optional sounds are held now. The weekly schedule and
// a pause or override, when the person has them, decide from what the server
// last said (flipped once its next pause or resume has passed); otherwise the
// daily interval decides, as it always did.
func chatQuietNow(model chatui.Model, now time.Time) bool {
	v := model.QuietSchedule
	pauseLive := v.PauseKind != "" && v.PauseUntil.After(now)
	if v.Loaded && (v.WeekActive || pauseLive) {
		quiet := v.QuietNow
		if v.HasNext && !now.Before(v.NextAt) {
			quiet = v.NextQuiet
		}
		return quiet
	}
	return chatQuietHoursActive(model.Preferences, now)
}
