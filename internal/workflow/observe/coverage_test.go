package observe_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// uninstrumentedByDesign lists the exported, context-taking workflow functions
// that deliberately open no operation, each with the reason. Everything else
// that takes a context must bracket itself with observe.Begin/observe.Start so
// its status and failures reach spans and logs.
var uninstrumentedByDesign = map[string]string{
	// No-op, in-memory and test-double implementations: nothing happens to trace.
	"execute/evidence.go NoopExecutionEvidence.RecordExecutionEvidence": "no-op",
	// Transactional twin of the no-op recorder above: still nothing to trace.
	"execute/evidence.go NoopExecutionEvidence.RecordExecutionEvidenceTx": "no-op",
	"execute/telemetry.go NoopInstrumentation.TraceID":                    "no-op",
	"execute/telemetry.go NoopInstrumentation.StartNodeSpan":              "no-op",
	"execute/telemetry.go NoopInstrumentation.StartAdvanceSpan":           "no-op",
	"execute/telemetry.go NoopInstrumentation.StartTerminalSpan":          "no-op",
	"execute/telemetry.go NoopInstrumentation.StartResumeSpan":            "no-op",
	"progress/sweep.go NoopObserver.StartSweep":                           "no-op",
	"progress/sweep.go NoopObserver.Stuck":                                "no-op",
	"recover/failpoint.go NoFailpoint.Check":                              "no-op",
	"recover/failpoint.go CrashAt.Check":                                  "test failpoint",
	"replay/source.go MemorySource.Load":                                  "in-memory",
	"execute/repair_record.go MemoryRepairRecords.LoadRepairRecords":      "in-memory",
	"execute/repair_record.go MemoryRepairRecords.AppendRepairRecord":     "in-memory",
	"replay/source.go sourceFunc.Load":                                    "function adapter",
	"runtime/continuation.go MemorySink.RequireWorkItem":                  "in-memory",
	"runtime/continuation.go MemorySink.RequireSignalSubscription":        "in-memory",
	"runtime/continuation.go MemorySink.RequireTimer":                     "in-memory",
	"runtime/continuation.go MemorySink.MarkReady":                        "in-memory",
	"runtime/continuation.go MemorySink.Complete":                         "in-memory",
	"runtime/proposal_facts.go MemoryProposalFacts.Supersession":          "in-memory",
	"runtime/proposal_facts.go MemoryApprovalFacts.Decisions":             "in-memory",
	"shadow/run.go StepRunnerFunc.Run":                                    "function adapter",
	"simulate/nodes.go invocationSink.RecordInvocation":                   "in-memory simulation sink",
	"simulate/ports.go noApprovals.WouldAwait":                            "unbound simulation port",
	"simulate/ports.go unboundTransforms.Transform":                       "unbound simulation port",
	"simulate/ports.go unboundReads.Observe":                              "unbound simulation port",
	"observe/observetest/recorder.go Recorder.Context":                    "test recorder",
	"observe/observetest/recorder.go Recorder.Start":                      "test recorder",

	// Pure delegation: the callee it forwards to is instrumented.
	"execute/driver.go transactionalResolver.ResolveWorkflow":             "delegates to ResolveWorkflowInTx",
	"execute/driver.go fixedResolver.ResolveWorkflow":                     "returns a fixed selection",
	"execute/effects/resolver.go PolicyResolver.ResolveWorkflow":          "delegates to ResolveWorkflowInTx (instrumented)",
	"runtime/continuation.go ContinuationStore.RequireWorkItem":           "delegates to the instrumented execute sink",
	"runtime/continuation.go ContinuationStore.RequireSignalSubscription": "delegates to the instrumented execute sink",
	"runtime/continuation.go ContinuationStore.RequireTimer":              "delegates to the instrumented execute sink",
	"runtime/continuation.go ContinuationStore.MarkReady":                 "delegates to the instrumented execute sink",
	"runtime/continuation.go ContinuationStore.Complete":                  "delegates to the instrumented execute sink",
	"shadow/run.go Run": "constructs a Runner whose Run is instrumented",
	"draftcompile/compile.go CapabilityPolicyFunc.AllowsCapability": "function adapter; its caller opens the authoring operation",

	// Hot-path reads called inside an instrumented operation; tracing each
	// would duplicate the parent's span without adding status.
	"lease/fenced.go Fenced.VerifyFence":                          "read inside instrumented fenced advance",
	"lease/lease.go Manager.Observe":                              "read inside instrumented recovery/scheduler serve",
	"lease/lease.go Manager.History":                              "evidence read",
	"runtime/store.go Store.LoadInstance":                         "read inside every instrumented runtime write",
	"runtime/store.go Store.LoadNodeExecution":                    "read inside instrumented node transition",
	"runtime/store.go Store.LoadNodeExecutions":                   "read inside instrumented operations",
	"runtime/store_batch.go Store.LoadInstances":                  "batched read inside instrumented list projection",
	"runtime/store_batch.go Store.LoadNodeExecutionsForInstances": "batched read inside instrumented list projection",
	"timer/timer.go Scheduler.Load":                               "read",
	"timer/timer.go Scheduler.Pending":                            "read inside instrumented cancel/migrate",
	"migrate/artifacts/store.go storeLeases.Current":              "read inside instrumented artifact migration",
	"migrate/artifacts/store.go storeTimers.Pending":              "read inside instrumented artifact migration",
	"migrate/artifacts/store.go storeSignals.Open":                "read inside instrumented artifact migration",
	"migrate/artifacts/store.go storeReadyWork.Pending":           "read inside instrumented artifact migration",
	"migrate/artifacts/store.go storeApprovals.Pending":           "read inside instrumented artifact migration",
	"migrate/artifacts/store.go storeChildren.Links":              "read inside instrumented artifact migration",
	"replay/adapters.go Recorder.Attempt":                         "replay trace recorder inside instrumented Replay",
	"replay/adapters.go guardedAdapter.Invoke":                    "replay adapter inside instrumented Replay",
	"shadow/recorder.go Recorder.Read":                            "shadow trace recorder inside instrumented shadow run",
	"shadow/recorder.go Recorder.Refuse":                          "shadow trace recorder inside instrumented shadow run",
	"simulate/approvals.go HumanWorkApprovals.WouldAwait":         "pure simulation port inside instrumented simulate.Run",
	"simulate/decisions.go RulesDecisions.Decide":                 "pure simulation port inside instrumented simulate.Run",
	"simulate/reads.go ProjectionReads.Observe":                   "pure simulation port inside instrumented simulate.Run",

	// The seam itself.
	"execute/fence.go WithFence": "context value helper; the fence it carries is verified inside the instrumented advance",
	"version/durable.go BindTx":  "binds a store to the caller's transaction without I/O; the resolve it serves runs inside the instrumented start",

	"execute/redeliver.go FenceFromContext": "context value reader; the fence it returns is verified inside the instrumented guarded step",

	"observe/observe.go Begin":        "the seam",
	"observe/observe.go Start":        "the seam",
	"observe/observe.go WithRecorder": "the seam",
	"observe/observe.go RecorderFrom": "the seam",
}

