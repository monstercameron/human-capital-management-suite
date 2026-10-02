package chat

import (
	"context"
	"strings"
)

// A poll or a to-do list is a message whose body carries the card after
// Chatcmd002BodyMarker. Only the card service writes such a body: a person who
// types the marker by hand (or an old client that re-sends one) would otherwise
// post data the page draws as a card, with options and tasks nobody validated.

type cardAuthorityKey struct{}

// withCardAuthority marks a context as the card service's own. The key is
// unexported, so nothing outside this package can mint it.
func withCardAuthority(ctx context.Context) context.Context {
	return context.WithValue(ctx, cardAuthorityKey{}, true)
}

func hasCardAuthority(ctx context.Context) bool {
	granted, _ := ctx.Value(cardAuthorityKey{}).(bool)
	return granted
}

// carriesCardMarker reports whether a body holds the card marker.
func carriesCardMarker(body string) bool {
	return strings.Contains(body, Chatcmd002BodyMarker)
}
