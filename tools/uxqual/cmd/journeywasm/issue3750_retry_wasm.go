//go:build js && wasm

package main

import "github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"

// retryChatWorkPreviews reads every ticket and document card that failed to
// load again, as the current viewer (issue 3750).
func retryChatWorkPreviews(cfg journeyclient.Config) {
	active := chatBrowser.config(cfg)
	identity := active.Tenant + "\x00" + active.Subject
	chatDocPreviews.retryFailed(identity)
	chatProjectTaskPreviews.retryFailed(identity)
	resolveVisibleChatDocs(cfg)
	resolveVisibleChatProjectTasks(cfg)
}
