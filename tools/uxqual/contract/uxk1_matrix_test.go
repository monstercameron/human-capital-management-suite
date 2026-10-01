package contract

import (
	"encoding/json"
	"testing"
)

// TestTodo_UX_001_Golden pins the wire bytes in the qualification package as
// well as the production contract package. This keeps the historical import
// path's evidence honest when its implementation remains an alias.
func TestTodo_UX_001_Golden(t *testing.T) {
	got, err := json.Marshal(NewWorkspaceContract(fixtureSourceRecord(), fixtureVisibleFields(), fixtureVisibleActions()))
	if err != nil {
		t.Fatalf("marshal Promotion workspace contract: %v", err)
	}
	const want = `{"WorkspaceID":"wf-promo-1048","Title":"Promotion: Jane Rivera","Request":{"WorkerID":"worker-1048","WorkerName":"Jane Rivera","Fields":[{"ID":"currentJobTitle","Label":"Current job title","Kind":"readonly","Value":"Registered Nurse II","Validation":{"Required":false,"Message":""}},{"ID":"proposedJobTitle","Label":"Proposed job title","Kind":"lookup","Value":"Registered Nurse III","Validation":{"Required":true,"Message":""}},{"ID":"proposedCompensation","Label":"Proposed base pay","Kind":"money","Value":"$98,000.00","Validation":{"Required":true,"Message":""}},{"ID":"effectiveDate","Label":"Effective date","Kind":"date","Value":"2026-10-01","Validation":{"Required":true,"Message":""}},{"ID":"businessReason","Label":"Business reason","Kind":"textarea","Value":"Scope of practice increase; retention risk.","Validation":{"Required":true,"Message":""}}]},"Preflight":[{"ID":"band-check","Severity":"success","Label":"Compensation band","Detail":"Proposed amount is within the approved band for the target level."},{"ID":"payroll-cutoff","Severity":"warning","Label":"Payroll cutoff","Detail":"Effective date falls inside the next payroll window."}],"Simulation":{"Status":"ready","Summary":"Simulation completed with no blocking findings.","GeneratedAt":"2026-09-01T11:10:00Z","Checks":[{"Label":"Compensation band","Status":"success","Detail":"Within approved band."},{"Label":"Payroll cutoff","Status":"warning","Detail":"Inside next payroll window."}]},"Timeline":[{"At":"2026-09-01T09:05:00Z","Actor":"Alex Manager","Label":"Submitted request"},{"At":"2026-09-01T10:20:00Z","Actor":"Riley HRBP","Label":"Approved HRBP review"}],"Actions":[{"ID":"approve","Label":"Approve","Transition":"approve","Variant":"primary","RequiresReason":false},{"ID":"reject","Label":"Reject","Transition":"reject","Variant":"danger","RequiresReason":true},{"ID":"request_more_information","Label":"Request info","Transition":"request_more_information","Variant":"secondary","RequiresReason":false}],"Provenance":{"CapabilityID":"people.promote","CapabilityVersion":"v1","SourceSystem":"hcm-next","AsOf":"2026-09-01T11:10:00Z"}}`
	if string(got) != want {
		t.Fatalf("Promotion workspace golden changed:\n got: %s\nwant: %s", got, want)
	}
}
