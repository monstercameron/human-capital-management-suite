package lineageconformance_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/lineage"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/closurewitness"
	lc "github.com/monstercameron/human-capital-management-suite/tools/planning/lineageconformance"
)

// TestMain starts embedded PostgreSQL lazily: only the Integration and
// Recovery tests ask for a database.
func TestMain(m *testing.M) { pgtest.RunMain(m) }

const (
	tenantA = "tenant-a"
	tenantB = "tenant-b"
	asOf    = "2026-09-13"
)

func repoRoot() string { return filepath.Join("..", "..", "..") }

func witnessFor(def string, source closurewitness.EdgeState) closurewitness.Witness {
	return closurewitness.Witness{
		Definition: def, Result: closurewitness.ResultIncomplete, Digest: "sha256:witness-" + def,
		Classes: []closurewitness.ClassStatus{{Class: closurewitness.ClassSource, State: source}},
	}
}

func witnessReport(defs []intent.Definition) closurewitness.Report {
	rep := closurewitness.Report{Digest: "sha256:witness-report", AsOf: asOf}
	for _, d := range defs {
		rep.Witnesses = append(rep.Witnesses, witnessFor(d.Ref.String(), closurewitness.StateBound))
	}
	return rep
}

// catalogInput is the compiled catalog with one bound witness per
// definition, the shape LoadInput produces without reading the tree.
func catalogInput() lc.CaseInput {
	defs := definitions.All()
	return lc.CaseInput{Witnesses: witnessReport(defs), Definitions: defs, Bindings: definitions.Bindings()}
}

func authorized(tenant string) lc.Reader {
	return lc.Reader{Tenant: tenant, Clearances: []string{lc.RestrictedClassification}}
}

func uncleared(tenant string) lc.Reader { return lc.Reader{Tenant: tenant} }

