//go:build !js || !wasm

package chatui

import "errors"

var errChattoneRewordNative = errors.New("chatui: reword settings need a browser")

// Without a browser there is no server to ask: the row asks nothing and draws
// nothing, which is what native rendering tests look at.
func chattoneRewordLoad(_ string, _ func(ChattoneRewordState, error)) func() { return nil }
func chattoneRewordSend(_ string, _ map[string]any, done func(ChattoneRewordState, error)) {
	done(ChattoneRewordState{}, errChattoneRewordNative)
}
func chattoneRewordNotAvailable(error) bool { return false }
