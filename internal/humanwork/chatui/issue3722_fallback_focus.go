package chatui

import "strings"

// issue3722MessageSelector is the selector of the message a layer was opened
// from. A message's hover bar is drawn only while the pointer is over it, so
// once a thread closes the Reply in thread button that opened it may be gone;
// focus then goes to the message itself (an article with tabindex 0) rather
// than falling to the page (issue 3722). It is empty when there is no id.
func issue3722MessageSelector(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	escaped := strings.NewReplacer(`\`, `\`, `"`, `\"`).Replace(id)
	return `.message[data-message-id="` + escaped + `"]`
}
