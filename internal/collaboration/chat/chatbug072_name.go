package chat

import (
	"fmt"
	"strings"
	"unicode"
)

// MaxChannelNameRunes is the longest name a new channel may have.
const MaxChannelNameRunes = 80

// ErrChannelName is returned when a new channel's name breaks the naming rule.
// It is an invalid-argument condition the transport reports against the name
// field, so the form can say why next to the box instead of failing the whole
// request.
var ErrChannelName = fmt.Errorf("%w: channel name", ErrInvalidArgument)

// NormalizeChannelName applies the naming rule to what a person typed: letters
// become lowercase, spaces become dashes, and letters, digits, dashes and
// underscores are kept. Everything else is dropped and the second result says
// so (as it does when the name was cut at MaxChannelNameRunes), so the form can
// name the rule next to the field. The create dialog and the service use this
// one function, which is why a name the form accepts is a name the service
// accepts.
func NormalizeChannelName(typed string) (name string, refused bool) {
	var out strings.Builder
	count := 0
	for _, r := range typed {
		switch {
		case unicode.IsSpace(r):
			r = '-'
		case unicode.IsUpper(r) || unicode.IsTitle(r):
			r = unicode.ToLower(r)
		}
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) || r == '-' || r == '_') {
			refused = true
			continue
		}
		if count == MaxChannelNameRunes {
			refused = true
			break
		}
		out.WriteRune(r)
		count++
	}
	return out.String(), refused
}

// ValidChannelName reports whether name already follows the naming rule as it
// stands: nothing to rewrite, and at least one letter or digit so the name can
// be read and searched.
func ValidChannelName(name string) bool {
	normal, refused := NormalizeChannelName(name)
	if refused || normal != name {
		return false
	}
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// ReasonChannelName is the reason reference a transport reports for
// ErrChannelName, and the one a client matches to show the rule under the name
// field.
const ReasonChannelName = "chat.channel_name_invalid"
