package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3995: the browser side of "Ask for clarification". The calls are plain
// JSON over /api/chat/journeys/{id}/clarification. Nothing here decides who may
// ask or who may read a question: the server does, and it answers only with
// what this person may see. A question is sent only after the approver reviews
// the case, the revision and the audience in the dialog and presses Send.

type clarifyAudienceJSON struct {
	Kind          string `json:"kind"`
	RecipientName string `json:"recipient_name"`
	Private       bool   `json:"private"`
}

type clarifyItemJSON struct {
	ID             string `json:"id"`
	Role           string `json:"role"`
	PeerName       string `json:"peer_name"`
	AskedAt        string `json:"asked_at"`
	Revision       string `json:"revision"`
	State          string `json:"state"`
	ConversationID string `json:"conversation_id"`
	Href           string `json:"href"`
	Unavailable    bool   `json:"unavailable"`
}

type clarifyViewJSON struct {
	CanAsk     bool                 `json:"can_ask"`
	AskBlocked string               `json:"ask_blocked"`
	CaseOpen   bool                 `json:"case_open"`
	Status     string               `json:"status"`
	Revision   string               `json:"revision"`
	WorkerName string               `json:"worker_name"`
	Audience   *clarifyAudienceJSON `json:"audience"`
	Items      []clarifyItemJSON    `json:"items"`
	Incomplete bool                 `json:"incomplete"`
}

type clarifyAskJSON struct {
	Revision       string `json:"revision"`
	Question       string `json:"question"`
	IdempotencyKey string `json:"idempotency_key"`
}

type clarifyAskedJSON struct {
	Item clarifyItemJSON `json:"item"`
}

// journeyClarifyCall reads (ask == nil) or sends (ask != nil) one journey's
// clarification. A refusal is reduced to the server's short code and its body
// is never shown.
func journeyClarifyCall(ctx context.Context, client *http.Client, cfg journeyclient.Config, journeyID string, ask *clarifyAskJSON) (clarifyViewJSON, clarifyAskedJSON, error) {
	var view clarifyViewJSON
	var asked clarifyAskedJSON
	if strings.TrimSpace(journeyID) == "" || strings.Contains(journeyID, "/") {
		return view, asked, chatworkError("invalid_argument")
	}
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" || client == nil {
		return view, asked, chatworkError("unavailable")
	}
	endpoint.Path, endpoint.RawPath, endpoint.RawQuery, endpoint.Fragment = "/api/chat/journeys/"+journeyID+"/clarification", "/api/chat/journeys/"+url.PathEscape(journeyID)+"/clarification", "", ""
	method := http.MethodGet
	var body io.Reader
	if ask != nil {
		encoded, err := json.Marshal(ask)
		if err != nil {
			return view, asked, err
		}
		method, body = http.MethodPost, bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return view, asked, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(req)
	if err != nil {
		return view, asked, chatworkError("unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && !(ask != nil && response.StatusCode == http.StatusCreated) {
		var refusal struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&refusal)
		if refusal.Code == "" {
			refusal.Code = "unavailable"
		}
		return clarifyViewJSON{}, clarifyAskedJSON{}, chatworkError(refusal.Code)
	}
	limited := io.LimitReader(response.Body, 1<<20)
	if ask != nil {
		if err := json.NewDecoder(limited).Decode(&asked); err != nil {
			return view, clarifyAskedJSON{}, chatworkError("unavailable")
		}
		return view, asked, nil
	}
	if err := json.NewDecoder(limited).Decode(&view); err != nil {
		return clarifyViewJSON{}, asked, chatworkError("unavailable")
	}
	return view, asked, nil
}

// clarifyErrorKey is the copy key for a refusal code.
func clarifyErrorKey(code string) string {
	switch code {
	case "invalid_argument":
		return "err_invalid"
	case "revision_changed":
		return "err_stale"
	case "case_closed":
		return "err_closed"
	case "no_recipient":
		return "err_no_recipient"
	case "permission_denied", "not_found":
		return "err_denied"
	}
	return "err_generic"
}

// clarifyAttemptKey names one attempt at sending a question. The dialog keeps
// it until the question is sent, so a double press or a retry after a failure
// is the same attempt and the server sends one message.
func clarifyAttemptKey() string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return ""
	}
	return "clarify-" + hex.EncodeToString(raw[:])
}

func clarifyFill(template string, vars map[string]string) string {
	for name, value := range vars {
		template = strings.ReplaceAll(template, "{"+name+"}", value)
	}
	return template
}

// clarifyRow is one question as the page lists it.
type clarifyRow struct {
	Title, State, Unavailable string
	Href, Aria, LinkLabel     string
}

// clarifyRows turns the server's items into rows using the page's own words.
// Only a same-origin chat address is ever linked, and an item the person can
// no longer open carries the unavailable sentence and no link.
func clarifyRows(items []clarifyItemJSON, words func(key string) string) []clarifyRow {
	rows := make([]clarifyRow, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.PeerName)
		titleKey := "asked_you"
		if item.Role == "asker" {
			titleKey = "asked_by_you"
		}
		if name == "" {
			titleKey += "_unknown"
		}
		row := clarifyRow{Title: clarifyFill(words(titleKey), map[string]string{"name": name})}
		switch item.State {
		case "outdated":
			row.State = words("state_outdated")
		case "closed":
			row.State = words("state_closed")
		default:
			row.State = words("state_current")
		}
		switch {
		case item.Unavailable:
			row.Unavailable = words("unavailable")
		case strings.HasPrefix(item.Href, "/workspace/app/chat") && !strings.HasPrefix(item.Href, "//"):
			row.Href, row.LinkLabel = item.Href, words("open")
			row.Aria = words("open")
			if name != "" {
				row.Aria = clarifyFill(words("open_aria"), map[string]string{"name": name})
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// clarifyWordsFor reads the copy the server-rendered slot carries.
func clarifyWordsFor(locale string) func(string) string {
	return func(key string) string { return productui.JourneyClarifyText(locale, key) }
}

// clarifyShortRevision is the revision as the dialog prints it: long enough to
// tell two apart, short enough to read. The full value is the element's title.
func clarifyShortRevision(revision string) string {
	revision = strings.TrimSpace(revision)
	if utf8.RuneCountInString(revision) > 12 {
		return string([]rune(revision)[:12]) + "…"
	}
	return revision
}

// clarifyFinal is true for a refusal that no retry of the same question can
// fix, so the dialog stops offering Send and keeps the text to copy.
func clarifyFinal(code string) bool {
	switch code {
	case "revision_changed", "case_closed", "no_recipient", "permission_denied", "not_found":
		return true
	}
	return false
}
