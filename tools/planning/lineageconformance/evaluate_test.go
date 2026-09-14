package lineageconformance

import (
	"testing"
)

func TestChainAndSealLinkRecordsInLineageOrder(t *testing.T) {
	recs := Chain("parent#TX", []Record{
		{ID: "e", Link: LinkEvent, Watermark: "s@1"},
		{ID: "p", Link: LinkProjection, DerivedFrom: []string{"e"}, SourceHead: "s@1"},
		{ID: "c", Link: LinkCausation},
		{ID: "i", Link: LinkIntent, Payload: "hello"},
	})
	order := ""
	for _, r := range recs {
		order += r.ID
	}
	if order != "ciep" || recs[0].CausedBy != "parent#TX" || recs[1].CausedBy != "c" || recs[3].CausedBy != "e" {
		t.Fatalf("chain = %+v", recs)
	}
	sealed := Seal(recs)
	if sealed[1].PayloadDigest != PayloadDigest("hello") || sealed[1].PrevDigest != sealed[0].Digest {
		t.Fatalf("seal did not link predecessor or payload: %+v", sealed[1])
	}
	if sealed[0].PrevDigest != "" {
		t.Fatal("a cause outside the slice produced a predecessor digest")
	}
	if sealed[3].ProjectionDigest != ProjectionReplay([]string{sealed[2].Digest}) {
		t.Fatal("seal did not replay the projection from its source event")
	}
	for _, r := range sealed {
		if r.Digest != r.ComputeDigest() {
			t.Fatalf("%s seal does not recompute", r.ID)
		}
	}
}

func TestViewRedactsAndGraphDigestIgnoresOrder(t *testing.T) {
	g := Graph{Tenant: "t", Records: Seal([]Record{
		{ID: "a", Tenant: "t", Link: LinkIntent, Payload: "open"},
		{ID: "b", Tenant: "t", Link: LinkWorkflow, Payload: "secret", Classification: "RESTRICTED"},
		{ID: "x", Tenant: "other", Link: LinkEvent, Payload: "foreign"},
	})}
	v := View(g, Reader{Tenant: "t"})
	if len(v.Records) != 2 || v.Records[0].Payload != "open" || v.Records[1].Payload != "" || !v.Records[1].Redacted || v.Records[1].Reason == "" {
		t.Fatalf("view = %+v", v.Records)
	}
	if cleared := View(g, Reader{Tenant: "t", Clearances: []string{"RESTRICTED"}}); cleared.Records[1].Payload != "secret" {
		t.Fatal("cleared reader lost the payload")
	}
	reversed := Graph{Tenant: "t", Records: []Record{g.Records[2], g.Records[1], g.Records[0]}}
	if GraphDigest(reversed) != GraphDigest(g) {
		t.Fatal("graph digest depends on record order")
	}
	if GraphDigest(Graph{Tenant: "u", Records: g.Records}) == GraphDigest(g) {
		t.Fatal("graph digest ignores tenant")
	}
}

func TestStatusRule(t *testing.T) {
	proven := func(l Link) LinkStatus { return LinkStatus{Link: l, State: StateProven, Evidence: []string{"e"}} }
	unknown := func(l Link) LinkStatus { return LinkStatus{Link: l, State: StateUnknown} }
	cases := []struct {
		name     string
		links    []LinkStatus
		findings []Finding
		want     Status
	}{
		{"all proven", []LinkStatus{proven(LinkIntent), proven(LinkEvent)}, nil, StatusComplete},
		{"downstream proven", []LinkStatus{proven(LinkIntent), proven(LinkEvent), unknown(LinkOutbox)}, []Finding{{Code: CodeLinkMissing}}, StatusPartial},
		{"only intent", []LinkStatus{proven(LinkIntent), unknown(LinkEvent)}, []Finding{{Code: CodeNoProducer}}, StatusUnknown},
		{"only causation", []LinkStatus{proven(LinkCausation), unknown(LinkIntent)}, []Finding{{Code: CodeLinkMissing}}, StatusUnknown},
		{"defect finding", []LinkStatus{proven(LinkIntent)}, []Finding{{Code: CodeCrossTenant}}, StatusDefective},
		{"defect state", []LinkStatus{{Link: LinkEvent, State: StateDefect}}, nil, StatusDefective},
		{"proven with gap finding", []LinkStatus{proven(LinkIntent), proven(LinkEvent)}, []Finding{{Code: CodeDefinitionAbsent}}, StatusPartial},
		{"no links", nil, nil, StatusUnknown},
	}
	for _, c := range cases {
		if got := statusOf(c.links, c.findings); got != c.want {
			t.Fatalf("%s = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestCheckProjectionRefusesForeignSource(t *testing.T) {
	c := Case{ID: "k"}
	byID := map[string]Record{
		"e": {ID: "e", Case: "other", Link: LinkEvent, Tenant: "t"},
	}
	f := checkProjection(c, Record{ID: "p", Tenant: "t", DerivedFrom: []string{"e"}}, byID)
	if len(f) != 1 || f[0].Code != CodeNotRebuildable {
		t.Fatalf("foreign projection source = %+v", f)
	}
}
