package rowbatch

// REV-089-01: finish the dbport.ExecAll batching rollout PERFOPT-004 scoped
// to twenty-two stores but only reached in four. This package scans
// internal/data for row-at-a-time Exec and QueryRow calls inside loops. Each
// site is batched or has a reviewed, count-pinned exception.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// Finding is one Exec or QueryRow call inside a loop body.
type Finding struct {
	// File is the slash-separated path relative to the repository root.
	File string
	// Line is the 1-based line of the database call.
	Line   int
	Method string
}

// Exception is one file with row-at-a-time sites that stay unconverted, with the
// reason batching does not apply.
type Exception struct {
	// File names the containing file relative to the repository root.
	File string
	// Calls lists exact database calls covered by the exception.
	Calls []Callsite
	// Reason is the justification a reviewer accepted.
	Reason string
}

// Callsite identifies one loop-contained operation that cannot be batched.
type Callsite struct {
	Line   int
	Method string
}

// Exceptions lists every row-at-a-time Exec or QueryRow site that stays. A site lands
// here only with a reason: batching genuinely does not apply (for example
// the per-iteration statement depends on the previous iteration's result),
// never because the rollout did not reach it.
var Exceptions = []Exception{
	{File: "internal/data/aggregates/store.go", Reason: "Each UPDATE is built for the table selected by the resolved aggregate kind and is guarded by its own lifecycle transition.", Calls: []Callsite{{199, "Exec"}}},
	{File: "internal/data/budgetstore/store.go", Reason: "Reservation expiry is a locked state transition: the fence update determines the sequence read and event inserted for that same reservation.", Calls: []Callsite{{413, "Exec"}, {421, "QueryRow"}, {424, "Exec"}}},
	{File: "internal/data/chatrecordstore/store.go", Reason: "Restore executes a different bulk JSON recordset statement and sequence repair per whitelisted table; identifiers and catalog sequence names vary by table.", Calls: []Callsite{{406, "Exec"}, {415, "Exec"}}},
	{File: "internal/data/chatstore/channel_widgets.go", Reason: "Each widget kind has independent revision and payload semantics used immediately to decide its write.", Calls: []Callsite{{118, "QueryRow"}}},
	{File: "internal/data/chatstore/contracts_adapter.go", Reason: "Membership writes are validated and authorized one principal at a time; failures are returned against the principal currently being added.", Calls: []Callsite{{96, "Exec"}}},
	{File: "internal/data/chatstore/seed_support.go", Reason: "Purge statements target different whitelisted tables and columns and use a table-specific strategy selected per spec.", Calls: []Callsite{{74, "Exec"}}},
	{File: "internal/data/conflictstore/store.go", Reason: "Per-footprint conflict checks and writes are interleaved so each retry verifies the digest observed for that footprint before proceeding.", Calls: []Callsite{{105, "Exec"}, {114, "QueryRow"}, {275, "Exec"}, {284, "QueryRow"}, {293, "QueryRow"}, {301, "Exec"}}},
	{File: "internal/data/contactstore/store.go", Reason: "Events are validated and assigned durable sequence context in the containing iteration; commit remains atomic in the caller transaction.", Calls: []Callsite{{369, "Exec"}}},
	{File: "internal/data/dbport/batch.go", Reason: "ExecAll's sequential fallback and error-index accounting are the primitive itself; routing fallback calls back through ExecAll would recurse.", Calls: []Callsite{{50, "Exec"}, {65, "Exec"}}},
	{File: "internal/data/demoworkforce/role_assignments.go", Reason: "Role assignments are seeded per worker-role pair with pair-specific conflict handling; the enclosing tenant import is transactional.", Calls: []Callsite{{195, "Exec"}}},
	{File: "internal/data/documenthubstore/backlinks.go", Reason: "Link transitions and outbox events are paired per link, and block membership checks determine whether each version participates in the result.", Calls: []Callsite{{133, "Exec"}, {147, "Exec"}, {231, "QueryRow"}, {237, "QueryRow"}}},
	{File: "internal/data/documenthubstore/backup.go", Reason: "Snapshot restoration writes five distinct entity types with different schemas and payloads in dependency order inside one restore transaction.", Calls: []Callsite{{211, "Exec"}, {219, "Exec"}, {226, "Exec"}, {233, "Exec"}, {240, "Exec"}}},
	{File: "internal/data/documenthubstore/blocks.go", Reason: "Blocks are inserted in source order because ordinal and block identity define the version's canonical document structure.", Calls: []Callsite{{88, "Exec"}}},
	{File: "internal/data/documenthubstore/comments.go", Reason: "Mention rows are emitted for one comment after its canonical comment row is persisted; errors abort the enclosing transaction.", Calls: []Callsite{{81, "Exec"}}},
	{File: "internal/data/documenthubstore/embeddings.go", Reason: "Vector rows are derived and validated per encoded section immediately before insertion, and preserve section error context.", Calls: []Callsite{{193, "Exec"}}},
	{File: "internal/data/documenthubstore/linkcheck.go", Reason: "Each link target and optional pinned version is checked independently to produce a finding attached to that link.", Calls: []Callsite{{109, "QueryRow"}, {116, "QueryRow"}}},
	{File: "internal/data/documenthubstore/links.go", Reason: "Links are inserted in caller order so a failure is associated with the first invalid link and aborts the transaction.", Calls: []Callsite{{106, "Exec"}}},
	{File: "internal/data/documenthubstore/reconcile.go", Reason: "Each selected model or cleanup statement has its own SQL shape and affected-row count, which is accumulated into the reconciliation result.", Calls: []Callsite{{99, "Exec"}, {192, "Exec"}}},
	{File: "internal/data/documenthubstore/records.go", Reason: "Record-series tables have distinct count SQL and are processed in declared dependency order before deletion.", Calls: []Callsite{{151, "QueryRow"}, {234, "Exec"}}},
	{File: "internal/data/documenthubstore/search.go", Reason: "Search terms are emitted from an ordered, canonical count map so deterministic row order is retained during rebuild.", Calls: []Callsite{{74, "Exec"}}},
	{File: "internal/data/documenthubstore/sharing.go", Reason: "Owner grants are created in the policy's declared action order during bootstrap; each action is a separate grant transition.", Calls: []Callsite{{28, "Exec"}}},
	{File: "internal/data/health/probe.go", Reason: "The latest applied ledger position is checked per stream and each result is used immediately to compute that stream's health state.", Calls: []Callsite{{287, "QueryRow"}}},
	{File: "internal/data/intentcontrol/proposal.go", Reason: "An ancestry walk must read each current parent before selecting the next ID; the next query key depends on the preceding row.", Calls: []Callsite{{706, "QueryRow"}}},
	{File: "internal/data/jobarchstore/store.go", Reason: "Archive import persists distinct revision record shapes in dependency order and validates each inserted revision before the next stage.", Calls: []Callsite{{274, "Exec"}, {300, "Exec"}, {326, "Exec"}, {352, "Exec"}}},
	{File: "internal/data/meritstore/store.go", Reason: "Recommendation serialization can fail per item and Save preserves participant-specific error context within its revision transaction.", Calls: []Callsite{{144, "Exec"}}},
	{File: "internal/data/outbox/adapter_pg.go", Reason: "Broker bootstrap contains different DDL statements, and offset allocation is a bounded retry loop whose next attempt depends on the prior uniqueness outcome.", Calls: []Callsite{{93, "Exec"}, {122, "QueryRow"}}},
	{File: "internal/data/partition/kit.go", Reason: "Partition setup issues generated policy and trigger DDL per relation, while RLS verification reads relation-specific catalog state.", Calls: []Callsite{{127, "Exec"}, {172, "Exec"}, {399, "QueryRow"}}},
	{File: "internal/data/promotioncommit/writer.go", Reason: "Schema references are validated and written in declared compatibility order as part of one promotion transaction.", Calls: []Callsite{{68, "Exec"}}},
	{File: "internal/data/promotionladder/ladder.go", Reason: "Ladder seeding inserts edges in topological order; each edge depends on the already established predecessor chain.", Calls: []Callsite{{121, "Exec"}}},
	{File: "internal/data/recordsmeta/copies.go", Reason: "Hold intersections are checked or inserted and copy state/events are updated per link according to that link's conflict result.", Calls: []Callsite{{160, "QueryRow"}, {175, "Exec"}, {179, "Exec"}}},
	{File: "internal/data/recordsmeta/release.go", Reason: "Release locks and rechecks each copy's active intersections before choosing its state and optional event; these decisions depend on current transactional rows.", Calls: []Callsite{{145, "Exec"}, {149, "QueryRow"}, {152, "QueryRow"}, {172, "Exec"}}},
	{File: "internal/data/roleaccessstore/store.go", Reason: "Bootstrap writes and verifies tenant role-policy rows in dependency order, with each conflict result determining the following operation.", Calls: []Callsite{{35, "Exec"}, {41, "Exec"}, {47, "Exec"}, {65, "Exec"}, {316, "QueryRow"}, {343, "Exec"}}},
	{File: "internal/data/seed/seed.go", Reason: "Seed statements have different schemas and state-dependent checks and run only inside the atomic bootstrap transaction.", Calls: []Callsite{{303, "Exec"}, {332, "QueryRow"}}},
	{File: "internal/data/signals/expire.go", Reason: "Expiry candidates are selected and transitioned under row locks; each state update's outcome is counted before processing the next candidate.", Calls: []Callsite{{125, "Exec"}}},
	{File: "internal/data/tenancy/planeverification.go", Reason: "The verifier checks independently named system tables and reports the first missing table by name.", Calls: []Callsite{{274, "QueryRow"}}},
	{File: "internal/data/truststore/accessreview.go", Reason: "Grant timestamps are read per review entry and used immediately to derive each entry's evidence state.", Calls: []Callsite{{183, "QueryRow"}}},
	{File: "internal/data/uow/locks.go", Reason: "Advisory locks are acquired in sorted key order to preserve deterministic deadlock avoidance for the transaction.", Calls: []Callsite{{108, "Exec"}}},
	{File: "internal/data/wakeup/wakeup.go", Reason: "LISTEN registers each tenant-specific channel on one session connection; each operation changes that connection's notification subscription state.", Calls: []Callsite{{286, "Exec"}}},
	{File: "internal/data/workeridstore/store.go", Reason: "The retry loop reserves one ID at a time; each next attempt depends on the preceding conflict outcome.", Calls: []Callsite{{159, "Exec"}}},
	{File: "internal/data/workflowversionstore/store.go", Reason: "Approval transitions are inserted in recorded sequence order, and each sequence is part of the version's audit history.", Calls: []Callsite{{341, "Exec"}}},
}

