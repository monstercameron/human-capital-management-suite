//go:build js && wasm

package chatui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
)

var errTranslationAdminInput = errors.New("chatui: translation administration input is invalid")

// translationAdminCall asks the translation administration API with the same
// admitted session credential as Chat. It answers the status so the panel can
// tell a refusal from a failure, and returns a cancel for the effect.
func translationAdminCall(action, room string, body any, done func(TranslationAdminData, int, error)) func() {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		data, status, err := translationAdminRequest(ctx, action, room, body)
		if ctx.Err() == nil {
			done(data, status, err)
		}
	}()
	return cancel
}

// translationAdminRequest asks once and, when the server refuses the page's
// credential (a tab that outlived its session or the server's restart), gets a
// fresh one and asks once more (CHATBUG-087).
func translationAdminRequest(ctx context.Context, action, room string, body any) (TranslationAdminData, int, error) {
	data, status, err := translationAdminAttempt(ctx, action, room, body)
	// A 403 on a read is the person's role, not the credential; on a write it may
	// be the browser token the server minted for an older page.
	if status != http.StatusUnauthorized && !(status == http.StatusForbidden && body != nil) {
		return data, status, err
	}
	if _, refreshErr := chatSessionRefresh(ctx); refreshErr != nil {
		return data, status, err
	}
	return translationAdminAttempt(ctx, action, room, body)
}

func translationAdminAttempt(ctx context.Context, action, room string, body any) (TranslationAdminData, int, error) {
	var data TranslationAdminData
	origin, err := url.Parse(js.Global().Get("location").Get("origin").String())
	if err != nil || origin.Host == "" || (origin.Scheme != "http" && origin.Scheme != "https") || origin.User != nil {
		return data, 0, errTranslationAdminInput
	}
	bearer := ""
	island := js.Global().Get("document").Call("getElementById", "journey-config")
	if island.Truthy() {
		var session struct {
			Bearer string `json:"bearer"`
		}
		if json.Unmarshal([]byte(island.Get("textContent").String()), &session) == nil {
			bearer = session.Bearer
		}
	}
	if bearer == "" {
		return data, 0, errTranslationAdminInput
	}
	origin.Path = "/api/chat/translation/v1/" + action
	origin.RawQuery, origin.Fragment = "", ""
	method := http.MethodGet
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return data, 0, err
		}
		method, payload = http.MethodPost, bytes.NewReader(encoded)
	}
	if room != "" && method == http.MethodGet {
		origin.RawQuery = url.Values{"conversation": {room}}.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, method, origin.String(), payload)
	if err != nil {
		return data, 0, err
	}
	request.Header.Set("Authorization", "Bearer "+bearer)
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return data, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return data, response.StatusCode, errors.New("chatui: translation administration answered " + strconv.Itoa(response.StatusCode))
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return data, response.StatusCode, err
	}
	if err = json.Unmarshal(raw, &data); err != nil {
		return data, response.StatusCode, err
	}
	return data, response.StatusCode, nil
}

func translationAdminForm(event ui.Event) js.Value { return event.JSValue().Get("currentTarget") }

// translationAdminReadWorkspace reads the workspace form. The formality the
// server holds is carried back unchanged: the form does not edit it.
func translationAdminReadWorkspace(event ui.Event, previous TranslationAdminData) (any, error) {
	form := translationAdminForm(event)
	if !form.Truthy() {
		return nil, errTranslationAdminInput
	}
	workspace := chatlang.Workspace{Formality: previous.Workspace.Formality}
	workspace.Enabled = form.Call("querySelector", "[name=enabled]").Get("checked").Bool()
	workspace.ExternalAllowed = form.Call("querySelector", "[name=external]").Get("checked").Bool()
	nodes := form.Call("querySelectorAll", "[name=language]:checked")
	for i := 0; i < nodes.Length(); i++ {
		workspace.Languages = append(workspace.Languages, nodes.Index(i).Get("value").String())
	}
	limit, err := strconv.ParseFloat(strings.TrimSpace(form.Call("querySelector", "[name=limit]").Get("value").String()), 64)
	if err != nil || limit < 0 || limit > 100000 {
		return nil, errTranslationAdminInput
	}
	workspace.BudgetMicros = int64(limit*1_000_000 + 0.5)
	return map[string]any{"workspace": workspace}, nil
}

func translationAdminReadChannel(event ui.Event, room string) (any, error) {
	form := translationAdminForm(event)
	if !form.Truthy() || room == "" {
		return nil, errTranslationAdminInput
	}
	external := chatlang.ExternalInherit
	if form.Call("querySelector", "[name=barred]").Get("checked").Bool() {
		external = chatlang.ExternalBarred
	}
	return map[string]any{"conversation": room, "translation": form.Call("querySelector", "[name=translation]").Get("value").String(), "external": external}, nil
}

func translationAdminReadTerm(event ui.Event) (any, error) {
	form := translationAdminForm(event)
	if !form.Truthy() {
		return nil, errTranslationAdminInput
	}
	term := chatlang.Term{Source: strings.TrimSpace(form.Call("querySelector", "[name=term]").Get("value").String())}
	if form.Call("querySelector", "[name=mode]").Get("value").String() == "translate" {
		term.Language = form.Call("querySelector", "[name=language]").Get("value").String()
		term.Target = strings.TrimSpace(form.Call("querySelector", "[name=target]").Get("value").String())
	}
	if !chatlang.ValidTerm(term) {
		return nil, errTranslationAdminInput
	}
	return map[string]any{"term": term}, nil
}

func translationAdminReadTermID(event ui.Event) string {
	target := event.JSValue().Get("currentTarget")
	if !target.Truthy() {
		return ""
	}
	return target.Get("dataset").Get("term").String()
}

// translationAdminChangedField is the name of the control a change event came
// from, so the page can say "Saved" beside that setting.
func translationAdminChangedField(event ui.Event) string {
	target := event.JSValue().Get("target")
	if !target.Truthy() || target.Get("name").Type() != js.TypeString {
		return ""
	}
	return target.Get("name").String()
}
