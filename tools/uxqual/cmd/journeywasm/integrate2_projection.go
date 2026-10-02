package main

import (
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"strconv"
	"strings"
)

func integrate2ProjectionKey(m chatui.Model) string {
	var key strings.Builder
	key.WriteString(m.CurrentTenantID + "\x00" + m.CurrentUser + "\x00" + m.SelectedID)
	for _, msg := range integrate2ReaderMessages(m) {
		key.WriteString("\x00" + msg.ID + ":" + strconv.FormatUint(msg.Revision, 10))
	}
	if m.ChatFeatures != nil {
		key.WriteString(strconv.FormatBool(m.ChatFeatures.Renderings) + strconv.FormatBool(m.ChatFeatures.Status) + strconv.FormatBool(m.ChatFeatures.Locations))
	}
	return key.String()
}
func integrate2ReaderMessages(m chatui.Model) []chatui.Message {
	out := append([]chatui.Message(nil), m.Messages...)
	out = append(out, m.ThreadMessages...)
	if m.ThreadParent != nil {
		out = append(out, *m.ThreadParent)
	}
	for _, pin := range m.ChannelPins {
		out = append(out, chatui.Message{ID: pin.PostID, Revision: pin.Revision, Body: pin.Body})
	}
	return out
}

// integrate2ApplyPolicyAnswer records only what the server answered. The message
// body in the model was delivered by the server, so a failed request, or an id
// the answer does not mention, adds nothing and the text stays as written
// (CHATBUG-019). A mark is never invented on the client.
func integrate2ApplyPolicyAnswer(selected map[string]chatui.ReaderSelection, messages []chatui.Message, _ []string, answer map[string]chatui.ReaderSelection, err error) {
	integrate2ApplyReaderAnswer(selected, messages, answer, err)
}

// integrate2PrepareReaderSelections used to mark every message without an
// answer as pending or unavailable, which hid text already on the page. It
// now leaves the selections as the server gave them.
func integrate2PrepareReaderSelections(*chatui.Model) {}
