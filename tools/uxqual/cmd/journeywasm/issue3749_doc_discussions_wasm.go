//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// documentDiscussionsLoader is the Docs page's read of the conversations that
// mention an open document (issue 3749). The reply is already limited by the
// server to what this person may open.
func documentDiscussionsLoader(cfg journeyclient.Config) func(string, func(productui.DocumentDiscussions, error)) {
	return func(documentID string, done func(productui.DocumentDiscussions, error)) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			done(documentDiscussionsCall(ctx, http.DefaultClient, chatBrowser.config(cfg), documentID))
		}()
	}
}
