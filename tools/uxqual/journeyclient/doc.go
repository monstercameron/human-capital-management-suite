// Package journeyclient is the Promotion journey page's browser client:
// the page's state machine deciding which RPC a route or button means,
// what the reader sees while it is in flight, and what the answer
// becomes. It owns no DOM and no connection.
//
// Runtime path (WEB-034): this package compiles to WebAssembly via
// tools/uxqual/cmd/journeywasm (GOOS=js GOARCH=wasm) into journey.wasm,
// which internal/humanwork/workspace embeds (assets.go) and serves at
// the journey shell's asset route (journey_shell.go, PathJourneyWasm)
// alongside the gRPC-over-WebSocket tunnel the client dials. It is
// intentionally outside every Go server binary's dependency closure: it
// ships as a served browser asset, not as server code. The shell file
// documents the client-side contract (config island, tunnel dial,
// per-RPC bearer, render target) that both sides pin.
package journeyclient
