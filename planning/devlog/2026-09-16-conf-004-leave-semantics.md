# CONF-004 leave and return-to-work semantics

Completed `CONF-004` with a P1A leave-and-return reference workflow that walks the real SIMULATE-mode interpreter, mirroring the CONF-005 termination pattern.

## What landed

- `internal/workflow/conformance/leavereturn/`: reference definition (employment/authority read, independent program resolution, sealed restricted review decision, proposal transform, benefits OBSERVE with bounded repair route, readiness capability, readiness-gated return decision, seven terminals), canned zero-effect environment, decision/transform/read/approval ports, setup with golden and RED params, eight-test matrix plus `testdata/conf004_leave.golden`.
- Degraded benefits observations route to a repair-linked terminal without rolling back valid leave; manager-determined eligibility, unsealed medical evidence and not-ready returns are blocked terminals; omitted legal context lands at unknown, never a false completion; forged terminal codes never verify.
- JOIN REQUIRED_SET semantics over the payroll/benefits/schedule restoration branches prove unknown stays unknown; the walked path fits the declared decomposition budget.

## Verification

- All eight `TestTodo_CONF_004*` tests pass; package 79.9% statements (floor 70%); `go vet` and `gofmt` clean. `-race` needs cgo on this host and runs in CI. RED was observed first (compiler MISSING_ROUTE on the readiness node, fixed with standard capability routes; omitted-context expectation corrected to the unknown terminal per the termination precedent).

The shared checkout contains unrelated concurrent work (`M .claude/settings.json`). That file was not staged, reset, or rewritten as part of this slice.
