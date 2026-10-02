//go:build js && wasm

package chatui

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var errChatmapAdminInput = errors.New("chatui: location settings input is invalid")

// chatmapAdminCall asks the location settings and answers the status so the panel
// can tell a refusal from a failure. It returns a cancel for the effect.
func chatmapAdminCall(action string, body any, done func(ChatmapAdminData, int, error)) func() {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		var data ChatmapAdminData
		raw, status, err := chatmapRawRequest(ctx, action, body)
		if err == nil {
			err = json.Unmarshal(raw, &data)
		}
		if ctx.Err() == nil {
			done(data, status, err)
		}
	}()
	return cancel
}

func chatmapAdminForm(event ui.Event) js.Value { return event.JSValue().Get("currentTarget") }

// chatmapAdminReadPolicy reads the switches and limits.
func chatmapAdminReadPolicy(event ui.Event) (any, error) {
	form := chatmapAdminForm(event)
	if !form.Truthy() {
		return nil, errChatmapAdminInput
	}
	checked := func(name string) bool { return form.Call("querySelector", "[name="+name+"]").Get("checked").Bool() }
	number := func(name string) int {
		n, _ := strconv.Atoi(form.Call("querySelector", "[name="+name+"]").Get("value").String())
		return n
	}
	live, kept := number("maxlive"), number("retention")
	if live <= 0 || kept <= 0 {
		return nil, errChatmapAdminInput
	}
	policy := map[string]any{"SharingEnabled": checked("sharing"), "LiveEnabled": checked("live"), "ExactAllowed": checked("exact"), "MaxLiveSeconds": live, "MaxRetentionSeconds": kept}
	return map[string]any{"Policy": policy}, nil
}

// chatmapAdminReadCountry reads the country form; a country and its basis are
// both required.
func chatmapAdminReadCountry(event ui.Event) (any, error) {
	form := chatmapAdminForm(event)
	if !form.Truthy() {
		return nil, errChatmapAdminInput
	}
	value := func(name string) string {
		return strings.TrimSpace(form.Call("querySelector", "[name="+name+"]").Get("value").String())
	}
	country, basis := value("country"), value("basis")
	if country == "" || basis == "" {
		return nil, errChatmapAdminInput
	}
	return map[string]any{"Country": country, "Enabled": value("state") == "on", "Basis": basis}, nil
}

// chatmapAdminChangedField is the name of the control a change event came from,
// so the page can say "Saved" beside that setting.
func chatmapAdminChangedField(event ui.Event) string {
	target := event.JSValue().Get("target")
	if !target.Truthy() || target.Get("name").Type() != js.TypeString {
		return ""
	}
	return target.Get("name").String()
}
