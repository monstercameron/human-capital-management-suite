package main

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"

// integrate2ApplyReaderAnswer folds one reading answer into the selections the
// client will commit. The message body in the model was delivered by the
// server, so a failed or slow request adds nothing and the text stays as
// written. Only what the server itself answered is recorded: a rendering
// selected for this reader, or the server's own mark that withholds the
// original.
func integrate2ApplyReaderAnswer(selected map[string]chatui.ReaderSelection, messages []chatui.Message, answer map[string]chatui.ReaderSelection, err error) {
	if err != nil {
		return
	}
	for id, value := range answer {
		for _, msg := range messages {
			if msg.ID == id {
				value.Revision = msg.Revision
				break
			}
		}
		selected[id] = value
	}
}