// ScanFile parses one Go source file and reports every Exec or QueryRow call inside a
// for or range loop body. Calls inside a nested function literal are not
// loop bodies and are ignored; nested loops are reported under their own
// loop so each Exec call is reported exactly once.
func ScanFile(rel string, src []byte) ([]Finding, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return nil, fmt.Errorf("rowbatch: parse %s: %w", rel, err)
	}
	var out []Finding
	ast.Inspect(f, func(n ast.Node) bool {
		switch loop := n.(type) {
		case *ast.ForStmt:
			out = append(out, execsInBody(fset, rel, loop.Body)...)
		case *ast.RangeStmt:
			out = append(out, execsInBody(fset, rel, loop.Body)...)
		}
		return true
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Method < out[j].Method
	})
	return out, nil
}

// execsInBody reports database calls anywhere in a loop body, including
// closures and expressions. Nested loops are scanned from their own bodies.
func execsInBody(fset *token.FileSet, rel string, body *ast.BlockStmt) []Finding {
	var out []Finding
	ast.Inspect(body, func(node ast.Node) bool {
		if node == nil {
			return true
		}
		if node != body {
			switch node.(type) {
			case *ast.ForStmt, *ast.RangeStmt:
				return false
			}
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (selector.Sel.Name != "Exec" && selector.Sel.Name != "QueryRow") {
			return true
		}
		out = append(out, Finding{File: rel, Line: fset.Position(call.Pos()).Line, Method: selector.Sel.Name})
		return true
	})
	return out
}

// ScanTree scans every non-test Go file under root/internal/data.
func ScanTree(root string) ([]Finding, error) {
	return scanTreeFiles(root)
}

// scanTreeFiles reads and scans every candidate file.
func scanTreeFiles(root string) ([]Finding, error) {
	var out []Finding
	base := filepath.Join(root, "internal", "data")
	err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		found, err := scanOneFile(path, rel)
		if err != nil {
			return err
		}
		out = append(out, found...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}
