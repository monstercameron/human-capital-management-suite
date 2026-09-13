package workflowmaturity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/designownership"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/intentcoverage"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/scenariomatrix"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdecisions"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdesign"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdesignjoin"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowexpansion"
)

const schemaVersion = 1

// Blocker codes. Each names one distinct clause of WF-DISC-012's RED.
const (
	UnboundDesign              = "UNBOUND_DESIGN"
	UnresolvedReference        = "UNRESOLVED_REFERENCE"
	UnjustifiedOmission        = "UNJUSTIFIED_OMISSION"
	MissingAdversarialScenario = "MISSING_ADVERSARIAL_SCENARIO"
	DanglingTodoEdge           = "DANGLING_TODO_EDGE"
	DanglingTestEdge           = "DANGLING_TEST_EDGE"
	DanglingEvidenceEdge       = "DANGLING_EVIDENCE_EDGE"
	UnresolvedDecision         = "UNRESOLVED_DECISION"
	CatalogNameOnly            = "CATALOG_NAME_ONLY"
)

// bareNotApplicable mirrors the inline finding code
// tools/planning/workflowdesign uses for an unjustified NOT_APPLICABLE
// marker; workflowdesign does not export a constant for it.
const bareNotApplicable = "BARE_NOT_APPLICABLE"

// statusRank locally mirrors tools/planning/intentcoverage's own EdgeStatus
// ladder order. intentcoverage.EdgeStatus.rank is unexported, so the
// vocabulary (the vetted five constants) is imported but the ordering is
// restated here rather than copying private behavior.
var statusRank = map[intentcoverage.EdgeStatus]int{
	intentcoverage.Conceptual:  0,
	intentcoverage.Catalogued:  1,
	intentcoverage.Contracted:  2,
	intentcoverage.Implemented: 3,
	intentcoverage.Verified:    4,
}

func rank(s intentcoverage.EdgeStatus) int { return statusRank[s] }

// Blocker is one exact, source-attributed maturity gap.
type Blocker struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// DefinitionResult is the complete maturity finding for one accepted
// definition. AllowedStatus is capped at CATALOGUED whenever Blockers is
// non-empty, regardless of what ClaimedStatus (tools/planning/intentcoverage's
// own bottom-up ladder) reports; Capped records whether that ceiling
// actually bit.
type DefinitionResult struct {
	Definition    string                    `json:"definition"`
	Intent        string                    `json:"intent,omitempty"`
	ClaimedStatus intentcoverage.EdgeStatus `json:"claimed_status"`
	AllowedStatus intentcoverage.EdgeStatus `json:"allowed_status"`
	Capped        bool                      `json:"capped"`
	Blockers      []Blocker                 `json:"blockers,omitempty"`
}

// Report is the complete WF-DISC-012 gate result: one row per accepted
// definition, never one aggregate boolean.
type Report struct {
	SchemaVersion    int                `json:"schema_version"`
	TotalDefinitions int                `json:"total_definitions"`
	BlockedCount     int                `json:"blocked_count"`
	Results          []DefinitionResult `json:"results"`
	Digest           string             `json:"digest"`
}

// Violations returns every definition still capped below its claimed
// status - the exact set of blockers a caller must resolve.
func (r Report) Violations() []DefinitionResult {
	var out []DefinitionResult
	for _, res := range r.Results {
		if len(res.Blockers) > 0 {
			out = append(out, res)
		}
	}
	return out
}

// JSON returns deterministic indented report JSON.
func (r Report) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal workflowmaturity report: %w", err)
	}
	return append(b, '\n'), nil
}

