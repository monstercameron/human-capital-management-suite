// Package hireexec is the second EXECUTE-mode workflow definition proving the
// engine's EXECUTE driver (internal/workflow/execute) and runtime
// (internal/workflow/runtime) are workflow-agnostic: they take Steps,
// WorkItems, Terminal, Timers and Signals as caller-supplied ports, and plan
// selection is a caller-supplied resolver, exactly as they are for
// internal/workflow/promotionexec. This package proves the same driver can
// run a wholly different business process -- "New employee hire" -- end to
// end, with no change to the driver or the runtime.
//
// The graph, start to its success exit: prepare_hire (TRANSFORM) ->
// approve_offer (APPROVAL by the hiring manager) -> await_background_check
// (SIGNAL) -> evaluate_background_check (DECISION) -> [collect_new_hire_forms
// directly on a CLEAR result, or review_adverse_result (TASK for HR) first on
// an ADVERSE one] -> collect_new_hire_forms (TASK) -> provision_system_access
// (TASK) -> provision_workspace (TASK) -> enroll_payroll (TASK) ->
// await_start_date (WAIT) -> commit_hire (CAPABILITY, the one authoritative
// write) -> end_hired (COMPLETED).
//
// Known engine limits this definition is deliberately designed around, not
// worked around:
//
//   - WF-EXT-019: PARALLEL and JOIN do not run in EXECUTE
//     (internal/workflow/runtime/advance.go's loadFrontierState refuses any
//     plan containing a JOIN with CodeJoinsNotDurable). The four provisioning
//     tasks (collect_new_hire_forms, provision_system_access,
//     provision_workspace, enroll_payroll) therefore run in one fixed
//     SEQUENCE rather than fanning out concurrently; that ordering is a
//     property of this definition's edges, not of a business rule that
//     forbids concurrency.
//   - WF-EXT-012: a WAIT node's wake condition is a literal baked into the
//     compiled definition (internal/workflow/steps/wait.FromCompiled parses
//     it directly); this definition's await_start_date therefore declares an
//     unused placeholder instant, and the real per-run wake instant -- the
//     candidate's own start date, carried on the bound proposal's effective
//     time -- is substituted by the caller's execute.TimerFactory before the
//     wake requirement is computed. See test/workflow/hire_execute_test.go's
//     hireTimerFactory.
//   - WF-EXT-014: SIGNAL correlation is the closed start-time vocabulary
//     internal/platform/execution/signals.go's DefaultCorrelation declares.
//     await_background_check correlates on "proposal.intent_id" so the
//     background-check provider's callback is matched to this run without
//     depending on any payload field.
//   - WF-EXT-004: node inputs are not resolved on the durable path, so the
//     caller's execute.StepRunner (test/workflow/hire_execute_test.go's
//     hireSteps) derives what a READY node needs -- including
//     evaluate_background_check's CLEAR/ADVERSE route -- from the bound
//     proposal's own revision, never from a compiled InputMapping.
package hireexec
