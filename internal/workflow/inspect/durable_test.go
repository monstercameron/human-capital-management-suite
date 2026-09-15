package inspect_test

import (
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/inspect"
)

// TestDurableView_RecordAndJSON proves the manifest lookup finds exactly the
// listed family and the rendered JSON keeps an UNAVAILABLE family's state and
// reason and a withheld trace list's redaction, rather than dropping either.
func TestDurableView_RecordAndJSON(t *testing.T) {
	t.Parallel()
	dv := inspect.DurableView{
		Records: []inspect.RecordFamily{
			{Family: inspect.FamilyTimer, Section: inspect.SectionNode, State: inspect.RecordLoaded, Count: 2},
			{Family: inspect.FamilyConnectorOperation, Section: inspect.SectionConnector,
				State: inspect.RecordUnavailable, Reason: inspect.ReasonNoConnectorOperationLink},
		},
		TraceIDs: inspect.RefListRedacted("NO_TRACE_SCOPE"),
	}

	rec, ok := dv.Record(inspect.FamilyTimer)
	if !ok || rec.Count != 2 || rec.State != inspect.RecordLoaded {
		t.Fatalf("Record(timer) = %+v, %v", rec, ok)
	}
	if _, ok := dv.Record(inspect.FamilyOutbox); ok {
		t.Fatal("Record found a family the manifest does not list")
	}

	b, err := dv.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if b[len(b)-1] != '\n' {
		t.Error("rendered durable view does not end in a newline")
	}
	var decoded struct {
		Records []struct {
			Family string `json:"family"`
			State  string `json:"state"`
			Reason string `json:"reason"`
		} `json:"records"`
		TraceIDs struct {
			State  string `json:"state"`
			Reason string `json:"reason"`
		} `json:"trace_ids"`
	}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Records) != 2 || decoded.Records[1].State != "UNAVAILABLE" ||
		decoded.Records[1].Reason != inspect.ReasonNoConnectorOperationLink {
		t.Errorf("rendered records = %+v", decoded.Records)
	}
	if decoded.TraceIDs.State == "VALUE" || decoded.TraceIDs.Reason != "NO_TRACE_SCOPE" {
		t.Errorf("rendered trace ids = %+v, want the redaction kept", decoded.TraceIDs)
	}
}