// Snapshot is the complete typed input the maturity gate reconciles. Every
// field is data already produced by the owning sibling package; Reconcile
// performs no I/O and re-derives no owner's own findings.
type Snapshot struct {
	// Accepted lists every definition_ref (e.g.
	// "hcmnext.people.change_manager/v1") the gate must report on.
	Accepted []string

	// Records and RecordFindings are workflowdesign's (WF-DISC-005) own
	// validated records and per-record findings (keyed by Intent display
	// name; workflowdesign.Finding carries no Definition field).
	Records        []workflowdesign.DesignRecord
	RecordFindings []workflowdesign.Finding

	// Join and JoinFindings are workflowdesignjoin's (WF-DISC-006) own
	// one-to-one join over Accepted.
	Join         workflowdesignjoin.Join
	JoinFindings []workflowdesignjoin.Finding

	// GraphFindings is workflowexpansion's (WF-DISC-007) per-definition
	// expansion findings. A definition present in Records with no entry
	// (or a nil entry) here and no findings has genuinely never been
	// expanded.
	Graphs        map[string]*workflowexpansion.Graph
	GraphFindings map[string][]workflowexpansion.Finding

	// Ownership and OwnershipFindings are designownership's (WF-DISC-009)
	// compiled register; OwnershipFindings carries a Definition field
	// directly.
	Ownership         designownership.Ownership
	OwnershipFindings []designownership.Finding

	// Scenarios and ScenarioFindings are scenariomatrix's (WF-DISC-010)
	// per-definition generation result.
	Scenarios        map[string]*scenariomatrix.Matrix
	ScenarioFindings map[string][]scenariomatrix.Finding

	// Decisions is workflowdecisions' (WF-DISC-003) validated decision
	// set. No live production sidecar is wired anywhere in the repository
	// yet (see loader.go); a live Snapshot therefore always carries a nil
	// or empty slice here, which is an honest reflection of the current
	// repository rather than a suppressed check - see doc.go and
	// maturity_test.go for the fixture-driven proof that the check itself
	// fires correctly once a decision does exist.
	Decisions []workflowdecisions.Decision

	// CatalogMappedIntents is the set of intent display names
	// (workflowarchetypes.CatalogRow.Intent, WF-DISC-002) placed in one of
	// the six HR catalogs, used to distinguish "never designed, only
	// catalogued" from "designed but joined incorrectly".
	CatalogMappedIntents map[string]bool

	// IntentNodes and Orphans are tools/planning/intentcoverage's (GOV-026)
	// own reconciled per-intent status ladder and unallowlisted orphans
	// (Report.Intents and Report.NewOrphans), keyed by bare intent id
	// (a definition_ref with its "/vN" suffix removed).
	IntentNodes []intentcoverage.IntentNode
	Orphans     []intentcoverage.Orphan
}

