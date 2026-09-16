// Package releasefixture is the fixture evidence a compiled workflow version
// must carry before it can be approved for activation (WF-COMP-006).
//
// A [Report] is produced by running a [Suite] against one exact
// [version.CompiledVersion]. It names the version's compiled-plan digest and
// record digest, the result of every fixture the version declared at
// publication, and its own content digest. [Verify] is the structural check a
// durable store applies to a report it is asked to keep: the report is intact,
// bound to this exact version, covers exactly the declared fixtures, and every
// one passed. [Reproduce] is the governed approval's check: it runs [Verify]
// and then re-runs every declared fixture in-process, so a hand-written report
// claiming a pass the fixtures do not reproduce is refused.
//
// The package is pure: no clock, no database, no context. Callers supply the
// run instant and the suite.
package releasefixture

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// Format is the report format identity; a digest is computed under it, so a
// report of another format can never verify as this one.
const Format = "hcmnext.workflow.fixture-report/v1"

var (
	// ErrMalformed reports a report that does not decode or names no runner,
	// run instant or format.
	ErrMalformed = errors.New("releasefixture: malformed fixture report")
	// ErrReportDigest reports a report whose content no longer matches its
	// own digest.
	ErrReportDigest = errors.New("releasefixture: fixture report digest does not match its content")
	// ErrVersionMismatch reports a report produced for a different version
	// (workflow, compiled-plan digest or record digest).
	ErrVersionMismatch = errors.New("releasefixture: fixture report is not bound to this version")
	// ErrNoDeclaredFixtures reports a version that declared no fixtures at
	// publication, so no report can prove it.
	ErrNoDeclaredFixtures = errors.New("releasefixture: the version declares no fixtures")
	// ErrFixtureMissing reports a declared fixture with no result, or a
	// result for a fixture the version did not declare.
	ErrFixtureMissing = errors.New("releasefixture: fixture results do not match the declared fixtures")
	// ErrFixtureFailed reports a declared fixture whose recorded result failed.
	ErrFixtureFailed = errors.New("releasefixture: a declared fixture failed")
	// ErrNotReproduced reports a recorded pass the in-process suite does not
	// reproduce, or a declared fixture the suite cannot run.
	ErrNotReproduced = errors.New("releasefixture: a recorded fixture pass was not reproduced")
)

// Fixture checks one compiled version and returns nil when it passes.
type Fixture func(version.CompiledVersion) error

// Suite maps a fixture reference, as a version declares it in
// [version.PublishMeta.FixtureRefs], to the check that proves it.
type Suite map[string]Fixture

// Result is one fixture's outcome.
type Result struct {
	FixtureRef string `json:"fixture_ref"`
	Passed     bool   `json:"passed"`
	Detail     string `json:"detail,omitempty"`
}

// Report is the fixture evidence for one exact compiled version.
type Report struct {
	Format             string    `json:"format"`
	WorkflowID         string    `json:"workflow_id"`
	CompiledPlanDigest string    `json:"compiled_plan_digest"`
	RecordDigest       string    `json:"record_digest"`
	CompilerVersion    string    `json:"compiler_version"`
	Runner             string    `json:"runner"`
	RanAt              time.Time `json:"ran_at"`
	Results            []Result  `json:"results"`
	ReportDigest       string    `json:"report_digest"`
}

// Run executes every fixture v declares, in declaration order, and returns
// the sealed report. A declared fixture the suite has no check for is
// recorded as failed rather than skipped.
func Run(suite Suite, v version.CompiledVersion, runner string, at time.Time) Report {
	r := Report{
		Format: Format, WorkflowID: v.WorkflowID, CompiledPlanDigest: v.CompiledPlanDigest,
		RecordDigest: v.Digest(), CompilerVersion: v.CompilerVersion, Runner: runner, RanAt: at.UTC(),
		Results: make([]Result, 0, len(v.FixtureRefs)),
	}
	for _, ref := range v.FixtureRefs {
		r.Results = append(r.Results, runOne(suite, ref, v))
	}
	return r.Seal()
}

