# UI interaction latency gate

The production UI uses a measured p95 quality gate for local interaction work.
Every measurement runs warmups first and then records at least 20 samples using
the nearest-rank percentile. A breach fails CI with the interaction name, p50,
p95, maximum, sample count, and budget.

The initial product budgets are:

| Interaction               | Workload                                                                | p95 budget |
| ------------------------- | ----------------------------------------------------------------------- | ---------: |
| Validation feedback       | Render a localized error summary and linked invalid fields              |      16 ms |
| Loading feedback          | Render the network-backed page loading proxy                            |      16 ms |
| People query              | Filter, sort, paginate, and render 100 rows from 10,000 indexed workers |     100 ms |
| Page transition           | Render each registered leaf page                                        |      50 ms |
| Persistent shell          | Render global chrome around an existing leaf                            |      50 ms |
| Reusable table            | Render a 1,000 by 12 cell matrix                                        |      75 ms |
| Journey workforce preview | Render the bounded preview from 10,000 workers                          |      16 ms |

These are client-compute budgets. Network and database SLOs are measured at
their own boundaries; they must not be hidden inside this gate. The loading
feedback budget ensures the UI can acknowledge that slower work immediately.

Route and loading visual checks use the same package's `LayoutShiftBudget` /
`CheckLayoutShift` contract. The default frontend CLS ceiling is 0.1 per page
load or route transition; callers pass the browser's layout-shift entries to
`EvaluateLayoutShift`, which sums them and rejects malformed negative, NaN, or
infinite telemetry.

Run the same gate as CI from the repository root:

```text
go test -count=1 -run ^TestInteractionLatencyGate$ -v ./internal/humanwork/productui ./tools/uxqual/render/journey
```

Do not run this measurement under the race detector: race instrumentation is a
correctness gate and intentionally changes timing and allocation behavior.
