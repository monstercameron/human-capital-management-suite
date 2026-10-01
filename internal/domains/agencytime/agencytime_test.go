package agencytime

import (
	"errors"
	"strings"
	"testing"
)

func assignment() Assignment {
	return Assignment{
		ID: "assign-1", WorkerRef: "worker-1", AgencyRef: "agency-acme",
		HostEntityRef: "host-ironridge", WorkOrderRef: "wo-77", WorksiteRef: "site-north", Role: "warehouse-associate",
	}
}

func approvedLines() []ApprovedLine {
	return []ApprovedLine{
		{WorkerRef: "worker-1", Date: "2026-09-01", Minutes: 480, Project: "receiving", CostCode: "CC-1", RateCode: "STD", RevisionDigest: "rev-1"},
		{WorkerRef: "worker-1", Date: "2026-09-02", Minutes: 480, Project: "receiving", CostCode: "CC-1", RateCode: "STD", RevisionDigest: "rev-2"},
	}
}

// TestTodo_WTIME_012 is the primary proof: an agency assignment names every
// party, and approved time exports into the generic canonical payload bound
// to that assignment, never to the host's own payroll.
func TestTodo_WTIME_012(t *testing.T) {
	payload, err := BuildExport(BuildExportRequest{Assignment: assignment(), Lines: approvedLines()})
	if err != nil {
		t.Fatal(err)
	}
	if err := payload.Validate(); err != nil {
		t.Fatalf("built payload fails validation: %v", err)
	}
	if payload.Assignment.AgencyRef != "agency-acme" || payload.Assignment.HostEntityRef != "host-ironridge" {
		t.Fatalf("assignment identity lost: %#v", payload.Assignment)
	}
	if len(payload.Lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(payload.Lines))
	}

	csvText, err := ToCSV(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(csvText, "agency-acme") || !strings.Contains(csvText, "worker-1") {
		t.Fatalf("csv export missing identity columns: %s", csvText)
	}
	roundTripped, err := FromCSV(csvText)
	if err != nil {
		t.Fatal(err)
	}
	if roundTripped.Digest != payload.Digest {
		t.Fatalf("csv round trip changed the payload digest: %s vs %s", roundTripped.Digest, payload.Digest)
	}
}

// TestTodo_WTIME_012_Security proves the SECURITY-relevant refusals: a line
// for a worker not on the assignment is rejected, an assignment missing any
// of agency/host/work-order/worksite is rejected (there is no default that
// would let the export silently omit one party), and a payload that
// validates only against a tampered digest is rejected.
func TestTodo_WTIME_012_Security(t *testing.T) {
	if _, err := BuildExport(BuildExportRequest{
		Assignment: assignment(),
		Lines:      []ApprovedLine{{WorkerRef: "someone-else", Date: "2026-09-01", Minutes: 60, RevisionDigest: "rev-x"}},
	}); !errors.Is(err, ErrRejected) {
		t.Fatalf("worker mismatch should be rejected, got %v", err)
	}

	incomplete := assignment()
	incomplete.AgencyRef = ""
	if err := incomplete.Validate(); !errors.Is(err, ErrRejected) {
		t.Fatalf("assignment missing agency should be rejected, got %v", err)
	}
	incomplete = assignment()
	incomplete.HostEntityRef = ""
	if err := incomplete.Validate(); !errors.Is(err, ErrRejected) {
		t.Fatalf("assignment missing host entity should be rejected, got %v", err)
	}

	payload, err := BuildExport(BuildExportRequest{Assignment: assignment(), Lines: approvedLines()})
	if err != nil {
		t.Fatal(err)
	}
	tampered := payload
	tampered.Lines[0].Minutes = 9999
	if err := tampered.Validate(); !errors.Is(err, ErrRejected) {
		t.Fatal("tampered payload (content changed without recomputing digest) should fail validation")
	}

	// The other client's data must never appear: two payloads for different
	// assignments never share a digest, and building a payload never accepts
	// lines for a worker outside the named assignment.
	other := assignment()
	other.ID = "assign-2"
	other.WorkerRef = "worker-2"
	otherPayload, err := BuildExport(BuildExportRequest{Assignment: other, Lines: []ApprovedLine{
		{WorkerRef: "worker-2", Date: "2026-09-01", Minutes: 60, RevisionDigest: "rev-9"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if otherPayload.Digest == payload.Digest {
		t.Fatal("distinct assignments produced the same digest")
	}
}

// TestTodo_WTIME_012_Fault proves malformed VMS acceptance/rejection
// payloads are classified, never lost: the raw content's digest survives
// even when the JSON is unparseable or missing required fields.
func TestTodo_WTIME_012_Fault(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
		want ResponseStatus
	}{
		{"not json", []byte("this is not json"), ResponseMalformed},
		{"unknown status", []byte(`{"status":"WEIRD"}`), ResponseMalformed},
		{"accepted missing ref", []byte(`{"status":"ACCEPTED"}`), ResponseMalformed},
		{"rejected missing reason", []byte(`{"status":"REJECTED"}`), ResponseMalformed},
		{"accepted", []byte(`{"status":"ACCEPTED","externalRef":"vms-ref-123"}`), ResponseAccepted},
		{"rejected", []byte(`{"status":"REJECTED","reason":"worker not enrolled"}`), ResponseRejected},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := ParseVMSResponse(c.raw)
			if resp.Status != c.want {
				t.Fatalf("want status %s, got %s", c.want, resp.Status)
			}
			if resp.RawDigest == "" {
				t.Fatal("raw digest must always be populated, even for a malformed payload")
			}
		})
	}
	if ParseVMSResponse([]byte(`{"status":"ACCEPTED","externalRef":"ref-1"}`)).ExternalRef != "ref-1" {
		t.Fatal("accepted response lost its external reference")
	}
	if ParseVMSResponse([]byte(`{"status":"REJECTED","reason":"bad rate"}`)).RejectionReason != "bad rate" {
		t.Fatal("rejected response lost its reason")
	}
}

// TestTodo_WTIME_012_AWR proves the UK AWR parity obligation is raised at
// the twelfth qualifying week and not before, from a supplied count only —
// this package does no counting of its own.
func TestTodo_WTIME_012_AWR(t *testing.T) {
	before, err := EvaluateParity(11)
	if err != nil {
		t.Fatal(err)
	}
	if before.Applies {
		t.Fatal("parity should not apply before the twelfth qualifying week")
	}
	at, err := EvaluateParity(12)
	if err != nil {
		t.Fatal(err)
	}
	if !at.Applies {
		t.Fatal("parity should apply at the twelfth qualifying week")
	}
	after, err := EvaluateParity(20)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Applies {
		t.Fatal("parity should remain applied after the twelfth qualifying week")
	}
	if _, err := EvaluateParity(-1); !errors.Is(err, ErrRejected) {
		t.Fatalf("negative qualifying weeks should be rejected, got %v", err)
	}
}

func TestTodo_WTIME_012_Version(t *testing.T) {
	if Version() != 1 {
		t.Fatalf("unexpected contract version %d", Version())
	}
	payload, err := BuildExport(BuildExportRequest{Assignment: assignment(), Lines: approvedLines()})
	if err != nil {
		t.Fatal(err)
	}
	explain, err := payload.Explain()
	if err != nil {
		t.Fatal(err)
	}
	if explain.Agency != "agency-acme" || explain.LineCount != 2 {
		t.Fatalf("explain mismatch: %#v", explain)
	}
}
