# LEAVE-013 return commit

Completed `LEAVE-013` by adding the atomic ReturnFromLeave commit to the existing leave domain package: verified readiness gates the return, every active restriction is preserved exactly, the four restoration effects are queued and observed without trusting providers, and closing requires a versioned obligation policy.

## What landed

- `internal/domains/leave/return_commit.go`: mutex-guarded `ReturnCommitter` appends one `LEAVE_ENDED` record (new `BusinessLeaveEnded` vocabulary in `commit.go`) across seven receipts: leave-ended revision, availability restoration, restriction carryover, ledger events, projections, effect outbox, obligation policy.
- Return before verified READY/READY_WITH_RESTRICTIONS readiness refuses; a discarded or forged restriction refuses; inactive employment refuses; missing restoration refuses; failpoints roll everything back; duplicates return the identical record.
- Failed providers keep local return truth with PENDING obligation plus targeted repair that never opens a second local transaction.

## Verification

- `TestTodo_LEAVE_013` plus RACE/INTEGRATION/RECOVERY/MUTATION matrix in `internal/domains/leave/return_commit_test.go` pass.
- `go test -count=1 ./internal/domains/leave/` PASS (87.1% of statements, floor is 70%); `go vet` and `gofmt` clean. `-race` needs cgo on this host and runs in CI.
- RED was observed first (undefined symbols), then GREEN after the implementation edit.

The shared checkout contains unrelated concurrent work (`M .claude/settings.json`). That file was not staged, reset, or rewritten as part of this slice.
