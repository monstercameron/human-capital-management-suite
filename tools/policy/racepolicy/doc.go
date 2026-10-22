// Package racepolicy: rationale (TOOL-012).
//
// # Why not just `go test -race`
//
// `-race` is unavailable on this development host (windows/arm64 has no
// race detector support at all — the Go toolchain simply refuses the
// flag). .github/workflows/tests.yml's go-core job already runs
// `go test -race -count=1` on Linux CI, which is where the actual race
// class gets proven; that step is not this package's job to duplicate, and
// this package's own test suite runs, and must pass, without -race, on this
// host.
//
// That step takes its package list from this package, via `racepolicy
// -list` (see cmd/racepolicy): the set that is raced and the set this
// policy audits are one scan, so they cannot drift. It used to be a blanket
// `./...`, which raced every PostgreSQL-backed data package for no
// race-class signal and could not finish inside the job's wall clock. The
// packages -list leaves out are not skipped; the same job sweeps every
// measurable package through covergate with the detector off.
//
// What a blind `-race -count=1 ./...` cannot prove on its own is
// *coverage*: it races whatever tests already exist, but says nothing
// about a concurrent package that has no tests at all, or whose only test
// file happens to be excluded by the `!race` build constraint. The go
// command defines the `race` build tag whenever `-race` is enabled, so a
// `race`-tagged test does run in that CI job.
// A concurrent package with zero raceable tests passes `go test -race
// ./...` today for the same reason an untested package passes `go test
// ./...`: there is nothing there to fail.
//
// # What this package does instead
//
// FindConcurrentPackages uses go/build's file matching for the same
// linux/amd64, cgo-enabled, race-tagged context as the CI job, then uses
// go/ast to declare packages whose selected production or test files import
// "sync" or "sync/atomic", or contain a `go` statement. This excludes
// platform-specific code that the Linux race job does not compile, such as
// JS/WASM-only browser hosts. Evaluate then asks go/build whether a test
// file resolves for each declared package in this exact race build context.
// A package with zero resolving test files fails the policy: its concurrent
// Linux code is included in the race package list without a suite to run.
//
// # Known live-repository finding (not this lane's file root)
//
// Running Evaluate against this repository as of this package's authoring
// finds exactly one violation:
// internal/authn/federation/registry.go declares a `sync.RWMutex` field and
// ships no _test.go file at all, so no test resolves for it under any
// build context, racing or not. internal/authn/federation is outside
// tools/policy/racepolicy's file root, so this package does not attempt to
// fix it; TestTodo_TOOL_012_Golden pins this exact finding as a golden
// snapshot precisely so it is visible (and so the golden test itself
// forces an update, rather than silently going stale, whenever that gap
// closes or a new one appears elsewhere).
package racepolicy
