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
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3998: the browser side of "Ask for input". The calls are plain JSON
// over /api/chat/journeys/{id}/input-request. Nothing here decides who may ask
// or who may read a question: the server does. A question is sent only after
// the reviewer has seen the exact message, the proposal revision and who can
// read it (a preview call that sends nothing) and presses Send.

type inputAudienceJSON struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Size int    `json:"size"`
}

type inputItemJSON struct {
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	AskedAt          string `json:"asked_at"`
	Revision         string `json:"revision"`
	State            string `json:"state"`
	AudienceKind     string `json:"audience_kind"`
	AudienceSize     int    `json:"audience_size"`
	SharedCaseName   bool   `json:"shared_case_name"`
	ConversationID   string `json:"conversation_id"`
	ConversationName string `json:"conversation_name"`
	Href             string `json:"href"`
}

type inputViewJSON struct {
	CanAsk     bool            `json:"can_ask"`
	CaseOpen   bool            `json:"case_open"`
	Status     string          `json:"status"`
	Revision   string          `json:"revision"`
	CaseName   string          `json:"case_name"`
	Items      []inputItemJSON `json:"items"`
	Incomplete bool            `json:"incomplete"`
}

type inputAskJSON struct {
	Revision        string `json:"revision"`
	Question        string `json:"question"`
	ConversationID  string `json:"conversation_id"`
	Label           string `json:"label"`
	RevisionLabel   string `json:"revision_label"`
	IncludeCaseName bool   `json:"include_case_name"`
	Preview         bool   `json:"preview"`
	IdempotencyKey  string `json:"idempotency_key,omitempty"`
}

type inputAskedJSON struct {
	Preview  bool              `json:"preview"`
	Message  string            `json:"message"`
	Audience inputAudienceJSON `json:"audience"`
	Revision string            `json:"revision"`
	Item     *inputItemJSON    `json:"item"`
}

// journeyInputCall reads (ask == nil) or posts (ask != nil: a preview or a
// send) one journey's input requests. A refusal is reduced to the server's
// short code and its body is never shown.
func journeyInputCall(ctx context.Context, client *http.Client, cfg journeyclient.Config, journeyID string, ask *inputAskJSON) (inputViewJSON, inputAskedJSON, error) {
	var view inputViewJSON
	var asked inputAskedJSON
	if strings.TrimSpace(journeyID) == "" || strings.Contains(journeyID, "/") {
		return view, asked, chatworkError("invalid_argument")
	}
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" || client == nil {
		return view, asked, chatworkError("unavailable")
	}
	endpoint.Path, endpoint.RawPath, endpoint.RawQuery, endpoint.Fragment = "/api/chat/journeys/"+journeyID+"/input-request", "/api/chat/journeys/"+url.PathEscape(journeyID)+"/input-request", "", ""
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
		return inputViewJSON{}, inputAskedJSON{}, chatworkError(refusal.Code)
	}
	limited := io.LimitReader(response.Body, 1<<20)
	if ask != nil {
		if err := json.NewDecoder(limited).Decode(&asked); err != nil {
			return view, inputAskedJSON{}, chatworkError("unavailable")
		}
		return view, asked, nil
	}
	if err := json.NewDecoder(limited).Decode(&view); err != nil {
		return inputViewJSON{}, asked, chatworkError("unavailable")
	}
	return view, asked, nil
}

// inputErrorKey is the copy key for a refusal code.
func inputErrorKey(code string) string {
	switch code {
	case "invalid_argument":
		return "err_invalid"
	case "revision_changed":
		return "err_stale"
	case "case_closed":
		return "err_closed"
	case "no_conversation":
		return "err_no_conversation"
	case "name_in_public":
		return "err_name_public"
	case "permission_denied", "not_found":
		return "err_denied"
	}
	return "err_generic"
}

// inputFinal is true for a refusal that no retry of the same question can fix.
func inputFinal(code string) bool {
	switch code {
	case "revision_changed", "case_closed", "permission_denied", "not_found":
		return true
	}
	return false
}

// inputAttemptKey names one attempt at sending a question, kept until it is
// sent so a double press or a retry is one message.
func inputAttemptKey() string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return ""
	}
	return "input-" + hex.EncodeToString(raw[:])
}

func inputWordsFor(locale string) func(string) string {
	return func(key string) string { return productui.JourneyInputText(locale, key) }
}

// inputAudienceSentence says who can read the question, from the audience the
// server computed. The size is added only when the server knows it.
func inputAudienceSentence(a inputAudienceJSON, words func(string) string) string {
	var sentence string
	switch a.Kind {
	case "direct":
		sentence = words("audience_direct")
	case "group":
		sentence = words("audience_group")
	case "private_channel":
		sentence = clarifyFill(words("audience_private"), map[string]string{"name": a.Name})
	default:
		sentence = clarifyFill(words("audience_public"), map[string]string{"name": a.Name})
	}
	if a.Size > 0 {
		sentence += " " + clarifyFill(words("audience_size"), map[string]string{"size": strconv.Itoa(a.Size)})
	}
	return sentence + " " + words("audience_case")
}

// inputRow is one question as the page lists it.
type inputRow struct {
	Title, Revision, State string
	Href, Aria, LinkLabel  string
}

// inputRows turns the server's items into rows using the page's own words.
// Only a same-origin chat address is ever linked; an item whose conversation
// can no longer be opened carries the not-current sentence and no link.
func inputRows(items []inputItemJSON, words func(string) string) []inputRow {
	rows := make([]inputRow, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.ConversationName)
		row := inputRow{Revision: clarifyFill(words("item_revision"), map[string]string{"revision": clarifyShortRevision(item.Revision)})}
		switch {
		case name != "":
			row.Title = clarifyFill(words("item_in"), map[string]string{"name": name})
		case item.AudienceKind == "direct":
			row.Title = words("item_dm")
		default:
			row.Title = words("item_unknown")
		}
		switch item.State {
		case "outdated":
			row.State = words("state_outdated")
		case "closed":
			row.State = words("state_closed")
		case "unavailable":
			row.State = words("state_unavailable")
		default:
			row.State = words("state_current")
		}
		if item.State != "unavailable" && strings.HasPrefix(item.Href, "/workspace/app/chat") && !strings.HasPrefix(item.Href, "//") {
			row.Href, row.LinkLabel, row.Aria = item.Href, words("open"), words("open")
			if name != "" {
				row.Aria = clarifyFill(words("open_aria"), map[string]string{"name": name})
			}
		}
		rows = append(rows, row)
	}
	return rows
}
