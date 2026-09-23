//go:build js && wasm

package chatui

import (
	"syscall/js"
	"testing"
)

func TestChannelWidgetDOMActionsReadCurrentFieldsAndManagerPin(t *testing.T) {
	global := js.Global()
	previous := global.Get("document")
	defer global.Set("document", previous)
	values := map[string]string{"channel-team-role-0": "Facilitator", "channel-milestone-text-0": "Review", "channel-milestone-status-0": "BLOCKED", "channel-milestone-owner-0": widgetOwnerToken("guest", "same"), "channel-milestone-date-0": "2026-10-01"}
	get := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return js.Null()
		}
		value, ok := values[args[0].String()]
		if !ok {
			return js.Null()
		}
		el := global.Get("Object").New()
		el.Set("value", value)
		return el
	})
	defer get.Release()
	doc := global.Get("Object").New()
	doc.Set("getElementById", get)
	global.Set("document", doc)
	var roleHome, roleSubject, roleLabel, pinKind string
	var pinValue bool
	var updated ChannelProjectMilestone
	m := Model{Members: []Member{{ID: "same", HomeTenantID: "host"}, {ID: "same", HomeTenantID: "guest"}}, ChannelTeam: ChannelTeamWidget{CanPin: true, Members: []ChannelTeamMember{{HomeTenantID: "guest", SubjectID: "same"}}}, ChannelProject: ChannelProjectWidget{CanPin: false, Milestones: []ChannelProjectMilestone{{ID: "ms"}}}, Callbacks: Callbacks{SetChannelTeamRoleLabel: func(h, s, l string) { roleHome, roleSubject, roleLabel = h, s, l }, SetChannelWidgetPinned: func(k string, v bool) { pinKind, pinValue = k, v }, UpdateChannelProjectMilestone: func(item ChannelProjectMilestone) { updated = item }}}
	m.act("team-role-save", "0")
	m.act("team-pin", "")
	m.act("project-pin", "")
	m.act("milestone-save", "ms")
	if roleHome != "guest" || roleSubject != "same" || roleLabel != "Facilitator" {
		t.Fatalf("role callback %q/%q %q", roleHome, roleSubject, roleLabel)
	}
	if pinKind != "TEAM" || !pinValue {
		t.Fatalf("pin callback %q/%t", pinKind, pinValue)
	}
	if updated.ID != "ms" || updated.Status != "BLOCKED" || updated.OwnerHomeTenantID != "guest" || updated.OwnerSubjectID != "same" || updated.DueDate != "2026-10-01" {
		t.Fatalf("milestone callback %+v", updated)
	}
	m.Members[0], m.Members[1] = m.Members[1], m.Members[0]
	updated = ChannelProjectMilestone{}
	m.act("milestone-save", "ms")
	if updated.OwnerHomeTenantID != "guest" || updated.OwnerSubjectID != "same" {
		t.Fatalf("reordered roster changed owner: %+v", updated)
	}
}

func TestChannelWidgetEditedSelectSurvivesUnrelatedRerender(t *testing.T) {
	global := js.Global()
	el := global.Get("Object").New()
	el.Set("value", "DONE")
	version := "room:3"
	attributes := map[string]string{selectValueAttr: "BLOCKED", selectVersionAttr: version, "data-chat-select-editable": "true"}
	get := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return js.Null()
		}
		if value, ok := attributes[args[0].String()]; ok {
			return value
		}
		return js.Null()
	})
	defer get.Release()
	el.Set("getAttribute", get)
	el.Set("__chatSelectEditedVersion", version)
	applySelectValue(el)
	if got := el.Get("value").String(); got != "DONE" {
		t.Fatalf("unsaved select reverted to %q", got)
	}
	attributes[selectVersionAttr] = "room:4"
	applySelectValue(el)
	if got := el.Get("value").String(); got != "BLOCKED" {
		t.Fatalf("saved revision did not sync select: %q", got)
	}
}
