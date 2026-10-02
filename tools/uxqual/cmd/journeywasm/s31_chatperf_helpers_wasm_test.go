//go:build js && wasm

package main

import "testing"

// The source-reading helpers of chatperf_load_test.go read this package's .go
// files, which a browser test host cannot open, so that file is excluded from
// the wasm test build. The tests that still compile there and call the helpers
// (chatperf2_requests_test.go and others) pin the order of calls in source and
// are run by the native build; here they skip.

func chatperfBody(t *testing.T, file, signature string) string {
	t.Helper()
	t.Skip("reads Go source files; checked by the native test run")
	return ""
}

func chatperfInOrder(t *testing.T, where, body string, pieces ...string) {
	t.Helper()
	t.Skip("reads Go source files; checked by the native test run")
}
