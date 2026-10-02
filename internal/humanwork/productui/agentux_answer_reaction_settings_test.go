package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestAgentUXQuality_ReactionSettings(t *testing.T) {
	on, off := true, false
	if !agentAnswerReactionsEnabled(nil, nil) || agentAnswerReactionsEnabled(&off, nil) || !agentAnswerReactionsEnabled(&off, &on) || agentAnswerReactionsEnabled(&on, &off) {
		t.Fatal("workspace default or conversation override ignored")
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		copy := agentAnswerReactionSettingsCopy(locale)
		props := AgentAnswerReactionSettingsProps{Locale: locale, Configured: true, Editable: true, Save: func(bool, func(error)) {}}
		markup, err := ui.RenderToString(ui.CreateElement(ChatSettingsPage, ChatSettingsPageProps{AgentReactions: &props}))
		if err != nil || !strings.Contains(markup, `role="switch"`) || !strings.Contains(markup, `aria-checked="true"`) || !strings.Contains(markup, copy[0]) || !strings.Contains(markup, copy[1]) || !strings.Contains(markup, copy[2]) {
			t.Fatalf("accessible localized switch %s: %s %v", locale, markup, err)
		}
		props.Conversation = &off
		props.Save = nil
		markup, err = ui.RenderToString(ui.CreateElement(AgentAnswerReactionSettings, props))
		if err != nil || !strings.Contains(markup, `aria-checked="false"`) || !strings.Contains(markup, "disabled") {
			t.Fatalf("unconfigured switch not disabled: %s %v", markup, err)
		}
	}
}
