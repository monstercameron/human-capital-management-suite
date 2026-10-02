//go:build !(js && wasm)

package chatui

import "context"

// chatSessionRefresh has no page to refresh outside the browser.
func chatSessionRefresh(context.Context) (string, error) { return "", ErrReadingSignedOut }
