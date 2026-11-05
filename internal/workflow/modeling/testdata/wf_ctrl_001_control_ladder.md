# WF-CTRL-001: How Rules, Decision Models and Agents Control a Workflow

Status: DECIDED 2026-10-23. Owner request 2026-10-01 ("add agent, jev
and deterministic control flow into the workflow engine to make it more
flexible"). The kernel deliberately has no `AGENT` step; this record says
how flexibility is added without undoing why. Linked todos: `WF-CTRL-001`
(this record), `WF-CTRL-004` (model-answered `DECISION`), `WF-EXT-006`
(published rules), `WF-EXT-013` (task forms), `AGENTCOST-002`
(decision port), `WF-EXT-024` (run access policy).

## 1. The ladder: cheapest and most certain first

Prefer the lowest rung whose inputs and failure cost fit the question.
An author must be able to say why a node is not one rung lower.

1. **Rule.** The inputs are structured and the policy can be written
   down. Deterministic, free, replayable. A rule is a `DECISION`
   answered by a published rule (`WF-EXT-006`), never a step type:
   there is no `RULE` node.
2. **Decision model.** The question is a bounded judgement over
   unstructured content — which of these four categories is this
   email, is this request complete — and a wrong answer has a stated
   cost. The model answers inside a `DECISION` (section 2) with a
   threshold, an abstain route and a recorded answer (`WF-CTRL-004`).
3. **Agent.** The output is text a person reads, or the work needs
   tools and several steps. An agent is an agent-eligible
   `CAPABILITY` (section 2), governed by the signed scope exchange
   (section 3) and the prohibitions (section 6).
4. **Person.** Authority or accountability requires one: approvals,
   attestations, employment decisions, and every route a model or
   agent must shed load onto (section 5).

## 2. Where each sits in the kernel: no new step type

- A decision model is an **evaluator kind on `DECISION`**. The node
  keeps its declared routes, its threshold and its recorded answer;
  only the evaluator changes from rule to model.
- An agent is an **agent-eligible `CAPABILITY`**. Eligibility is the
  capability manifest's `agent_eligible` attribute, not a node type;
  the capability gateway executes it with the manifest's bindings.
- The retired step types stay retired. `AGENT` was specified and
  retired on 2026-09-02 (`WF-STEP-012`) because an agent is a
  capability with a manifest, not a node type. `RULE`, `CHECKPOINT`
  and `DOCUMENT` are likewise absent: they are attributes or
  capabilities, not primitives (`internal/workflow/steptype.go`,
  "Kernel Vocabulary").

## 3. The signed scope exchange

`WF-STEP-012` left production agent use out "until a signed scope
exchange". For an agent capability to run in a served workflow, the
exchange states all six, and it is the published agent version's own
bindings — never the caller's paraphrase:

1. the **principal** the agent acts as;
2. the **data scope** it may read;
3. the **tools** it may invoke;
4. the **budget** bounding its cost (`AGENTCOST-002` decision port
   prices the run);
5. the **effect class** it may commit, gated by a rule or a person
   (section 6);
6. **who signed** the exchange, and which published agent version
   they signed.

## 4. Determinism: the record is the output

A model-backed node's answer, confidence, model identity, prompt
digest and cost are its recorded node output. Replay, recovery and
simulation read the record; they never ask the model again. Only a
new attempt asks again, and the new attempt is itself a recorded
output on the same node. The run stays replayable because everything
a model said is data the run already holds.

## 5. Explicit routes, no implicit default

Every model-backed node declares explicit routes for each of:

- **abstained or below threshold** — the model declined or its
  confidence missed the bar;
- **unavailable** — the model or gateway could not be reached;
- **refused** — policy refused the call;
- **budget exhausted** — the run's cost budget ran out.

Each route leads to a rule, a person, or a stated end. There is no
implicit default route: an unhandled model outcome is a compile-time
refusal, not a runtime guess.

## 6. What a model-backed node may never do

1. Commit an effect that a rule or a person has not gated. The
   triage contract (`internal/workflow/conformance/triage`) is the
   worked example: read-only agent execution, deterministic rules
   route the risky and unknown cases to a specialist, and final
   evidence always states the agent never made an employment
   decision.
2. Widen the run's access. Agent execution inherits the run's
   participant wall and purpose (`WF-EXT-024`); it grants nothing.
3. See data outside the run's access policy. Classification,
   purpose and need-to-know bind the model exactly as they bind
   every other node.
