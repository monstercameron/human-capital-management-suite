package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

var (
	errAgentAnnouncement         = errors.New("agent announcement request failed")
	errAgentAnnouncementDenied   = errors.New("agent announcement access denied")
	errAgentAnnouncementConflict = errors.New("agent announcement changed")
	errAgentAnnouncementInvalid  = errors.New("agent announcement input is invalid")
)

type agentAnnouncementReasonError struct{ Reason string }

func (e agentAnnouncementReasonError) Error() string { return e.Reason }
func (e agentAnnouncementReasonError) Unwrap() error { return errAgentAnnouncement }

func agentAnnouncementRequest(ctx context.Context, client *http.Client, cfg journeyclient.Config, action string, input any) (agentcontrols.AnnouncementReply, error) {
	var reply agentcontrols.AnnouncementReply
	endpoint, err := url.Parse(cfg.TunnelURL)
	if err != nil || endpoint.Host == "" || strings.TrimSpace(cfg.Bearer) == "" {
		return reply, errAgentAnnouncement
	}
	switch endpoint.Scheme {
	case "ws":
		endpoint.Scheme = "http"
	case "wss":
		endpoint.Scheme = "https"
	case "http", "https":
	default:
		return reply, errAgentAnnouncement
	}
	endpoint.Path, endpoint.RawQuery, endpoint.Fragment = agentcontrols.AnnouncementPath, "", ""
	method := http.MethodGet
	var body io.Reader
	if action != "" {
		if action != "preview" && action != "update" && action != "control" && action != "create" {
			return reply, errAgentAnnouncementInvalid
		}
		if action != "create" {
			endpoint.Path += "/" + action
		}
		method = http.MethodPost
		encoded, encodeErr := json.Marshal(input)
		if encodeErr != nil {
			return reply, encodeErr
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return reply, err
	}
	request.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return reply, err
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden:
		return reply, errAgentAnnouncementDenied
	case http.StatusConflict:
		return reply, errAgentAnnouncementConflict
	case http.StatusBadRequest:
		return reply, errAgentAnnouncementInvalid
	default:
		var refusal struct {
			Reason string `json:"reason"`
		}
		if json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(&refusal) == nil && refusal.Reason != "" {
			return reply, agentAnnouncementReasonError{Reason: refusal.Reason}
		}
		return reply, errAgentAnnouncement
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&reply); err != nil {
		return reply, err
	}
	return reply, nil
}

func validateAgentAnnouncementInput(input agentcontrols.AnnouncementDraft) error {
	if agentAnnouncementInvalidField(input) != "" || strings.TrimSpace(input.IdempotencyKey) == "" {
		return errAgentAnnouncementInvalid
	}
	return nil
}

func fmtAnnouncementTime(hour, minute int) string { return fmt.Sprintf("%02d:%02d", hour, minute) }