func runOne(suite Suite, ref string, v version.CompiledVersion) Result {
	fixture, ok := suite[ref]
	if !ok || fixture == nil {
		return Result{FixtureRef: ref, Detail: "no fixture is registered for this reference"}
	}
	if err := fixture(v); err != nil {
		return Result{FixtureRef: ref, Detail: err.Error()}
	}
	return Result{FixtureRef: ref, Passed: true}
}

// Seal returns r with ReportDigest set to its content digest.
func (r Report) Seal() Report {
	r.Results = append([]Result(nil), r.Results...)
	r.ReportDigest = r.ComputeDigest()
	return r
}

// ComputeDigest is the report's content digest: sha256 under [Format] over
// every field except ReportDigest.
func (r Report) ComputeDigest() string {
	r.ReportDigest = ""
	r.RanAt = r.RanAt.UTC()
	b, err := json.Marshal(r)
	if err != nil {
		b = []byte("unencodable:" + err.Error())
	}
	h := sha256.New()
	h.Write([]byte(Format))
	h.Write([]byte{0})
	h.Write(b)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// Passed reports whether the report holds at least one result and every
// result passed.
func (r Report) Passed() bool {
	if len(r.Results) == 0 {
		return false
	}
	for _, res := range r.Results {
		if !res.Passed {
			return false
		}
	}
	return true
}

// FixtureRefs returns the fixture references the report holds results for.
func (r Report) FixtureRefs() []string {
	refs := make([]string, 0, len(r.Results))
	for _, res := range r.Results {
		refs = append(refs, res.FixtureRef)
	}
	return refs
}

// Encode renders the report as indented JSON.
func (r Report) Encode() ([]byte, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	return append(b, '\n'), nil
}

// Decode parses a report; unknown fields are refused.
func Decode(b []byte) (Report, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var r Report
	if err := dec.Decode(&r); err != nil {
		return Report{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	return r, nil
}

// Verify proves r is intact, bound to exactly v, covers exactly the fixtures
// v declared, and records every one as passed.
func Verify(r Report, v version.CompiledVersion) error {
	if r.Format != Format || strings.TrimSpace(r.Runner) == "" || r.RanAt.IsZero() {
		return fmt.Errorf("%w: format, runner and run instant are required", ErrMalformed)
	}
	if r.ReportDigest == "" || r.ReportDigest != r.ComputeDigest() {
		return ErrReportDigest
	}
	if r.WorkflowID != v.WorkflowID || r.CompiledPlanDigest != v.CompiledPlanDigest || r.RecordDigest != v.Digest() {
		return fmt.Errorf("%w: report names %s@%s (record %s), version is %s@%s (record %s)", ErrVersionMismatch,
			r.WorkflowID, r.CompiledPlanDigest, r.RecordDigest, v.WorkflowID, v.CompiledPlanDigest, v.Digest())
	}
	if len(v.FixtureRefs) == 0 {
		return fmt.Errorf("%w: %s", ErrNoDeclaredFixtures, v.CompiledPlanDigest)
	}
	declared := append([]string(nil), v.FixtureRefs...)
	reported := r.FixtureRefs()
	sort.Strings(declared)
	sort.Strings(reported)
	if strings.Join(declared, "\x00") != strings.Join(reported, "\x00") {
		return fmt.Errorf("%w: declared %v, reported %v", ErrFixtureMissing, declared, reported)
	}
	for _, res := range r.Results {
		if !res.Passed {
			return fmt.Errorf("%w: %s: %s", ErrFixtureFailed, res.FixtureRef, res.Detail)
		}
	}
	return nil
}

// Reproduce runs [Verify] and then re-runs every declared fixture through
// suite, refusing [ErrNotReproduced] when any recorded pass does not hold now.
func Reproduce(r Report, v version.CompiledVersion, suite Suite) error {
	if err := Verify(r, v); err != nil {
		return err
	}
	for _, ref := range v.FixtureRefs {
		if res := runOne(suite, ref, v); !res.Passed {
			return fmt.Errorf("%w: %s: %s", ErrNotReproduced, ref, res.Detail)
		}
	}
	return nil
}
