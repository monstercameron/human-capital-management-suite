package productui

import (
	"net/url"
	"strings"
)

// fragmentTargetID turns a location hash ("#agents-setting") into the element
// id it names, or "" when there is nothing to scroll to. The browser's own
// fragment scroll runs before the client has rendered the page, so the client
// repeats it once the target exists.
func fragmentTargetID(hash string) string {
	id := strings.TrimPrefix(strings.TrimSpace(hash), "#")
	if decoded, err := url.PathUnescape(id); err == nil {
		id = decoded
	}
	return id
}
