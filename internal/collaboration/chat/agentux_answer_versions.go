package chat

import (
	"regexp"
	"strconv"
	"strings"
)

func DisplayVersion(sequence uint64) string {
	return "v" + strconv.FormatUint(sequence, 10) + ".0.0"
}

// Existing semantic labels win; legacy revision sequences have one display
// convention until the publication owner supplies semantic version labels.
func DisplayVersionLabel(label string) string {
	label = strings.TrimSpace(label)
	if regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`).MatchString(label) {
		return "v" + strings.TrimPrefix(label, "v")
	}
	label = strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(label, "version "), "Version "), "v")
	sequence, err := strconv.ParseUint(label, 10, 64)
	if err != nil || sequence == 0 {
		return ""
	}
	return DisplayVersion(sequence)
}
