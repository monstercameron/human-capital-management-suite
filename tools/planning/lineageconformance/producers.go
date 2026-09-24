package lineageconformance

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/lineage"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/closurewitness"
)

// ErrProducerInvalid reports a lineage producer the tree does not back.
var ErrProducerInvalid = errors.New("lineageconformance: producer is not backed by a closed todo and existing tests")

// ErrUnmappedStage reports an owner lineage stage with no link mapping: a
// new stage must be classified, never silently dropped.
var ErrUnmappedStage = errors.New("lineageconformance: lineage stage has no link mapping")

// Producer is one implementation that publishes lineage for some links of
// one case. It is a claim, so it is only honoured when [ValidateProducer]
// finds its todo closed and every proving test present and cited.
type Producer struct {
	ID      string   `json:"id"`
	Todo    string   `json:"todo"`
	Package string   `json:"package"`
	Case    string   `json:"case"`
	Links   []Link   `json:"links"`
	Tests   []string `json:"tests"`
}

// stageLinks maps DATA-015's closed stage vocabulary onto the universal
// chain: an approval is the governed workflow step, a plan is the
// transaction plan and a connector operation is the external effect.
var stageLinks = map[lineage.Stage]Link{
	lineage.StageProposal:    LinkProposal,
	lineage.StageApproval:    LinkWorkflow,
	lineage.StagePlan:        LinkTransaction,
	lineage.StageEvent:       LinkEvent,
	lineage.StageConnectorOp: LinkEffect,
	lineage.StageObservation: LinkObservation,
	lineage.StageRepair:      LinkRepair,
}

// StageLink returns the chain link a DATA-015 stage publishes.
func StageLink(stage lineage.Stage) (Link, bool) {
	l, ok := stageLinks[stage]
	return l, ok
}

// stageLinksFor maps a stage list, refusing any unmapped stage.
func stageLinksFor(stages []lineage.Stage) ([]Link, error) {
	links := make([]Link, 0, len(stages))
	for _, stage := range stages {
		l, ok := StageLink(stage)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnmappedStage, stage)
		}
		links = append(links, l)
	}
	return links, nil
}

// PromotionDefinition is the definition DATA-015's trace belongs to.
const PromotionDefinition = "hcmnext.people.promote_worker/v1"

// DefaultProducers returns the lineage producers the tree holds today.
// DATA-015's links are computed from internal/data/lineage.Stages, so a
// stage added there changes this set (or fails, if unmapped) without an
// edit here. LEDGER-013 rebuilds workflow.promotion_outcome from the
// ledger, and RECON-001 persists one reconciliation job per effect and
// comparison policy.
func DefaultProducers() ([]Producer, error) {
	data015, err := stageLinksFor(lineage.Stages)
	if err != nil {
		return nil, err
	}
	return []Producer{
		{
			ID: "DATA-015/lineage.Assemble", Todo: "DATA-015",
			Package: "internal/data/lineage", Case: PromotionDefinition, Links: data015,
			Tests: []string{"TestTodo_DATA_015", "TestTodo_DATA_015_Integration", "TestTodo_DATA_015_Mutation"},
		},
		{
			ID: "LEDGER-013/projection.RebuildPromotionOutcome", Todo: "LEDGER-013",
			Package: "internal/data/projection", Case: PromotionDefinition, Links: []Link{LinkProjection},
			Tests: []string{"TestTodo_LEDGER_013"},
		},
		{
			ID: "RECON-001/effect_reconciliation_job", Todo: "RECON-001",
			Package: "internal/operations/reconcile", Case: PromotionDefinition, Links: []Link{LinkReconciliation},
			Tests: []string{"TestTodo_RECON_001"},
		},
	}, nil
}

// ValidateProducer checks a producer against the todo registry and the
// test-name scan: the todo exists, is closed and not retired, every
// proving test exists and is named by the todo's TEST or Evidence, and
// every link is valid and distinct.
func ValidateProducer(p Producer, todos []closurewitness.TodoRow, tests map[string]bool) error {
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Case) == "" || len(p.Links) == 0 || len(p.Tests) == 0 {
		return fmt.Errorf("%w: %q is missing id, case, links or tests", ErrProducerInvalid, p.ID)
	}
	seen := map[Link]bool{}
	for _, l := range p.Links {
		if !l.Valid() || seen[l] {
			return fmt.Errorf("%w: %s declares invalid or repeated link %q", ErrProducerInvalid, p.ID, l)
		}
		seen[l] = true
	}
	var row *closurewitness.TodoRow
	for i := range todos {
		if todos[i].ID == p.Todo {
			row = &todos[i]
			break
		}
	}
	switch {
	case row == nil:
		return fmt.Errorf("%w: %s names todo %q, which is not in the registry", ErrProducerInvalid, p.ID, p.Todo)
	case !row.Done || row.Retired:
		return fmt.Errorf("%w: %s names todo %s, which is not closed", ErrProducerInvalid, p.ID, p.Todo)
	}
	for _, test := range p.Tests {
		if !tests[test] {
			return fmt.Errorf("%w: %s proving test %s is defined nowhere in the tree", ErrProducerInvalid, p.ID, test)
		}
		if row.PrimaryTest != test && !slices.Contains(row.EvidenceTests, test) {
			return fmt.Errorf("%w: %s proving test %s is not cited by %s", ErrProducerInvalid, p.ID, test, p.Todo)
		}
	}
	return nil
}

// FromTrace maps one verified DATA-015 trace onto unchained lineage
// records for caseID: each node keeps its owner digest as SourceDigest,
// an EVENT carries its exact stream@sequence watermark and every other
// node its source/version@known-at watermark, and restricted nodes stay
// RESTRICTED stubs. Callers add their own links and then Chain and Seal.
func FromTrace(caseID, tenant string, trace lineage.Trace) ([]Record, error) {
	if err := trace.Verify(); err != nil {
		return nil, fmt.Errorf("lineageconformance: trace for %s: %w", caseID, err)
	}
	if trace.Tenant != tenant {
		return nil, fmt.Errorf("lineageconformance: trace of tenant %q offered for tenant %q", trace.Tenant, tenant)
	}
	records := make([]Record, 0, len(trace.Nodes))
	for _, node := range trace.Nodes {
		link, ok := StageLink(node.Stage)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnmappedStage, node.Stage)
		}
		rec := Record{
			ID: caseID + "#" + string(link), Tenant: tenant, Case: caseID, Link: link,
			SourceDigest: node.Digest, Payload: node.Payload,
			Watermark: node.Source + "/" + node.Version + "@" + node.KnownAt.UTC().Format(time.RFC3339Nano),
		}
		if node.Stage == lineage.StageEvent {
			rec.Watermark = node.StreamKey + "@" + strconv.FormatInt(node.Sequence, 10)
		}
		if node.Restricted {
			rec.Classification = RestrictedClassification
			rec.Redacted = true
			rec.Reason = node.Reason
		}
		records = append(records, rec)
	}
	return records, nil
}

// RestrictedClassification is the classification a DATA-015 restricted
// node carries.
const RestrictedClassification = "RESTRICTED"