// TestWorkflowEngineOperationsAreInstrumented proves every exported,
// context-taking function in the workflow engine opens a telemetry operation
// (span plus structured log) unless it is a documented exemption, and that no
// exemption goes stale.
func TestWorkflowEngineOperationsAreInstrumented(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var missing []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if d.Name() == "testdata" || rel == "conformance" || strings.HasPrefix(rel, "conformance/") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !fn.Name.IsExported() || !takesContext(fn) {
				continue
			}
			body := string(src[fset.Position(fn.Body.Pos()).Offset:fset.Position(fn.Body.End()).Offset])
			if strings.Contains(body, "observe.Begin(") || strings.Contains(body, "observe.Start(") {
				continue
			}
			name := fn.Name.Name
			if fn.Recv != nil && len(fn.Recv.List) > 0 {
				recv := string(src[fset.Position(fn.Recv.List[0].Type.Pos()).Offset:fset.Position(fn.Recv.List[0].Type.End()).Offset])
				name = strings.TrimPrefix(recv, "*") + "." + name
			}
			key := rel + " " + name
			seen[key] = true
			if _, ok := uninstrumentedByDesign[key]; !ok {
				missing = append(missing, key)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(missing)
	for _, key := range missing {
		t.Errorf("%s takes a context but opens no observe operation; bracket it with observe.Begin or document why not", key)
	}
	var stale []string
	for key := range uninstrumentedByDesign {
		if !seen[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	for _, key := range stale {
		t.Errorf("exemption %q no longer matches an uninstrumented function; remove it", key)
	}
}

func takesContext(fn *ast.FuncDecl) bool {
	for _, p := range fn.Type.Params.List {
		if sel, ok := p.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "Context" {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "context" {
				return true
			}
		}
	}
	return false
}

// discardAllowed names calls whose result the engine may discard: a deferred
// rollback after commit, and the observe seam that has already recorded the
// outcome.
var discardAllowed = map[string]bool{"Rollback": true, "Done": true, "DoneWith": true, "Finish": true}

// TestWorkflowEngineDoesNotDiscardContextualFailures refuses `_ = f(ctx, ...)`
// in the engine: a context-bound call whose failure is thrown away leaves no
// span, log or result behind. A failure that is deliberately non-fatal must
// still be recorded, by bracketing the call and discarding only observe.Done.
func TestWorkflowEngineDoesNotDiscardContextualFailures(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || len(as.Rhs) != 1 {
				return true
			}
			for _, lhs := range as.Lhs {
				if id, ok := lhs.(*ast.Ident); !ok || id.Name != "_" {
					return true
				}
			}
			call, ok := as.Rhs[0].(*ast.CallExpr)
			if !ok {
				return true
			}
			name := ""
			switch fn := call.Fun.(type) {
			case *ast.SelectorExpr:
				name = fn.Sel.Name
			case *ast.Ident:
				name = fn.Name
			}
			if discardAllowed[name] {
				return true
			}
			for _, arg := range call.Args {
				if id, ok := arg.(*ast.Ident); ok && (id.Name == "ctx" || strings.HasSuffix(id.Name, "Ctx")) {
					found = append(found, rel+":"+strconv.Itoa(fset.Position(as.Pos()).Line)+" "+name)
					break
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(found)
	for _, f := range found {
		t.Errorf("%s discards a context-bound failure without recording it; bracket it with observe.Begin and discard only observe.Done", f)
	}
}