// Reconcile compiles one Report from snap. It is a pure function: no I/O,
// no clock, no map-iteration-order leakage.
func Reconcile(snap Snapshot) Report {
	recordByDefinition := make(map[string]workflowdesign.DesignRecord, len(snap.Records))
	for _, r := range snap.Records {
		recordByDefinition[r.Definition] = r
	}
	findingsByIntent := make(map[string][]workflowdesign.Finding)
	for _, f := range snap.RecordFindings {
		findingsByIntent[f.Intent] = append(findingsByIntent[f.Intent], f)
	}
	boundDefinition := make(map[string]bool, len(snap.Join.Bindings))
	for _, b := range snap.Join.Bindings {
		boundDefinition[b.Definition] = true
	}
	joinFindingsByDefinition := make(map[string][]workflowdesignjoin.Finding)
	for _, f := range snap.JoinFindings {
		if f.Definition != "" {
			joinFindingsByDefinition[f.Definition] = append(joinFindingsByDefinition[f.Definition], f)
		}
	}
	ownershipFindingsByDefinition := make(map[string][]designownership.Finding)
	for _, f := range snap.OwnershipFindings {
		ownershipFindingsByDefinition[f.Definition] = append(ownershipFindingsByDefinition[f.Definition], f)
	}
	openDecisionsByKey := make(map[string][]string)
	for _, d := range snap.Decisions {
		if d.Status != workflowdecisions.StatusOpenOwned {
			continue
		}
		for _, affected := range d.Affected {
			openDecisionsByKey[affected] = append(openDecisionsByKey[affected], d.ID)
		}
	}
	nodeByBareID := make(map[string]intentcoverage.IntentNode, len(snap.IntentNodes))
	for _, n := range snap.IntentNodes {
		nodeByBareID[n.IntentID] = n
	}

	accepted := dedupSorted(snap.Accepted)
	results := make([]DefinitionResult, 0, len(accepted))
	blockedCount := 0
	for _, definition := range accepted {
		record, hasRecord := recordByDefinition[definition]
		intentName := record.Intent

		var blockers []Blocker
		add := func(code, detail string) { blockers = append(blockers, Blocker{Code: code, Detail: detail}) }

		if !boundDefinition[definition] {
			add(UnboundDesign, "no exact design record is bound to "+definition)
		}
		for _, f := range joinFindingsByDefinition[definition] {
			switch f.Code {
			case workflowdesignjoin.DuplicateDesign, workflowdesignjoin.AliasRecord, workflowdesignjoin.InvalidDefinition, workflowdesignjoin.UnknownDefinition:
				add(UnboundDesign, f.Code+": "+f.Detail)
			}
		}

		if !hasRecord && intentName == "" {
			// Recover the display name from the definition_ref itself
			// when no design record exists at all, so CatalogNameOnly can
			// still be evaluated against workflowarchetypes.
			intentName = definitionIntentHint(definition)
		}
		if !hasRecord && snap.CatalogMappedIntents[intentName] {
			add(CatalogNameOnly, "intent "+intentName+" is placed in a WF-DISC-002 HR catalog with no workflow design record; it carries only a catalog name/archetype label")
		}

		if hasRecord {
			for _, f := range findingsByIntent[intentName] {
				if f.Code == bareNotApplicable {
					add(UnjustifiedOmission, f.Field+": "+f.Detail)
				} else {
					add(CatalogNameOnly, f.Code+": "+f.Detail)
				}
			}
		}

		for _, f := range snap.GraphFindings[definition] {
			switch f.Code {
			case workflowexpansion.BareOmission, workflowexpansion.BareReplacement, workflowexpansion.MandatoryOmission, workflowexpansion.MandatoryReplacement, workflowexpansion.DuplicateOmit:
				add(UnjustifiedOmission, f.Code+": "+f.Detail)
			default:
				add(UnresolvedReference, f.Code+": "+f.Detail)
			}
		}
		if hasRecord && snap.Graphs[definition] == nil && len(snap.GraphFindings[definition]) == 0 {
			add(UnresolvedReference, "no expanded high-level graph is available for "+definition)
		}

		for _, f := range ownershipFindingsByDefinition[definition] {
			if f.Code == designownership.UnknownDefinition {
				continue
			}
			add(UnresolvedReference, f.Code+": "+f.Detail)
		}

		for _, f := range snap.ScenarioFindings[definition] {
			add(MissingAdversarialScenario, f.Code+": "+f.Detail)
		}
		if hasRecord {
			matrix := snap.Scenarios[definition]
			switch {
			case matrix == nil && len(snap.ScenarioFindings[definition]) == 0:
				add(MissingAdversarialScenario, "no adversarial scenario matrix is available for "+definition)
			case matrix != nil && len(matrix.Scenarios) == 0 && len(matrix.NotApplicable) == 0:
				add(MissingAdversarialScenario, "scenario matrix for "+definition+" carries zero scenarios and zero typed justifications")
			}
		}

		seenKey := map[string]bool{}
		for _, key := range []string{definition, intentName} {
			if key == "" || seenKey[key] {
				continue
			}
			seenKey[key] = true
			for _, decisionID := range openDecisionsByKey[key] {
				add(UnresolvedDecision, "decision "+decisionID+" affecting "+key+" is still OPEN_OWNED")
			}
		}

		bareID := bareIntentID(definition)
		node, hasNode := nodeByBareID[bareID]
		for _, o := range snap.Orphans {
			if o.ID != bareID {
				continue
			}
			switch o.Kind {
			case intentcoverage.KindIntentWorkflowOrDirect, intentcoverage.KindTodoDirectDangling:
				add(DanglingTodoEdge, o.Detail)
			case intentcoverage.KindIntentTest:
				add(DanglingTestEdge, o.Detail)
			case intentcoverage.KindIntentEvidence:
				add(DanglingEvidenceEdge, o.Detail)
			}
		}

		claimed := intentcoverage.Conceptual
		if hasNode {
			claimed = node.Status
		}
		allowed := claimed
		if len(blockers) > 0 && rank(intentcoverage.Catalogued) < rank(claimed) {
			allowed = intentcoverage.Catalogued
		}

		results = append(results, DefinitionResult{
			Definition:    definition,
			Intent:        intentName,
			ClaimedStatus: claimed,
			AllowedStatus: allowed,
			Capped:        allowed != claimed,
			Blockers:      sortBlockers(blockers),
		})
		if len(blockers) > 0 {
			blockedCount++
		}
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Definition < results[j].Definition })
	report := Report{
		SchemaVersion:    schemaVersion,
		TotalDefinitions: len(results),
		BlockedCount:     blockedCount,
		Results:          results,
	}
	report.Digest = digestReport(report)
	return report
}

