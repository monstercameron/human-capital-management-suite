# ELIG-008 four-domain eligibility parity

Completed `ELIG-008` with a parity gate proving Benefits, Leave, Learning and Rewards share the eligibility engine's status, time, binding and unknown semantics while retaining domain-specific rules.

## What landed

- `internal/engines/eligibility/parity.go`: `CheckDomainParity` evaluates the four domain fixtures through the shared `Evaluate` and seals a `ParityReport`. Exactly-four membership, request validity, compiled plans, result binding, shared effective-time semantics, distinct subject matters and a non-mono rule union are all enforced; any divergence refuses so it blocks shared engine publication. `Verify` re-seals over the same cases.
- `internal/engines/eligibility/elig008_test.go`: PRIMARY/PROPERTY/CONFORMANCE matrix reusing the package's fixture helpers.

## Verification

- All three `TestTodo_ELIG_008*` tests pass; package 77.3% statements (floor 70%); `go vet` and `gofmt` clean. `-race` needs cgo on this host and runs in CI. One RED expectation was corrected during the work: fact-reader failures resolve UNKNOWN through the shared vocabulary (engine semantics, never denial), so the test asserts the sealed unknown instead of a refusal.

The shared checkout contains unrelated concurrent work (`M .claude/settings.json`). That file was not staged, reset, or rewritten as part of this slice.
