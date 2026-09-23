//go:build js && wasm

package main

import (
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestChatTodoAddPolicyRequestShape(t *testing.T) {
	selected := []chatui.ChannelTodoSelectedMember{{HomeTenantID: "home", SubjectID: "sam"}}
	for _, mode := range []string{"EVERYONE", "ME", "ME_AND_SELECTED"} {
		request := &chatv1.MutateChannelTodoListRequest{Operation: "ADD"}
		applyChatTodoPolicy(request, chatTodoPolicy{mode: mode, selected: selected})
		if request.GetCompletionMode() != mode {
			t.Fatalf("%s mode = %q", mode, request.GetCompletionMode())
		}
		if mode != "ME_AND_SELECTED" && len(request.GetSelectedCompleters()) != 0 {
			t.Fatalf("%s leaked selected identities", mode)
		}
		if mode == "ME_AND_SELECTED" && (len(request.GetSelectedCompleters()) != 1 || request.GetSelectedCompleters()[0].GetHomeTenantId() != "home") {
			t.Fatalf("selected policy request = %#v", request.GetSelectedCompleters())
		}
	}
}

func TestMobilePinJumpClosesDetailsOverlay(t *testing.T) {
	oldModel, oldRerender := chatBrowser.snapshot(), chatRerender
	chatRerender = func() {}
	t.Cleanup(func() {
		chatBrowser.mutate(func(model *chatui.Model) { *model = oldModel })
		chatRerender = oldRerender
	})
	chatBrowser.mutate(func(model *chatui.Model) { model.ShowDetails = true })
	closeChatDetailsForPinJump(320)
	if chatBrowser.snapshot().ShowDetails {
		t.Fatal("mobile pin jump left overlay covering the target")
	}
	chatBrowser.mutate(func(model *chatui.Model) { model.ShowDetails = true })
	closeChatDetailsForPinJump(1440)
	if !chatBrowser.snapshot().ShowDetails {
		t.Fatal("desktop pin jump closed inline details")
	}
}
