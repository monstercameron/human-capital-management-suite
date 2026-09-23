package chatui

import (
	"strings"
	"testing"
)

func TestMobileConversationHeaderKeepsTitleAndShortcutsVisible(t *testing.T) {
	css := Stylesheet
	contracts := []string{
		`@media(max-width:760px){.conversation-header{height:auto;min-height:52px;flex-wrap:wrap;align-content:center;`,
		`.conversation-header>.conversation-title{order:2;flex:1 1 0%;min-inline-size:0}`,
		`.conversation-header>.conversation-actions{order:3;flex:0 0 100%;justify-content:flex-end;`,
		`.conversation-header .channel-todo-trigger,.conversation-header .channel-poll-trigger{flex:none}`,
		`@media(max-width:350px){.channel-todo-trigger-label,.channel-poll-trigger-label{display:none}`,
	}
	for _, contract := range contracts {
		if !strings.Contains(css, contract) {
			t.Errorf("mobile conversation header layout missing %q", contract)
		}
	}
	if strings.Contains(css, `.conversation-title{flex:0 1 auto}`) {
		t.Fatal("conversation title must not shrink to zero beside mobile header actions")
	}
}
