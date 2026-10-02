//go:build js && wasm

package main

import (
	"context"
	"net/http"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// chatperf2AgentSnapshotFetcher reads the deferred agent snapshot from the
// origin the page was served from, with the page's own credential.
func chatperf2AgentSnapshotFetcher(cfg journeyclient.Config) func(context.Context) (journeyclient.Agents, error) {
	return func(ctx context.Context) (journeyclient.Agents, error) {
		return chatperf2FetchAgentSnapshot(ctx, http.DefaultClient, personaChatHTTPConfig(cfg).TunnelURL, cfg.Bearer)
	}
}
