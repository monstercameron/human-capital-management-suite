# WF-CTRL-002: Control-flow shapes authors need and how each lowers to the kernel

Status: decision record. The kernel facts below were verified against
`internal/workflow/steptype.go`, `steps.go`, `graph.go`, `compile.go`,
`mode.go`, the `parallel` and `timer` packages, and the cancellation and
subworkflow surfaces. Where the issue's RED clause is stale (DECISION already
routes N ways), the record says so.

## 1. What the kernel expresses today

Thirteen step types: CAPABILITY, DECISION, TRANSFORM, OBSERVE, END (P1A);
APPROVAL, TASK, WAIT, SIGNAL, COMPENSATE (P1B); PARALLEL, JOIN, SUBWORKFLOW
(STRUCTURAL — gated behind P1B evidence, not a release claim). AGENT, RULE,
CHECKPOINT and DOCUMENT are deliberately absent: attributes or capabilities,
not node types.

Three guarantees the kernel never loosens, and every lowering below keeps:

- Every outcome a node can produce needs an explicit edge; there is no
  implicit first edge and no positional default (`steptype.go`: "there is no
  implicit first edge"; DECISION's default route names a declared route).
- Every loop is declared, bounded and guarded: each back edge needs a
  `CycleDeclaration` naming its entry node, a positive `MaxIterations`, and a
  guard node that is a DECISION lying on the path from the entry back to the
  edge source (`graph.go` `checkCycles`). A loop the compiler discovers but
  the author never declared is rejected.
- The compiled graph is flat and digestible: one node list, one edge list,
  one content digest that pins every section (see the promotionexec golden
  pins). No construct below introduces a nested graph the digest cannot see.

DECISION already routes N ways: each route declares a key and a predicate,
routes sharing a predicate declare distinct precedence, and the default names
a declared route (`steps.go` `checkDecisionStep`). The issue RED's "two-way
decisions" describes the common case, not the kernel limit.

PARALLEL fans SUCCEEDED out to a fixed branch set under a MaxBranches bound
with disjoint declared write keys, under FailFast (first failure cancels
siblings) or CollectAll; JOIN aggregates branch terminals into SUCCEEDED,
PARTIAL or FAILED. WAIT completes SUCCEEDED, LATE or CANCELLED off a durable
timer row; SIGNAL completes SUCCEEDED, TIMED_OUT or CANCELLED. SUBWORKFLOW
spawns statically today: collection, event and population spawn sources are
open work owned by WF-EXT-019. Cancellation is instance-level
(`cancellation.Request` names an instance) plus per-node CANCELLED outcomes;
there is no region primitive and no mutual-exclusion primitive — disjoint
write keys forbid the shared state one would guard.

## 2. The catalogue, pattern by pattern

Verdicts: NATIVE (write it today), LOWERABLE (an authoring construct the
compiler lowers to existing step types), KERNEL-CHANGE (the kernel cannot say
it and no lowering is honest).

| # | Pattern | Verdict | Reason |
|---|---------|---------|--------|
| 1 | Sequence | NATIVE | Edges. |
| 2 | Exclusive choice | NATIVE | One DECISION with N predicated routes, precedences and an explicit default. |
| 3 | Multi-choice (static branch set) | LOWERABLE | DECISION-guarded PARALLEL groups, one group per admissible subset; every subset is explicit edges. |
| 4 | Multi-choice (dynamic membership) | KERNEL-CHANGE | Needs collection spawn; owned by WF-EXT-019. |
| 5 | Simple merge | NATIVE | Many edges into one node; no synchronization to declare. |
| 6 | Structured merge | NATIVE | JOIN after PARALLEL; PARTIAL/FAILED terminals are explicit. |
| 7 | Structured loop | NATIVE | A declared bounded DECISION-guarded cycle. |
| 8 | Arbitrary loop | LOWERABLE | Any back edge compiles with a declaration; irreducibly structured flow must be restructured by hand first, which the declaration then pins. |
| 9 | Multiple instances, count known | LOWERABLE | Static PARALLEL fan-out under MaxBranches; the bound is the count. |
| 10 | Multiple instances, count unknown | KERNEL-CHANGE | Needs collection/event spawn; owned by WF-EXT-019. |
| 11 | Deferred choice | LOWERABLE | Bounded OBSERVE polling loop over the candidate sources with an explicit timeout route; the first observed ready source wins and the rest are ignored after the choice. |
| 12 | Deferred choice, event-driven | KERNEL-CHANGE | A first-wins join that cancels the losing waits does not exist; FailFast cancels on failure, not on success. |
| 13 | Milestone | LOWERABLE | An OBSERVE loop that completes when the milestone condition reads true, or a SIGNAL the milestone publisher sends. |
| 14 | Cancel activity | NATIVE | TASK/APPROVAL/SUBWORKFLOW CANCELLED outcomes plus operator cancel of the instance. |
| 15 | Cancel region | KERNEL-CHANGE | No region primitive; cancellation is instance-level or a node outcome. |
| 16 | Interleaved routing | KERNEL-CHANGE | No mutual exclusion; disjoint write keys forbid the shared state it would serialize. |
| 17 | Discriminator (first wins, rest ignored) | KERNEL-CHANGE | JOIN aggregates every branch; no first-wins-and-ignore join exists. |
| 18 | Partial join | LOWERABLE | JOIN's PARTIAL terminal plus an explicit counting guard for the threshold; the full N-of-M threshold join rides with WF-EXT-019. |

## 3. The constructs worth adding first

Each construct states its lowering and the guarantees it keeps. "Flat" means
the lowering is ordinary nodes and edges in the one compiled graph.

### 3.1 Switch on a value or decision table

Lowering: one DECISION, one route per value (or per table row), precedences
resolving overlaps, and an explicit else/default route. No new step type:
the N-route DECISION is the switch. Keeps: exhaustive edges (the else route),
flat, digestible.

### 3.2 For-each over a collection, in order

Lowering: a declared cycle over an index with MaxIterations set to the
collection size and a DECISION guard that leaves past the last element; tiny
fixed sets may unroll to a chain instead. Keeps: bounded by the size,
guarded, flat, digestible. Dynamic-length collections wait on WF-EXT-019.

### 3.3 For-each over a collection, in bounded parallel

Lowering: PARALLEL fan-out with one branch per element and a JOIN, under a
MaxBranches bound the author states. Keeps: the bound, disjoint writes,
explicit join terminal, flat, digestible. Dynamic membership waits on
WF-EXT-019.

### 3.4 Repeat-until with a maximum count and a route for reaching it

Lowering: a declared cycle whose DECISION guard exits on the condition and
whose MaxIterations carries the maximum; reaching the maximum takes the
bound-exhausted edge, never an implicit exit. Keeps: terminates by
construction, explicit bound route, flat, digestible.

### 3.5 First-of (a signal, a timer, a task: the losers are cancelled)

Lowering today: a bounded OBSERVE polling loop over the candidates with an
explicit timeout route; the first ready candidate wins and later arrivals
are ignored once the choice is recorded. The event-driven form (rival SIGNAL
waits, losers cancelled on the first arrival) needs the first-wins join of
row 12 and is a kernel change. Keeps: bounded polls, explicit timeout,
recorded choice, flat, digestible.

### 3.6 Attempt-otherwise (an error boundary with a typed catch route)

Lowering: the guarded CAPABILITY's FAILED edge routes to the catch subgraph;
COMPENSATE nodes undo partial effects on the way. Keeps: explicit catch,
flat, digestible; the boundary is edges, not a new scope.

### 3.7 A timeout route on any waiting node

Already native: WAIT's LATE edge and SIGNAL's TIMED_OUT edge ARE the timeout
routes, fired by durable timer rows. The construct is authoring sugar that
requires the edge. Keeps: durable wake condition, explicit route, flat,
digestible.

### 3.8 Early return

Already native: an edge to an END node. The construct is authoring sugar that
skips the remaining nodes by routing. Keeps: explicit edge, flat,
digestible.

## 4. What stays a kernel change and why

Dynamic multi-choice and multiple instances with an unknown count wait on
WF-EXT-019's spawn sources. Event-driven deferred choice, the discriminator
and cancel region need join/cancellation primitives the kernel does not have:
a first-wins join, and a region scope for cancellation. Interleaved routing
needs mutual exclusion, which contradicts the disjoint-write-keys rule and
should stay out. None of these is reachable by relabelling; each would add a
step type or a join variant with its own conformance row.

## 5. Consequences for authors

Write switches, ordered and bounded-parallel loops, repeat-until,
attempt-otherwise, timeouts and early returns as the lowerings above; the
compiler should offer them as authoring constructs that expand to plain
nodes before the checks run, so every guarantee in section 1 is checked on
the expansion, not trusted from the sugar. Defer dynamic fan-out to
WF-EXT-019 and first-wins joins to a kernel proposal. Never hand-build from
nodes what a construct states once: the construct is the reviewable unit.