// Validate re-derives the report from snap and returns one exact mismatch
// string for every place report claims more than fresh evidence supports:
// a higher AllowedStatus for a definition, or totals that do not
// reconcile with the per-definition rows. A report built by Reconcile
// itself can never fail Validate; it exists so a hand-tampered or
// independently produced report is still caught - the same defense
// intentcoverage.Validate provides for its own ladder.
func Validate(snap Snapshot, report Report) []string {
	fresh := Reconcile(snap)
	freshByDefinition := make(map[string]DefinitionResult, len(fresh.Results))
	for _, r := range fresh.Results {
		freshByDefinition[r.Definition] = r
	}

	var mismatches []string
	blocked := 0
	for _, r := range report.Results {
		if len(r.Blockers) > 0 {
			blocked++
		}
		want, ok := freshByDefinition[r.Definition]
		if !ok {
			mismatches = append(mismatches, "definition "+r.Definition+" is not part of the fresh accepted set")
			continue
		}
		if rank(r.AllowedStatus) > rank(want.AllowedStatus) {
			mismatches = append(mismatches, fmt.Sprintf("definition %s claims allowed status %s but fresh evidence supports at most %s", r.Definition, r.AllowedStatus, want.AllowedStatus))
		}
	}
	if report.TotalDefinitions != len(report.Results) {
		mismatches = append(mismatches, fmt.Sprintf("total_definitions=%d does not match %d reported rows", report.TotalDefinitions, len(report.Results)))
	}
	if report.BlockedCount != blocked {
		mismatches = append(mismatches, fmt.Sprintf("blocked_count=%d does not match %d rows that actually carry a blocker", report.BlockedCount, blocked))
	}
	sort.Strings(mismatches)
	return mismatches
}

func dedupSorted(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func sortBlockers(blockers []Blocker) []Blocker {
	if len(blockers) == 0 {
		return nil
	}
	out := append([]Blocker(nil), blockers...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Code != out[j].Code {
			return out[i].Code < out[j].Code
		}
		return out[i].Detail < out[j].Detail
	})
	seen := make(map[string]bool, len(out))
	deduped := out[:0]
	for _, b := range out {
		key := b.Code + "\x00" + b.Detail
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, b)
	}
	return deduped
}

// bareIntentID strips a definition_ref's "/vN" version suffix, matching
// how tools/planning/intentcoverage and definitions/planning/capability-coverage.yaml
// key an accepted intent without its version.
func bareIntentID(definition string) string {
	if i := strings.LastIndex(definition, "/v"); i >= 0 {
		return definition[:i]
	}
	return definition
}

// definitionIntentHint recovers a plausible display name for a definition
// that has no design record at all, by reversing the definition_ref's
// dotted verb_noun segment into the CamelCase convention every design
// record and catalog row already uses (e.g. "change_manager" ->
// "ChangeManager"). It only needs to be good enough to probe
// CatalogMappedIntents at the call site; a miss simply means
// CatalogNameOnly does not fire and UnboundDesign alone still names the
// gap.
func definitionIntentHint(definition string) string {
	bare := bareIntentID(definition)
	parts := strings.Split(bare, ".")
	if len(parts) == 0 {
		return ""
	}
	verbNoun := parts[len(parts)-1]
	segments := strings.Split(verbNoun, "_")
	var b strings.Builder
	for _, seg := range segments {
		if seg == "" {
			continue
		}
		b.WriteString(strings.ToUpper(seg[:1]))
		b.WriteString(seg[1:])
	}
	return b.String()
}

func digestReport(report Report) string {
	var lines []string
	for _, r := range report.Results {
		var blockerParts []string
		for _, b := range r.Blockers {
			blockerParts = append(blockerParts, b.Code+"\x01"+b.Detail)
		}
		lines = append(lines, strings.Join([]string{
			r.Definition, r.Intent, string(r.ClaimedStatus), string(r.AllowedStatus),
			fmt.Sprint(r.Capped), strings.Join(blockerParts, "\x02"),
		}, "\x00"))
	}
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// MarshalReport renders a report as canonical indented JSON, mirroring the
// MarshalX helpers every sibling workflow* package exports.
func MarshalReport(report Report) ([]byte, error) { return report.JSON() }
