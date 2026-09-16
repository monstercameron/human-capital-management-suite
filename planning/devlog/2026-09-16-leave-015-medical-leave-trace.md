# LEAVE-015 medical leave trace

Completed `LEAVE-015` by adding the canonical Medical Leave logical-persistence trace: one deterministic, sealed 15-stage proof from intent/evidence intake through the final execution receipt, using only the real domain functions with hypothetical rules and public capabilities.

## What landed

- `internal/domains/leave/medical_trace.go`: `TraceMedicalLeave` walks anchor, snapshot, eligibility, plan, simulation, proposal, restricted review, determination (rendered plus acknowledged blocking-notice delivery), leave-start compile, leave-start commit, extension successor, effect reconciliation, readiness, return commit and execution receipt. Any stage failure aborts with the stage named.
- Degraded benefits observations reconcile to PENDING obligation with a proven repair path instead of rolling back valid leave; revisions advance seals instead of overwriting; raw untyped evidence, overlapping leave, terminated employment, pre-readiness return and free-text restrictions all refuse.
- `internal/domains/leave/testdata/leave015_trace.golden` pins the stage order and seals.

## Verification

- `TestTodo_LEAVE_015` plus PROPERTY/GOLDEN/RACE/FAULT/SECURITY/CONFORMANCE matrix pass.
- `go test -count=1 ./internal/domains/leave/` PASS (85.5% of statements, floor is 70%); `go vet` and `gofmt` clean. `-race` needs cgo on this host and runs in CI. Golden generated with `HCMNEXT_UPDATE_GOLDEN=1` via the Windows shell (WSL env forwarding drops the variable).

The shared checkout contains unrelated concurrent work (`M .claude/settings.json`). That file was not staged, reset, or rewritten as part of this slice.
