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
)

var errChatAuditInput = errors.New("chatui: the audit log needs a signed-in page")

// chatAuditCall asks the audit log and answers the status so the page can tell a
// refusal from a failure. It returns a cancel for the effect.
func chatAuditCall(action string, body any, done func(ChatAuditData, int, error)) func() {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		var data ChatAuditData
		raw, status, err := chatAuditRequest(ctx, action, body)
		if err == nil {
			err = json.Unmarshal(raw, &data)
		}
		if ctx.Err() == nil {
			done(data, status, err)
		}
	}()
	return cancel
}

// chatAuditRequest asks once and, when the server refuses the page's credential
// (a tab that outlived its session), gets a fresh one and asks once more.
func chatAuditRequest(ctx context.Context, action string, body any) ([]byte, int, error) {
	raw, status, err := chatAuditAttempt(ctx, action, body)
	if status != http.StatusUnauthorized && !(status == http.StatusForbidden && action == "export") {
		return raw, status, err
	}
	if _, refreshErr := chatSessionRefresh(ctx); refreshErr != nil {
		return raw, status, err
	}
	return chatAuditAttempt(ctx, action, body)
}

func chatAuditAttempt(ctx context.Context, action string, body any) ([]byte, int, error) {
	origin, err := url.Parse(js.Global().Get("location").Get("origin").String())
	if err != nil || origin.Host == "" || (origin.Scheme != "http" && origin.Scheme != "https") || origin.User != nil {
		return nil, 0, errChatAuditInput
	}
	bearer := ""
	if island := js.Global().Get("document").Call("getElementById", "journey-config"); island.Truthy() {
		var session struct {
			Bearer string `json:"bearer"`
		}
		if json.Unmarshal([]byte(island.Get("textContent").String()), &session) == nil {
			bearer = session.Bearer
		}
	}
	if bearer == "" || (action != "list" && action != "export") {
		return nil, 0, errChatAuditInput
	}
	origin.Path, origin.RawQuery, origin.Fragment = "/api/chat/audit/v1/"+action, "", ""
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, origin.String(), bytes.NewReader(encoded))
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Authorization", "Bearer "+bearer)
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, response.StatusCode, errors.New("chatui: audit log answered " + strconv.Itoa(response.StatusCode))
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	return raw, response.StatusCode, err
}

// chatAuditExport asks for the CSV and hands it to the browser as a download.
// Nothing is downloaded unless the server answered, and the server answers only
// after it has recorded the export.
func chatAuditExport(filter ChatAuditFilter, done func(int, error)) {
	go func() {
		raw, status, err := chatAuditRequest(context.Background(), "export", chatAuditBody(filter, ""))
		if err == nil {
			array := js.Global().Get("Uint8Array").New(len(raw))
			js.CopyBytesToJS(array, raw)
			blob := js.Global().Get("Blob").New([]any{array}, map[string]any{"type": "text/csv;charset=utf-8"})
			link := js.Global().Get("document").Call("createElement", "a")
			address := js.Global().Get("URL").Call("createObjectURL", blob)
			link.Set("href", address)
			link.Set("download", "chat-audit-log.csv")
			link.Set("rel", "noopener")
			js.Global().Get("document").Get("body").Call("appendChild", link)
			link.Call("click")
			link.Call("remove")
			js.Global().Get("URL").Call("revokeObjectURL", address)
		}
		done(status, err)
	}()
}

// chatAuditReadFilter reads the filter form.
func chatAuditReadFilter(event ui.Event) ChatAuditFilter {
	form := event.JSValue().Get("currentTarget")
	if !form.Truthy() {
		return ChatAuditFilter{}
	}
	value := func(name string) string {
		field := form.Call("querySelector", "[name="+name+"]")
		if !field.Truthy() {
			return ""
		}
		return strings.TrimSpace(field.Get("value").String())
	}
	return ChatAuditFilter{Actor: value("actor"), Action: value("action"), Conversation: value("conversation"), FromDate: value("fromdate"), ToDate: value("todate")}
}

// chatAuditResetForm empties the filter form.
func chatAuditResetForm() {
	if form := js.Global().Get("document").Call("querySelector", "[data-chataudit=filters]"); form.Truthy() {
		form.Call("reset")
	}
}
