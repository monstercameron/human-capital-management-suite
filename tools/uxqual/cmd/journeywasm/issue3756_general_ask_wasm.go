//go:build js && wasm

package main

import (
	"context"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	agentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/agent/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3756: Chat asks the built-in General agent through the same Agents
// service the Agents page uses, on the same tunnel. The question becomes the
// person's own quick-answer task; nothing is posted to the conversation.

var chatGeneralAgentConn struct {
	sync.Mutex
	conn grpc.ClientConnInterface
}

func configureChatGeneralAgent(conn grpc.ClientConnInterface) {
	chatGeneralAgentConn.Lock()
	chatGeneralAgentConn.conn = conn
	chatGeneralAgentConn.Unlock()
}

// chatGeneralAgentReason is the part of a failure the person can act on.
func chatGeneralAgentReason(err error) string {
	switch status.Code(err) {
	case codes.FailedPrecondition:
		return "disabled"
	case codes.PermissionDenied, codes.Unauthenticated:
		return "denied"
	}
	return ""
}

// chatAskGeneralAgent starts the task off the page's thread and reports the
// outcome back on it.
func chatAskGeneralAgent(cfg journeyclient.Config, question string, done func(chatui.GeneralAgentAnswer, error)) {
	chatGeneralAgentConn.Lock()
	conn := chatGeneralAgentConn.conn
	chatGeneralAgentConn.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), agentCallTimeout)
		defer cancel()
		binding := newAgentServiceBinding(chatBrowser.config(cfg), conn)
		task, err := binding.StartTaskModeWithSelection(ctx, question, agentv1.AgentStartMode_AGENT_START_MODE_QUICK_ANSWER, nil, "")
		answer := chatui.GeneralAgentAnswer{TaskID: task.ID, State: string(task.State), Answer: strings.TrimSpace(task.AnswerText)}
		if err != nil {
			answer = chatui.GeneralAgentAnswer{Reason: chatGeneralAgentReason(err)}
		}
		ui.PostAsync(func() { done(answer, err) })
	}()
}
