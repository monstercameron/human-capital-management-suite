package main

import "sync"

// CHATBUG-059. A read that failed and was tried again put its failure line
// above the conversation and left it there after the retry succeeded: "We
// couldn't load the member list" over a member list that was on screen. The
// notice remembers which action wrote it. When that same action succeeds, the
// line goes; a line another action wrote is not touched.

// chatNoticeToken is the number of the notice on screen.
func (s *chatState) noticeTokenNow() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.noticeToken
}

var chatNoticeActions = struct {
	sync.Mutex
	byAction map[string]uint64
}{byAction: map[string]uint64{}}

// rememberChatNoticeAction records that action wrote the notice with token.
func rememberChatNoticeAction(action string, token uint64) {
	if action == "" || token == 0 {
		return
	}
	chatNoticeActions.Lock()
	chatNoticeActions.byAction[action] = token
	chatNoticeActions.Unlock()
}

// takeChatNoticeAction forgets and returns the token action's failure wrote.
func takeChatNoticeAction(action string) uint64 {
	chatNoticeActions.Lock()
	defer chatNoticeActions.Unlock()
	token := chatNoticeActions.byAction[action]
	delete(chatNoticeActions.byAction, action)
	return token
}

// resetChatNoticeActions forgets every action, for a new session.
func resetChatNoticeActions() {
	chatNoticeActions.Lock()
	chatNoticeActions.byAction = map[string]uint64{}
	chatNoticeActions.Unlock()
}
