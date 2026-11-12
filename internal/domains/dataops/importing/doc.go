// Package importing is the operator CSV import path for HRIS DataOps
// (planning/todos.md DATAOPS-001..004; see the P1A disposition note at the
// top of "13. HRIS DataOps and configuration lifecycle" in planning/todos.md
// and planning/specs/hris-admin-dataops.md, "1. Data Import and Staging").
//
// This is deliberately an operator CSV seed path, not a product surface: it
// stages, profiles, maps and validates a batch of rows entirely in memory and
// never performs a domain write. Four pipeline stages, each pure and each
// replayable from the last:
//
//	StageCSV / StageBatch  ->  Batch      (DATAOPS-001, immutable, content-addressed)
//	ProfileBatch           ->  Profile    (DATAOPS-002, deterministic column profile)
//	Compile                ->  MappingProfile (DATAOPS-003, source -> canonical mapping)
//	ValidateBatch          ->  Result     (DATAOPS-004, stable per-row error identity)
//
// Every stage is a pure function over its inputs: no clock is read, no
// storage is touched, and no map is iterated when producing a digest or an
// ordered result, so re-running a stage over the same bytes always reproduces
// the same output. DATAOPS-005 (simulating an import as ordered
// BusinessIntents) is DEFERRED per the same disposition note and is not
// implemented here.
//
// The package imports only internal/kernel/*, internal/intent/model (the
// canonical property registry), its draft leaf and the Go standard library. It does not
// import internal/data, internal/connectivity or internal/intent/app, and it
// holds no storage and no connector of its own. The value types that the
// browser client links (batches, drafts, checkpoints) live in the draft leaf,
// so that importing the leaf does not pull the model-bound stages into the
// client graph.
package importing