func caseByID(t *testing.T, cases []lc.Case, id string) lc.Case {
	t.Helper()
	for _, c := range cases {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no generated case %s", id)
	return lc.Case{}
}

// caseRecords builds one unchained record per required link: the EVENT
// carries an exact stream watermark, the PROJECTION derives from it, the
// CORRECTION corrects it, the WORKFLOW hop is a RESTRICTED stub and a
// TRIGGER's causation names its trigger.
func caseRecords(c lc.Case, tenant string) []lc.Record {
	event := c.ID + "#" + string(lc.LinkEvent)
	var recs []lc.Record
	for _, l := range c.Required {
		rec := lc.Record{
			ID: c.ID + "#" + string(l), Tenant: tenant, Case: c.ID, Link: l,
			Payload:   fmt.Sprintf("%s %s", c.ID, l),
			Watermark: "publisher/" + strings.ToLower(string(l)) + "@1",
		}
		switch l {
		case lc.LinkEvent:
			rec.Watermark = "stream:" + c.ID + "@1"
		case lc.LinkWorkflow:
			rec.Classification = lc.RestrictedClassification
			rec.PayloadDigest = lc.PayloadDigest("approver=hrbp-7")
			rec.Payload, rec.Redacted, rec.Reason = "", true, "approver identity above caller clearance"
		case lc.LinkProjection:
			rec.DerivedFrom = []string{event}
			rec.SourceHead = "stream:" + c.ID + "@1"
		case lc.LinkCorrection:
			rec.Corrects = event
		case lc.LinkCausation:
			rec.Trigger = c.Trigger
		}
		recs = append(recs, rec)
	}
	return recs
}

// completeGraph builds a complete sealed graph for any generated case; a
// CHILD case's graph also holds its parent's lineage, and the child's
// causation names the parent's transaction (or last) record.
func completeGraph(t *testing.T, cases []lc.Case, c lc.Case, tenant string) lc.Graph {
	t.Helper()
	if c.Path != lc.PathChild {
		return lc.Graph{Tenant: tenant, Records: lc.Seal(lc.Chain("", caseRecords(c, tenant)))}
	}
	parent := lc.Chain("", caseRecords(caseByID(t, cases, c.Parent), tenant))
	cause := parent[len(parent)-1].ID
	for _, rec := range parent {
		if rec.Link == lc.LinkTransaction {
			cause = rec.ID
		}
	}
	child := lc.Chain(cause, caseRecords(c, tenant))
	return lc.Graph{Tenant: tenant, Records: lc.Seal(append(parent, child...))}
}

// promotionNodes is a DATA-015-shaped trace: proposal, restricted
// approval, plan, ledger event, connector operation, observation, repair.
func promotionNodes() []lineage.Node {
	base := time.Date(2026, time.February, 1, 9, 0, 0, 0, time.UTC)
	nodes := []lineage.Node{
		{Stage: lineage.StageProposal, Source: "proposal-svc", Version: "v3", Authority: "auth/hrbp", Principal: "hrbp-1", Payload: "base_pay=95000.00"},
		{Stage: lineage.StageApproval, Source: "approval-svc", Version: "v3", Authority: "auth/policy", Principal: "policy-7", Restricted: true, Reason: "approver PII above caller clearance"},
		{Stage: lineage.StagePlan, Source: "workflow", Version: "promote/v1", Authority: "auth/workflow", Principal: "workflow-3", Payload: "plan=promote-into-management"},
		{Stage: lineage.StageEvent, Source: "ledger", Version: "stream-v1", Authority: "auth/ledger", Principal: "tx-44", Payload: "stream=payroll seq=41", StreamKey: "payroll", Sequence: 41},
		{Stage: lineage.StageConnectorOp, Source: "hris-connector", Version: "conn-v9", Authority: "auth/connector", Principal: "op-117", Payload: "op=update-base-pay"},
		{Stage: lineage.StageObservation, Source: "hris-connector", Version: "conn-v9", Authority: "auth/connector", Principal: "op-118", Payload: "base_pay=95000.00", ObservationRef: "obs-118", ConnectorRef: "conn/hris"},
		{Stage: lineage.StageRepair, Source: "repair-svc", Version: "v2", Authority: "auth/repair", Principal: "repair-5", Payload: "rounding-delta=0.00"},
	}
	for i := range nodes {
		nodes[i].Field = "worker.base_pay"
		nodes[i].EffectiveAt = base
		nodes[i].KnownAt = base.Add(time.Duration(i) * time.Hour)
		nodes[i].Evidence = []string{fmt.Sprintf("ev-%d", i)}
		nodes[i].Digest = fmt.Sprintf("sha256:node-%d", i)
		if i > 0 {
			nodes[i].PrevDigest = nodes[i-1].Digest
		}
	}
	return nodes
}

func promotionTrace(t *testing.T, tenant string) lineage.Trace {
	t.Helper()
	trace, err := lineage.Assemble(tenant, "intent/promo-22", "worker.base_pay", promotionNodes())
	if err != nil {
		t.Fatal(err)
	}
	return trace
}

// promotionGraph extends the DATA-015 trace to the full chain: the trace
// supplies proposal, workflow, transaction, event, effect, observation and
// repair; intent, projection, outbox, reconciliation and correction are
// added around it, and the whole case is chained and sealed.
func promotionGraph(t *testing.T, tenant string) lc.Graph {
	t.Helper()
	caseID := lc.PromotionDefinition
	recs, err := lc.FromTrace(caseID, tenant, promotionTrace(t, tenant))
	if err != nil {
		t.Fatal(err)
	}
	event := caseID + "#" + string(lc.LinkEvent)
	recs = append(recs,
		lc.Record{ID: caseID + "#INTENT", Tenant: tenant, Case: caseID, Link: lc.LinkIntent, Watermark: "intent-store@promo-22", Payload: "intent/promo-22"},
		lc.Record{ID: caseID + "#PROJECTION", Tenant: tenant, Case: caseID, Link: lc.LinkProjection, Watermark: "workflow.promotion_outcome@41",
			DerivedFrom: []string{event}, SourceHead: "payroll@41", Payload: "promotion_outcome row 1"},
		lc.Record{ID: caseID + "#OUTBOX", Tenant: tenant, Case: caseID, Link: lc.LinkOutbox, Watermark: "outbox@promotion.effect:promo-22", Payload: "promotion.effect:promo-22"},
		lc.Record{ID: caseID + "#RECONCILIATION", Tenant: tenant, Case: caseID, Link: lc.LinkReconciliation, Watermark: "reconcile@run-9", Payload: "expected=observed base_pay"},
		lc.Record{ID: caseID + "#CORRECTION", Tenant: tenant, Case: caseID, Link: lc.LinkCorrection, Watermark: "payroll@42", Corrects: event, Payload: "effective date corrected"},
	)
	return lc.Graph{Tenant: tenant, Records: lc.Seal(lc.Chain("", recs))}
}

func hasFinding(res lc.CaseResult, code string, link lc.Link) bool {
	return slices.ContainsFunc(res.Findings, func(f lc.Finding) bool { return f.Code == code && f.Link == link })
}

func linkState(res lc.CaseResult, link lc.Link) lc.LinkState {
	for _, l := range res.Links {
		if l.Link == link {
			return l.State
		}
	}
	return ""
}

// withRecord returns a copy of g with fn applied to the record id, resealed
// so the planted defect is the only thing that changed.
func withRecord(g lc.Graph, id string, reseal bool, fn func(*lc.Record)) lc.Graph {
	out := lc.Graph{Tenant: g.Tenant, Records: slices.Clone(g.Records)}
	for i := range out.Records {
		if reseal {
			out.Records[i].ProjectionDigest = ""
		}
		if out.Records[i].ID == id {
			fn(&out.Records[i])
		}
	}
	if reseal {
		out.Records = lc.Seal(out.Records)
	}
	return out
}

// dropLink removes a case's records for one link and republishes the rest
// of that case correctly chained and sealed, so the absence is the only
// defect planted.
func dropLink(g lc.Graph, caseID string, link lc.Link) lc.Graph {
	var others, own []lc.Record
	for _, rec := range g.Records {
		switch {
		case rec.Case != caseID:
			others = append(others, rec)
		case rec.Link != link:
			rec.ProjectionDigest = ""
			own = append(own, rec)
		}
	}
	cause := ""
	for _, rec := range g.Records {
		if rec.Case == caseID && rec.CausedBy != "" {
			if _, sameCase := findRecord(g, rec.CausedBy, caseID); !sameCase {
				cause = rec.CausedBy
			}
		}
	}
	return lc.Graph{Tenant: g.Tenant, Records: lc.Seal(append(others, lc.Chain(cause, own)...))}
}

func findRecord(g lc.Graph, id, caseID string) (lc.Record, bool) {
	for _, rec := range g.Records {
		if rec.ID == id && rec.Case == caseID {
			return rec, true
		}
	}
	return lc.Record{}, false
}
